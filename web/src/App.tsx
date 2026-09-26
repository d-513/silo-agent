import { ArrowUp, Book, Box, Brain, ChevronDown, ChevronLeft, ChevronRight, Folder, Key, LayoutGrid, ListChecks, LogOut, MessageCircle, Monitor, Paperclip, Pencil, Plug, Plus, Power, Radio, SlidersHorizontal, Square, SquarePen, SquareTerminal, Trash2, User, Wrench, X } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type FormEvent, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Link, Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { ArmedButton, SaveButton, useSave } from "./Feedback";
import { COLOR_COUNT, Crest, CrestPicker, packCrest, SHAPE_COUNT } from "./Crest";
import { ArtifactOverlay, type Artifact } from "./Artifact";
import { ApprovalSlip, ConnectorAuthSlip, SlipPresence } from "./Approval";
import { ConsoleTerm } from "./Console";
import { FilesPane } from "./Files";
import { Field, inputClass, Panel, SkeletonRows, textareaClass } from "./Field";
import { joinPath } from "./fs";
import { Lamp, StatusWord, statusText } from "./Lamp";
import { NeedMachine } from "./NeedMachine";
import { Thread, type Ev } from "./Thread";
import { Composer } from "./Composer";
import { AdminLayout, AccountPage, AdminDebug, AdminSettings, AdminSearchExtract } from "./Admin";
import { SettingsPane } from "./Settings";
import { MemoriesPane } from "./Memories";
import { AdminConnectors } from "./AdminConnectors";
import { BotConnectors, startConnectorAuth } from "./BotConnectors";
import { BotChannels } from "./BotChannels";
import { RulesPane } from "./Rules";
import { AdminSkills, BotSkills, SkillHub } from "./Skills";
import type { Approval, Bot, BotConnector, Chat, Container, ModelOption, SecretMeta } from "./gen/silo/v1/ui_pb";

const tabs = ["run", "desktop", "files", "connectors", "channels", "skills", "memories", "secrets", "rules", "container", "settings"] as const;
type NavTab = (typeof tabs)[number];
type Tab = NavTab | "console";

function isNavTab(s: string | undefined): s is NavTab {
  return !!s && (tabs as readonly string[]).includes(s);
}

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

// Chat-row meta: "Just now", "12 min ago", "09:12", "Yesterday", "Tue", "Mar 4".
function when(iso: string) {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "";
  const d = new Date(t);
  const now = new Date();
  const mins = Math.floor((now.getTime() - t) / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins} min ago`;
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(now) - day(d)) / 86400000);
  if (days === 0) return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (days === 1) return "Yesterday";
  if (days < 7) return d.toLocaleDateString([], { weekday: "short" });
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

const tabMeta: Record<NavTab, { label: string; icon: typeof MessageCircle }> = {
  run: { label: "Chat", icon: MessageCircle },
  desktop: { label: "Desktop", icon: Monitor },
  files: { label: "Files", icon: Folder },
  connectors: { label: "Connectors", icon: Plug },
  channels: { label: "Channels", icon: Radio },
  skills: { label: "Skills", icon: Book },
  memories: { label: "Memories", icon: Brain },
  secrets: { label: "Secrets", icon: Key },
  rules: { label: "Rules", icon: ListChecks },
  container: { label: "Container", icon: Box },
  settings: { label: "Settings", icon: SlidersHorizontal },
};

function randomCrest() {
  return packCrest(Math.floor(Math.random() * SHAPE_COUNT), Math.floor(Math.random() * COLOR_COUNT));
}

function SiloGlyph({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 32 32" fill="none" aria-hidden="true">
      <path
        d="M6 12a10 10 0 0 1 20 0v14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2ZM11.8 11.2h8.4a0.8 0.8 0 0 1 0 1.6h-8.4a0.8 0.8 0 0 1 0-1.6Z"
        fill="currentColor"
        fillRule="evenodd"
      />
    </svg>
  );
}

function SiloMark() {
  return (
    <div className="flex shrink-0 items-center gap-2 px-3 text-ink max-wide:h-12 wide:flex-col wide:gap-1 wide:px-2 wide:pt-4">
      <SiloGlyph className="max-wide:h-5 max-wide:w-5 wide:h-[26px] wide:w-[26px]" />
      <span className="font-semibold max-wide:text-[13px] wide:text-[11px] wide:leading-4">Silo</span>
    </div>
  );
}

const AuthCtx = createContext<{
  email: string;
  admin: boolean;
  setSession: (s: { email: string; admin: boolean } | null) => void;
} | null>(null);

function useAuth() {
  const a = useContext(AuthCtx);
  if (!a) throw new Error("auth");
  return a;
}

const BotsCtx = createContext<{
  bots: Bot[] | null;
  err: string;
  refresh: () => void;
} | null>(null);

function useBots() {
  const c = useContext(BotsCtx);
  if (!c) throw new Error("bots");
  return c;
}

function BotsProvider({ children }: { children: ReactNode }) {
  const [bots, setBots] = useState<Bot[] | null>(null);
  const [err, setErr] = useState("");
  const refresh = useCallback(() => {
    ui.listBots({})
      .then((r) => {
        setBots(r.bots);
        setErr("");
      })
      .catch((e) => setErr(fail(e)));
  }, []);
  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 4000);
    return () => clearInterval(t);
  }, [refresh]);
  return <BotsCtx.Provider value={{ bots, err, refresh }}>{children}</BotsCtx.Provider>;
}

// Rail items: 40px hit areas. The active one is a lifted surface well with a
// 3px cobalt bar on the leading edge (bottom edge in the narrow top bar).
function railHit(active: boolean, extra = "") {
  return `relative flex h-10 w-10 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow] duration-[160ms] ease-quiet ${
    active ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
  } ${extra}`;
}

function railBar(on: boolean) {
  if (!on) return null;
  return (
    <span
      aria-hidden
      className="absolute rounded-full bg-cobalt max-wide:inset-x-2.5 max-wide:-bottom-1 max-wide:h-[3px] wide:inset-y-2.5 wide:-left-3 wide:w-[3px]"
    />
  );
}

function Rail({ page }: { page: "bots" | "admin" | "account" | "skills" }) {
  const { admin, email, setSession } = useAuth();
  const { bots } = useBots();
  const loc = useLocation();
  const botMatch = loc.pathname.match(/^\/bots\/([^/]+)/);
  const activeBotId = botMatch?.[1];
  const homeActive = page === "bots" && !activeBotId && loc.pathname !== "/new";
  const initial = (email.trim()[0] ?? "?").toUpperCase();
  return (
    <aside className="flex shrink-0 bg-well max-wide:h-[calc(3rem+env(safe-area-inset-top))] max-wide:w-full max-wide:flex-row max-wide:items-center max-wide:gap-1 max-wide:pt-[env(safe-area-inset-top)] max-wide:shadow-[inset_0_-1px_0_var(--color-line)] wide:w-16 wide:flex-col wide:items-center">
      <SiloMark />
      <nav className="flex min-h-0 min-w-0 flex-1 items-center gap-1 max-wide:flex-row wide:mt-5 wide:flex-col">
        <Link to="/" title="Bots" className={railHit(homeActive)}>
          {railBar(homeActive)}
          <LayoutGrid size={20} />
        </Link>
        <Link to="/skills" title="Skills" className={railHit(page === "skills")}>
          {railBar(page === "skills")}
          <Book size={20} />
        </Link>
        <span aria-hidden className="shrink-0 bg-line max-wide:mx-1 max-wide:h-6 max-wide:w-px wide:my-1.5 wide:h-px wide:w-6" />
        <div className="silo-scroll-x flex min-h-0 min-w-0 flex-1 gap-1 max-wide:flex-row max-wide:items-center max-wide:py-1 wide:flex-col wide:items-center wide:overflow-x-hidden wide:overflow-y-auto wide:px-3 wide:py-0.5">
          {(bots ?? []).map((b) => {
            const on = b.id === activeBotId;
            return (
              <Link
                key={b.id}
                to={`/bots/${b.id}/run`}
                title={b.name}
                className={railHit(on, `blink ${b.status === "working" ? "blink-idle" : ""}`)}
              >
                {railBar(on)}
                <span className="relative flex h-7 w-7">
                  <Crest index={b.crest} size={28} />
                  <Lamp status={b.status} onCrest={on ? "surface" : "well"} className="absolute -right-0.5 -bottom-0.5" />
                </span>
              </Link>
            );
          })}
          <Link to="/new" title="New Bot" className={railHit(loc.pathname === "/new")}>
            {railBar(loc.pathname === "/new")}
            <Plus size={20} />
          </Link>
        </div>
      </nav>
      <div className="flex items-center gap-1 max-wide:pr-2 wide:mb-4 wide:flex-col">
        {admin && (
          <Link to="/admin" title="Admin" className={railHit(page === "admin")}>
            {railBar(page === "admin")}
            <Wrench size={20} />
          </Link>
        )}
        <Link to="/account" title={email ? `Account · ${email}` : "Account"} className={railHit(page === "account")}>
          {railBar(page === "account")}
          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-ink text-[12px] font-semibold text-white">{initial}</span>
        </Link>
        <button
          type="button"
          title="Sign out"
          className={railHit(false)}
          onClick={async () => {
            await ui.signOut({});
            setSession(null);
          }}
        >
          <LogOut size={18} />
        </button>
      </div>
    </aside>
  );
}

function Shell({ page, fill, children }: { page: "bots" | "admin" | "account" | "skills"; fill?: boolean; children: ReactNode }) {
  return (
    <div className="flex h-dvh overflow-hidden max-wide:flex-col">
      <Rail page={page} />
      <main className={`min-w-0 flex-1 ${fill ? "min-h-0 overflow-hidden" : "overflow-auto"}`}>{children}</main>
    </div>
  );
}

function SignIn() {
  const nav = useNavigate();
  const { setSession } = useAuth();
  const [email, setEm] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const r = await ui.signIn({ email, password });
      setSession({ email: r.user?.email ?? email, admin: r.user?.admin ?? false });
      nav("/");
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  return (
    <div className="flex min-h-dvh items-start justify-center bg-canvas px-4 pt-[18vh]">
      <form onSubmit={onSubmit} className="rise w-full max-w-[400px] rounded-card shadow-card bg-surface p-8">
        <div className="mb-7 flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-[8px] bg-well text-ink">
            <SiloGlyph className="h-5 w-5" />
          </span>
          <span className="text-[14px] font-medium">Silo Agent</span>
        </div>
        <h1 className="mb-6 text-[22px] leading-7 font-medium tracking-[-0.015em]">Sign in</h1>
        <label htmlFor="silo-email" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Email
        </label>
        <input
          id="silo-email"
          className={`${inputClass} mb-4`}
          value={email}
          onChange={(e) => setEm(e.target.value)}
          autoComplete="username"
          autoFocus
          required
        />
        <label htmlFor="silo-pass" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Password
        </label>
        <input
          id="silo-pass"
          type="password"
          className={`${inputClass} mb-6`}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
        {err && (
          <p role="alert" className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
            <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
            <span>{err}</span>
          </p>
        )}
        <Btn kind="primary" className="w-full justify-center" type="submit" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </Btn>
      </form>
    </div>
  );
}

function FolioSkeleton() {
  return (
    <div className="flex gap-4 rounded-card bg-surface p-5 shadow-card" aria-hidden>
      <div className="skeleton h-14 w-14 shrink-0 rounded-card" />
      <div className="min-w-0 flex-1 py-1">
        <div className="skeleton mb-2.5 h-4 w-28 rounded-xs" />
        <div className="skeleton mb-2.5 h-3 w-44 rounded-xs" />
        <div className="skeleton h-3 w-16 rounded-xs" />
      </div>
    </div>
  );
}

function BotsPage() {
  const { bots, err } = useBots();
  const loading = bots === null;
  const empty = !loading && bots.length === 0;
  return (
    <div className="p-4 wide:p-7">
      {!empty ? (
        <div className="mb-6 flex items-center justify-between gap-3">
          <div>
            <h1 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Bots</h1>
            <p className="mt-0.5 text-[12.5px] leading-[18px] text-ink-2">Machines you can open.</p>
          </div>
          <Link to="/new" className={btnClass("primary")}>
            <Plus size={16} />
            New Bot
          </Link>
        </div>
      ) : null}
      {err && (
        <p role="alert" className="mb-4 text-[13px] text-vermilion">
          {err}
        </p>
      )}
      {loading ? (
        <div className="grid grid-cols-1 gap-4 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
          <FolioSkeleton />
          <FolioSkeleton />
          <FolioSkeleton />
        </div>
      ) : empty ? (
        <div className="rise pt-[12vh]">
          <p className="mb-2 text-[40px] leading-[48px] font-medium tracking-[-0.02em]">No Bots yet</p>
          <p className="mb-7 text-[14px] text-ink-2">A Bot is its own machine. It does not share files with the others.</p>
          <Link to="/new" className={btnClass("primary")}>
            <Plus size={16} />
            New Bot
          </Link>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
          {bots.map((b, i) => (
            <Link
              key={b.id}
              to={`/bots/${b.id}/run`}
              style={{ animationDelay: `${Math.min(i * 45, 270)}ms` }}
              className="rise blink group relative flex items-center gap-4 overflow-hidden rounded-card bg-surface p-5 shadow-card transition-[box-shadow,transform] duration-[200ms] ease-quiet hover:-translate-y-px hover:shadow-float active:scale-[.995] active:duration-[70ms] motion-reduce:hover:translate-y-0"
            >
              {b.status === "needs_you" ? (
                <span aria-hidden className="absolute inset-y-0 left-0 w-[2px] bg-vermilion" />
              ) : null}
              <Crest index={b.crest} size={56} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-[15px] leading-5 font-semibold tracking-[-0.01em]">{b.name}</div>
                {b.description ? <div className="mt-0.5 truncate text-[12.5px] leading-[18px] text-ink-2">{b.description}</div> : null}
                <div className="mt-2 flex items-center gap-2">
                  <Lamp status={b.status} />
                  <StatusWord status={b.status} />
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function NewBotPage() {
  const nav = useNavigate();
  const { refresh } = useBots();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [crest, setCrest] = useState(randomCrest);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  async function create(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setErr("");
    try {
      const b = await ui.createBot({ name: name.trim(), crest, description: description.trim() });
      refresh();
      nav(`/bots/${b.id}/run`);
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  return (
    <div className="silo-page silo-page-sm rise">
      <h1 className="mb-6 text-[22px] leading-7 font-medium tracking-[-0.015em]">New Bot</h1>
      <form onSubmit={create}>
        <div className="mb-5 flex flex-col items-center gap-4">
          <Crest index={crest} size={88} />
          <span className="text-[12px] text-ink-3">Pick a crest for this machine</span>
        </div>
        <div className="mb-6">
          <CrestPicker value={crest} onChange={setCrest} />
        </div>
        <label htmlFor="bot-name" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Name
        </label>
        <input
          id="bot-name"
          className={`${inputClass} mb-4`}
          placeholder="Scout"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />
        <label htmlFor="bot-desc" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Description
        </label>
        <textarea
          id="bot-desc"
          className={`${textareaClass} mb-3 min-h-[72px]`}
          placeholder="What this machine is for"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <p className="mb-6 text-[13px] text-ink-2">A Bot is its own machine. It does not share files with the others.</p>
        {err && (
          <p role="alert" className="mb-3 text-[13px] text-vermilion">
            {err}
          </p>
        )}
        <div className="flex gap-2">
          <Btn kind="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? "Creating…" : "Create Bot"}
          </Btn>
          <Link to="/" className={btnClass("secondary")}>
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}

function chatBusy(events: Ev[]): boolean {
  const open = new Set<string>();
  for (const ev of events) {
    if (ev.runId && ev.kind === "user") open.add(ev.runId);
    if (ev.runId && (ev.kind === "done" || ev.kind === "error")) open.delete(ev.runId);
  }
  return open.size > 0;
}

function Hatch({ botId, live, visible }: { botId: string; live: boolean; visible: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const [phase, setPhase] = useState<"off" | "connecting" | "connected" | "lost">("off");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!live) {
      setPhase("off");
      return;
    }
    const el = ref.current;
    if (!el) return;
    let rfb: { disconnect: () => void } | null = null;
    let cancelled = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let hang: ReturnType<typeof setTimeout> | undefined;
    const backoff = Math.min(15000, 2000 * 2 ** Math.min(attempt, 3));
    const schedule = () => {
      if (!cancelled) retry = setTimeout(() => setAttempt((n) => n + 1), backoff);
    };
    setPhase("connecting");
    const start = window.setTimeout(() => {
      if (cancelled || !el.isConnected) return;
      (async () => {
        const mod = await import("@novnc/novnc");
        if (cancelled || !el.isConnected) return;
        const RFB = mod.default;
        const proto = location.protocol === "https:" ? "wss" : "ws";
        const next = new RFB(el, `${proto}://${location.host}/vnc?bot=${botId}`, { shared: true });
        next.scaleViewport = true;
        next.clipViewport = true;
        next.background = "var(--color-matte)";
        next.addEventListener("connect", () => {
          if (cancelled) return;
          clearTimeout(hang);
          setPhase("connected");
        });
        next.addEventListener("disconnect", () => {
          if (cancelled) return;
          setPhase("lost");
          schedule();
        });
        next.addEventListener("securityfailure", () => {
          if (cancelled) return;
          setPhase("lost");
          schedule();
        });
        rfb = next;
        hang = setTimeout(() => {
          if (cancelled) return;
          try {
            rfb?.disconnect();
          } catch {
            /* retry via disconnect */
          }
        }, 8000);
      })().catch((e) => {
        console.error(e);
        if (!cancelled) {
          setPhase("lost");
          schedule();
        }
      });
    }, 50);
    return () => {
      cancelled = true;
      window.clearTimeout(start);
      clearTimeout(retry);
      clearTimeout(hang);
      try {
        rfb?.disconnect();
      } catch {
        /* already closed */
      }
      el.replaceChildren();
    };
  }, [botId, live, attempt]);
  useEffect(() => {
    if (visible) window.dispatchEvent(new Event("resize"));
  }, [visible]);
  return (
    <div className="relative h-full min-h-0 w-full bg-matte">
      <div ref={ref} className="silo-hatch h-full min-h-[320px] w-full" />
      {phase !== "connected" && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center font-mono text-[12.5px] text-white/80">
          {phase === "off" ? "Desktop not connected" : phase === "connecting" ? "Opening desktop…" : "Desktop lost"}
        </div>
      )}
    </div>
  );
}

function MachinePane({
  bot,
  onStart,
  visible,
  kind,
}: {
  bot: Bot;
  onStart: () => void;
  visible: boolean;
  kind: "desktop" | "console";
}) {
  const label = kind === "console" ? "Console" : "Desktop";
  if (!bot.workerConnected) {
    return (
      <NeedMachine
        copy={`${label} not connected`}
        starting={bot.status === "starting"}
        onStart={onStart}
      />
    );
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-panel bg-hatch">
      <div className="flex h-9 shrink-0 items-center gap-2 px-3 text-[13px] text-white/80">
        <Crest index={bot.crest} size={20} />
        <span className="min-w-0 truncate">{bot.name}</span>
        <span className="font-mono text-[12px] text-white/60">{label}</span>
        <Lamp status={bot.status} />
        <span className="ml-auto font-mono text-[12px] text-white/80">{statusText(bot.status)}</span>
      </div>
      <div className="mx-2 mb-2 min-h-0 flex-1 overflow-hidden rounded-sm bg-matte">
        {kind === "console" ? <ConsoleTerm botId={bot.id} live visible={visible} /> : <Hatch botId={bot.id} live visible={visible} />}
      </div>
    </div>
  );
}

// Under 1280px the header's Start/Stop shows only the power glyph.
const machineBtnCompact =
  "@max-[1280px]:w-10 @max-[1280px]:justify-center @max-[1280px]:gap-0 @max-[1280px]:px-0 @max-[1280px]:pr-0 @max-[1280px]:[&>span:last-child]:bg-transparent";

function FadeScroll({
  className = "",
  innerClass = "",
  fade = "from-canvas",
  innerRef,
  children,
}: {
  className?: string;
  innerClass?: string;
  fade?: "from-canvas" | "from-well";
  innerRef?: RefObject<HTMLDivElement | null>;
  children: ReactNode;
}) {
  const own = useRef<HTMLDivElement>(null);
  const ref = innerRef ?? own;
  const [edge, setEdge] = useState({ start: false, end: false });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const tick = () => {
      setEdge({
        start: el.scrollLeft > 1,
        end: el.scrollLeft + el.clientWidth < el.scrollWidth - 1,
      });
    };
    tick();
    el.addEventListener("scroll", tick, { passive: true });
    const ro = new ResizeObserver(tick);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", tick);
      ro.disconnect();
    };
  }, [ref]);
  function nudge(dir: -1 | 1) {
    const el = ref.current;
    if (!el) return;
    el.scrollBy({ left: dir * Math.max(160, el.clientWidth * 0.7), behavior: "smooth" });
  }
  return (
    <div className={`relative min-w-0 ${className}`}>
      <div ref={ref} className={`silo-scroll-x relative h-full ${innerClass}`}>
        {children}
      </div>
      {edge.start ? (
        <div className={`absolute inset-y-0 left-0 z-10 flex w-8 items-stretch bg-gradient-to-r ${fade} to-transparent`}>
          <button
            type="button"
            title="Previous"
            className="flex w-8 items-center justify-center text-ink-2 hover:text-ink"
            onClick={() => nudge(-1)}
          >
            <ChevronLeft size={14} />
          </button>
        </div>
      ) : null}
      {edge.end ? (
        <div className={`absolute inset-y-0 right-0 z-10 flex w-8 items-stretch justify-end bg-gradient-to-l ${fade} to-transparent`}>
          <button
            type="button"
            title="Next"
            className="flex w-8 items-center justify-center text-ink-2 hover:text-ink"
            onClick={() => nudge(1)}
          >
            <ChevronRight size={14} />
          </button>
        </div>
      ) : null}
    </div>
  );
}

function tabClass(on: boolean, compact?: boolean) {
  return `flex h-8 shrink-0 items-center gap-1.5 self-center whitespace-nowrap rounded-sm text-[13px] font-medium transition-[background-color,color] duration-[160ms] ease-quiet ${
    compact && !on ? "min-w-10 justify-center px-2" : "px-2.5"
  } ${on ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"}`;
}

function TabLink({
  to,
  on,
  icon: Icon,
  label,
  compact,
}: {
  to: string;
  on: boolean;
  icon: typeof MessageCircle;
  label: string;
  compact?: boolean;
}) {
  return (
    <NavLink to={to} title={label} data-tab-on={on || undefined} className={tabClass(on, compact)}>
      <Icon size={15} />
      {(!compact || on) && label}
    </NavLink>
  );
}

// One 2px cobalt underline that slides to the active tab. It measures the tab
// marked data-tab-on, and re-measures on resize and once fonts settle.
function TabUnderline({ strip, tab }: { strip: RefObject<HTMLDivElement | null>; tab: string }) {
  const [box, setBox] = useState<{ x: number; w: number } | null>(null);
  const [live, setLive] = useState(false);
  useLayoutEffect(() => {
    const el = strip.current;
    if (!el) return;
    const measure = () => {
      const on = el.querySelector<HTMLElement>("[data-tab-on]");
      if (!on) {
        setBox(null);
        return;
      }
      setBox((cur) => (cur && cur.x === on.offsetLeft && cur.w === on.offsetWidth ? cur : { x: on.offsetLeft, w: on.offsetWidth }));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    for (const c of Array.from(el.children)) ro.observe(c);
    let dead = false;
    void document.fonts?.ready.then(() => {
      if (!dead) measure();
    });
    return () => {
      dead = true;
      ro.disconnect();
    };
  }, [strip, tab]);
  useEffect(() => {
    // Skip the slide on first placement; animate every move after.
    if (!box || live) return;
    const r = requestAnimationFrame(() => setLive(true));
    return () => cancelAnimationFrame(r);
  }, [box, live]);
  if (!box) return null;
  return (
    <span
      aria-hidden
      className={`pointer-events-none absolute bottom-0 left-0 h-[2px] rounded-full bg-cobalt ${ live ? "transition-[transform,width] duration-[280ms] ease-quiet motion-reduce:transition-none" : ""
      }`}
      style={{ width: box.w, transform: `translateX(${box.x}px)` }}
    />
  );
}

function TabStrip({ className, innerClass, children, tab }: { className: string; innerClass: string; children: ReactNode; tab: string }) {
  const strip = useRef<HTMLDivElement>(null);
  return (
    <FadeScroll className={className} innerClass={innerClass} innerRef={strip}>
      {children}
      <TabUnderline strip={strip} tab={tab} />
    </FadeScroll>
  );
}

function BotTabs({
  id,
  tab,
  chatId,
  splitMachine,
  compact,
}: {
  id: string;
  tab: Tab;
  chatId?: string;
  splitMachine?: boolean;
  compact?: boolean;
}) {
  return (
    <>
      {tabs.map((t) => {
        if (t === "desktop") {
          if (splitMachine) {
            return (
              <span key="machine" className="contents">
                <TabLink to={`/bots/${id}/desktop`} on={tab === "desktop"} icon={Monitor} label="Desktop" compact={compact} />
                <TabLink to={`/bots/${id}/console`} on={tab === "console"} icon={SquareTerminal} label="Console" compact={compact} />
              </span>
            );
          }
          return <MachineNav key="desktop" id={id} tab={tab} />;
        }
        const { label, icon } = tabMeta[t];
        return (
          <TabLink
            key={t}
            to={t === "run" && chatId ? `/bots/${id}/run/${chatId}` : `/bots/${id}/${t}`}
            on={tab === t}
            icon={icon}
            label={label}
            compact={compact}
          />
        );
      })}
    </>
  );
}

function MachineNav({ id, tab }: { id: string; tab: Tab }) {
  const wrap = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [box, setBox] = useState({ top: 0, left: 0 });
  const onMachine = tab === "desktop" || tab === "console";
  const onConsole = tab === "console";
  const label = onConsole ? "Console" : "Desktop";
  const Icon = onConsole ? SquareTerminal : Monitor;
  const href = onConsole ? `/bots/${id}/console` : `/bots/${id}/desktop`;
  const other = onConsole ? `/bots/${id}/desktop` : `/bots/${id}/console`;
  const otherLabel = onConsole ? "Desktop" : "Console";
  const OtherIcon = onConsole ? Monitor : SquareTerminal;
  useEffect(() => {
    if (!open) return;
    const r = wrap.current?.getBoundingClientRect();
    if (r) setBox({ top: r.bottom + 6, left: r.left });
    const close = () => setOpen(false);
    const onDoc = (e: MouseEvent) => {
      const t = e.target;
      if (!(t instanceof Node)) return;
      if (wrap.current?.contains(t) || menu.current?.contains(t)) return;
      close();
    };
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    document.addEventListener("mousedown", onDoc);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      document.removeEventListener("mousedown", onDoc);
    };
  }, [open]);
  return (
    <div
      ref={wrap}
      data-tab-on={onMachine || undefined}
      className={`group/machine flex h-8 shrink-0 items-stretch self-center rounded-sm transition-colors duration-[160ms] ease-quiet ${ onMachine ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"
      }`}
    >
      <NavLink to={href} className="flex shrink-0 items-center gap-1.5 whitespace-nowrap pr-1 pl-2.5 text-[13px] font-medium">
        <Icon size={15} />
        {label}
      </NavLink>
      <button
        type="button"
        title={otherLabel}
        aria-expanded={open}
        className="flex items-center rounded-sm pr-2 pl-0.5"
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronDown size={12} className={`transition-transform duration-[200ms] ease-quiet ${open ? "rotate-180" : ""}`} />
      </button>
      {open &&
        createPortal(
          <div
            ref={menu}
            className="rise z-50 w-44 rounded-control bg-surface p-1 shadow-slip"
            style={{ position: "fixed", top: box.top, left: box.left }}
          >
            <Link
              to={other}
              onClick={() => setOpen(false)}
              className="flex h-8 items-center gap-2 rounded-sm px-2.5 text-[13px] font-medium text-ink transition-colors duration-[160ms] hover:bg-well"
            >
              <OtherIcon size={15} />
              {otherLabel}
            </Link>
          </div>,
          document.body,
        )}
    </div>
  );
}

function n64(v: bigint | number | undefined) {
  if (typeof v === "bigint") return Number(v);
  return v ?? 0;
}

function fmtBytes(n: number) {
  if (n <= 0) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let x = n;
  while (x >= 1024 && i < u.length - 1) {
    x /= 1024;
    i++;
  }
  return `${x < 10 && i > 0 ? x.toFixed(1) : Math.round(x)} ${u[i]}`;
}

function Meter({ value, max }: { value: number; max: number }) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  return (
    <div className="h-1.5 overflow-hidden rounded-full bg-pressed">
      <div className="h-full rounded-full bg-ink transition-[width] duration-[280ms] ease-quiet" style={{ width: `${pct}%` }} />
    </div>
  );
}

function ContainerPane({
  bot,
  onStart,
  onStop,
}: {
  bot: Bot;
  onStart: () => void;
  onStop: () => void;
}) {
  const [box, setBox] = useState<Container | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    const load = () => {
      ui.getContainer({ id: bot.id })
        .then((c) => {
          if (!dead) {
            setBox(c);
            setErr("");
          }
        })
        .catch((e) => {
          if (!dead) setErr(fail(e));
        });
    };
    load();
    const t = setInterval(load, 2000);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [bot.id]);
  const mem = n64(box?.memUsed);
  const cap = n64(box?.memLimit);
  const cpu = box?.cpuPercent ?? 0;
  return (
    <div className="silo-page">
      <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Container</h2>
      <p className="mb-6 text-ink-2">This Bot’s machine. Usage from Docker.</p>
      {err && <p className="mb-4 text-vermilion">{err}</p>}
      <div className="mb-6 flex items-center gap-3">
        <Lamp status={bot.status} />
        <StatusWord status={bot.status} />
      </div>
      <div className="mb-6 grid max-w-[560px] gap-4">
        <div className="rounded-card bg-surface p-5 shadow-card">
          <div className="mb-1 text-[11px] leading-4 font-medium tracking-[0.08em] text-ink-3 uppercase">CPU</div>
          <div className="mb-2 font-mono text-[20px] font-medium tabular-nums tracking-tight">
            {box?.running ? `${cpu.toFixed(1)}%` : "—"}
          </div>
          <Meter value={box?.running ? cpu : 0} max={100} />
        </div>
        <div className="rounded-card bg-surface p-5 shadow-card">
          <div className="mb-1 text-[11px] leading-4 font-medium tracking-[0.08em] text-ink-3 uppercase">RAM</div>
          <div className="mb-2 font-mono text-[20px] font-medium tabular-nums tracking-tight">
            {box?.running ? `${fmtBytes(mem)}${cap ? ` / ${fmtBytes(cap)}` : ""}` : "—"}
          </div>
          <Meter value={mem} max={cap} />
        </div>
      </div>
      {bot.workerConnected ? (
        <Btn kind="secondary" onClick={onStop} icon={<Power size={12} />}>
          Stop Bot
        </Btn>
      ) : (
        <Btn kind="primary" onClick={onStart} disabled={bot.status === "starting"} icon={<Power size={12} />}>
          {bot.status === "starting" ? "Starting…" : "Start Bot"}
        </Btn>
      )}
    </div>
  );
}

function BotPage() {
  const { id, "*": splat } = useParams();
  const nav = useNavigate();
  const { refresh } = useBots();
  const parts = (splat ?? "").split("/").filter(Boolean);
  const tabParam = parts[0];
  const chatId = tabParam === "run" ? parts[1] : undefined;
  const tab: Tab = chatId ? "run" : tabParam === "console" ? "console" : isNavTab(tabParam) ? tabParam : "run";
  const [bot, setBot] = useState<Bot | null>(null);
  const [loadErr, setLoadErr] = useState("");
  const [text, setText] = useState("");
  const [events, setEvents] = useState<Ev[]>([]);
  const [sending, setSending] = useState(false);
  const [pending, setPending] = useState<Approval[]>([]);
  const [authPrompt, setAuthPrompt] = useState<BotConnector | null>(null);
  const [secrets, setSecrets] = useState<SecretMeta[] | null>(null);
  const secretSaver = useSave();
  const [chats, setChats] = useState<Chat[]>([]);
  const [editingChat, setEditingChat] = useState("");
  const [editTitle, setEditTitle] = useState("");
  const renameCancel = useRef(false);
  const [secName, setSecName] = useState("");
  const [secVal, setSecVal] = useState("");
  const [actErr, setActErr] = useState("");
  const [atts, setAtts] = useState<{ name: string; path: string; size: number }[]>([]);
  const [attachErr, setAttachErr] = useState("");
  const [keepDesk, setKeepDesk] = useState(tab === "desktop");
  const [keepCon, setKeepCon] = useState(tab === "console");
  const [inspect, setInspect] = useState<Artifact | null>(null);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [defaultModel, setDefaultModel] = useState("");
  const [usage, setUsage] = useState<{ input: number; output: number; cacheRead: number; cacheWrite: number } | null>(null);
  const [streamNonce, setStreamNonce] = useState(0);
  // User messages sent from this tab glide in; history does not animate.
  const sentAt = useRef(0);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(() => new Set());

  useEffect(() => {
    setKeepDesk(tab === "desktop");
    setKeepCon(tab === "console");
    setAuthPrompt(null);
  }, [id]);
  useEffect(() => {
    if (tab === "desktop") setKeepDesk(true);
    if (tab === "console") setKeepCon(true);
  }, [tab]);

  const settled = Boolean(bot && (bot.workerConnected || bot.status === "stopped"));
  useEffect(() => {
    if (!id) return;
    let dead = false;
    const tick = (first = false) => {
      ui.getBot({ id })
        .then((b) => {
          if (!dead) setBot(b);
        })
        .catch((e) => {
          if (!dead && first) setLoadErr(fail(e));
        });
      ui.listApprovals({ botId: id })
        .then((r) => {
          if (!dead) setPending(r.approvals);
        })
        .catch(() => {});
    };
    tick(true);
    const t = setInterval(() => tick(), settled ? 3000 : 500);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [id, settled]);

  useEffect(() => {
    if (!id || tab !== "run") return;
    let dead = false;
    ui.listChats({ botId: id })
      .then((r) => {
        if (dead) return;
        setChats(r.chats);
        if (!chatId && r.chats[0]) nav(`/bots/${id}/run/${r.chats[0].id}`, { replace: true });
      })
      .catch(() => {});
    ui.listModels({ botId: id })
      .then((r) => {
        if (!dead) {
          setModels(r.models);
          setDefaultModel(r.defaultModel);
        }
      })
      .catch(() => {});
    return () => {
      dead = true;
    };
  }, [id, tab, chatId, nav]);

  useEffect(() => {
    setUsage(null);
  }, [chatId]);

  useEffect(() => {
    if (!id || !chatId) return;
    let dead = false;
    let ac = new AbortController();
    let after = "";
    const seen = new Set<string>();
    setEvents([]);
    setSending(false);
    const push = (ev: Ev) => {
      setEvents((xs) => {
        const next = [...xs, ev];
        setSending(chatBusy(next));
        return next;
      });
    };
    (async () => {
      while (!dead) {
        ac = new AbortController();
        try {
          for await (const ev of ui.streamRun({ botId: id, chatId, afterEventId: after }, { signal: ac.signal })) {
            if (ev.id) {
              if (seen.has(ev.id)) continue;
              seen.add(ev.id);
              after = ev.id;
            }
            if (ev.kind === "approval") {
              const list = await ui.listApprovals({ botId: id });
              if (!dead) setPending(list.approvals);
            }
            if (ev.kind === "chat_title" && ev.body) {
              setChats((xs) => xs.map((c) => (c.id === chatId ? { ...c, title: ev.body } : c)));
            }
            if (ev.kind === "usage" && ev.body) {
              try {
                const u = JSON.parse(ev.body) as Record<string, number>;
                if (!dead) {
                  setUsage({
                    input: u.input ?? 0,
                    output: u.output ?? 0,
                    cacheRead: u.cache_read ?? 0,
                    cacheWrite: u.cache_write ?? 0,
                  });
                }
              } catch {
                /* ignore malformed usage */
              }
            }
            if (ev.kind === "done") {
              ui.listChats({ botId: id }).then((r) => {
                if (!dead) setChats(r.chats);
              }).catch(() => {});
            }
            if (ev.kind === "reset") {
              // History was truncated by an edit/delete elsewhere; drop the
              // cached events and replay from scratch.
              setEvents([]);
              setSending(false);
              setStreamNonce((n) => n + 1);
              return;
            }
            if (ev.kind === "user" && ev.id && sentAt.current && Date.now() - sentAt.current < 15000) {
              sentAt.current = 0;
              const sid = ev.id;
              setFresh((xs) => new Set(xs).add(sid));
            }
            if (!dead) push({ id: ev.id, kind: ev.kind, body: ev.body, tool: ev.tool, runId: ev.runId, at: Date.now(), attachments: ev.attachments.map((a) => ({ name: a.name, path: a.path, size: Number(a.size) })) });
          }
        } catch {
          if (!dead) setSending(false);
        }
        if (dead) return;
        await new Promise((r) => setTimeout(r, 800));
      }
    })();
    ui.listApprovals({ botId: id }).then((r) => {
      if (!dead) setPending(r.approvals);
    }).catch(() => {});
    return () => {
      dead = true;
      ac.abort();
    };
  }, [id, chatId, streamNonce]);

  useEffect(() => {
    if (!id || tab !== "secrets") return;
    ui.listSecrets({ botId: id }).then((r) => setSecrets(r.secrets)).catch((e) => {
      setSecrets([]);
      setActErr(fail(e));
    });
  }, [id, tab]);

  if (!id) return <Navigate to="/" />;
  if (!tabParam) return <Navigate to={`/bots/${id}/run`} replace />;
  if (!chatId && tabParam && tabParam !== "console" && !isNavTab(tabParam)) return <Navigate to={`/bots/${id}/run`} replace />;
  if (loadErr) {
    return (
      <div className="p-7">
        <p className="mb-3 text-vermilion">{loadErr}</p>
        <Link to="/" className="text-cobalt">
          Back to Bots
        </Link>
      </div>
    );
  }
  if (!bot) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 items-center gap-3 px-4 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14">
          <div className="skeleton hidden h-7 w-7 rounded-sm wide:block" />
          <div className="skeleton hidden h-4 w-32 rounded-xs wide:block" />
        </div>
        <div className="flex min-h-0 flex-1">
          <div className="hidden w-[248px] shrink-0 bg-well p-2 pt-12 wide:block">
            <SkeletonRows rows={4} height={52} />
          </div>
          <div className="flex-1" />
        </div>
      </div>
    );
  }

  const chatRuns = new Set(events.map((e) => e.runId).filter(Boolean));
  const waiting = pending.some((p) => p.runId && chatRuns.has(p.runId));

  async function send(e?: FormEvent) {
    e?.preventDefault();
    if (!id || (!text.trim() && atts.length === 0)) return;
    const msg = text.trim();
    setText("");
    setSending(true);
    sentAt.current = Date.now();
    setActErr("");
    try {
      await ui.send({
        botId: id,
        chatId: chatId || "",
        text: msg,
        attachments: atts.map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
      });
      setAtts([]);
      ui.getBot({ id }).then(setBot);
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
    } catch (ex) {
      setSending(false);
      setActErr(fail(ex));
    }
  }

  function resync() {
    setEvents([]);
    setSending(false);
    setStreamNonce((n) => n + 1);
  }

  async function editMessage(eventId: string, text: string, attachments?: { name: string; path: string; size: number }[]) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      await ui.editMessage({
        botId: id,
        chatId,
        eventId,
        text,
        attachments: (attachments ?? []).map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
      });
      resync();
      ui.getBot({ id }).then(setBot);
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function deleteMessage(eventId: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      await ui.deleteMessage({ botId: id, chatId, eventId });
      resync();
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function divergeChat(eventId: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      const res = await ui.divergeChat({ botId: id, chatId, eventId });
      if (!res.chat) return;
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
      nav(`/bots/${id}/run/${res.chat.id}`);
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function attach(list: FileList | null) {
    if (!list?.length || !id || !bot?.workerConnected) return;
    setAttachErr("");
    const added: { name: string; path: string; size: number }[] = [];
    for (const f of list) {
      if (f.size > 50 << 20) {
        setAttachErr(`${f.name} is larger than 50 MB`);
        continue;
      }
      const path = joinPath("tmp", f.name);
      if (!path) continue;
      try {
        const buf = new Uint8Array(await f.arrayBuffer());
        await ui.putFile({ botId: id, path, data: buf });
        added.push({ name: f.name, path, size: f.size });
      } catch (ex) {
        setAttachErr(fail(ex));
      }
    }
    if (added.length) {
      setAtts((xs) => {
        const seen = new Set(xs.map((x) => x.path));
        return [...xs, ...added.filter((a) => !seen.has(a.path))];
      });
    }
  }

  async function pickModel(model: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      const c = await ui.setChatModel({ botId: id, chatId, model });
      setChats((xs) => xs.map((x) => (x.id === c.id ? c : x)));
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function stopRun() {
    if (!id || !chatId) return;
    setActErr("");
    try {
      await ui.stopRun({ botId: id, chatId });
      ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {});
      ui.getBot({ id }).then(setBot).catch(() => {});
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function start() {
    if (!id) return;
    setActErr("");
    try {
      setBot(await ui.startBot({ id }));
      refresh();
    } catch (e) {
      setActErr(fail(e));
    }
  }
  async function stop() {
    if (!id) return;
    setActErr("");
    try {
      setBot(await ui.stopBot({ id }));
      refresh();
    } catch (e) {
      setActErr(fail(e));
    }
  }

  async function saveSkill(a: Artifact) {
    if (!id) return;
    try {
      await ui.saveSkill({ botId: id, path: a.path, runId: a.runId ?? "" });
      setInspect((cur) => (cur && cur.path === a.path ? { ...cur, status: "saved", scope: "personal" } : cur));
    } catch (e) {
      setActErr(fail(e));
    }
  }

  async function renameChat(cid: string) {
    if (!id) return;
    const title = editTitle.trim();
    setEditingChat("");
    if (!title) return;
    const cur = chats.find((x) => x.id === cid);
    if (cur && cur.title === title) return;
    try {
      const row = await ui.renameChat({ botId: id, id: cid, title });
      setChats((xs) => xs.map((c) => (c.id === cid ? row : c)));
    } catch (e) {
      setActErr(fail(e));
    }
  }

  async function newChat() {
    if (!id) return;
    const c = await ui.createChat({ botId: id });
    setChats((xs) => [c, ...xs]);
    nav(`/bots/${id}/run/${c.id}`);
  }

  async function deleteChat(cid: string) {
    if (!id) return;
    await ui.deleteChat({ botId: id, id: cid });
    const next = chats.filter((x) => x.id !== cid);
    setChats(next);
    if (cid === chatId) {
      if (next[0]) nav(`/bots/${id}/run/${next[0].id}`);
      else {
        const created = await ui.createChat({ botId: id });
        setChats([created]);
        nav(`/bots/${id}/run/${created.id}`);
      }
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="@container flex h-12 shrink-0 items-center gap-1 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:gap-3 wide:px-4">
        <span className="blink hidden wide:inline-flex">
          <Crest index={bot.crest} size={28} />
        </span>
        <h1 className="sr-only min-w-0 truncate text-[15px] leading-5 font-semibold tracking-[-0.01em] wide:not-sr-only wide:max-w-[12rem]">{bot.name}</h1>
        <span className="hidden shrink-0 items-center gap-2 wide:inline-flex">
          <Lamp status={bot.status} />
          <StatusWord status={bot.status} />
        </span>
        <TabStrip tab={tab} className="hidden min-h-0 flex-1 self-stretch wide:ml-2 wide:block" innerClass="flex h-full items-stretch gap-0.5">
          <BotTabs id={id} tab={tab} chatId={chatId} />
        </TabStrip>
        <TabStrip tab={tab} className="min-h-0 flex-1 self-stretch wide:hidden" innerClass="flex h-full items-stretch gap-0.5">
          <BotTabs id={id} tab={tab} chatId={chatId} splitMachine compact />
        </TabStrip>
        <div className="shrink-0">
          {bot.workerConnected ? (
            <Btn
              kind="ghost"
              title="Stop Bot"
              aria-label="Stop Bot"
              onClick={stop}
              className={machineBtnCompact}
              icon={<Power size={13} />}
            >
              <span className="@max-[1280px]:hidden">Stop Bot</span>
            </Btn>
          ) : (
            <Btn
              kind="ghost"
              title={bot.status === "starting" ? "Starting…" : "Start Bot"}
              aria-label={bot.status === "starting" ? "Starting" : "Start Bot"}
              onClick={start}
              disabled={bot.status === "starting"}
              className={machineBtnCompact}
              icon={<Power size={13} />}
            >
              <span className="@max-[1280px]:hidden">{bot.status === "starting" ? "Starting…" : "Start Bot"}</span>
            </Btn>
          )}
        </div>
      </header>
      {actErr && (
        <div role="alert" className="rise flex items-center gap-2 bg-vermilion-pale px-4 py-2 text-[13px] text-vermilion">
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
          <span className="min-w-0 flex-1">{actErr}</span>
          <button type="button" title="Dismiss" className="rounded-xs p-1 hover:bg-white/60" onClick={() => setActErr("")}>
            <X size={13} />
          </button>
        </div>
      )}
      <div
        className="relative flex min-h-0 flex-1"
        style={{ "--wash-left": tab === "run" ? "248px" : "0px" } as CSSProperties}
      >
        {tab === "run" && (
          <>
            <aside className="hidden w-[248px] shrink-0 flex-col bg-well wide:flex">
              <div className="flex h-12 items-center justify-between pr-2 pl-4">
                <span className="text-[11px] leading-4 font-medium tracking-[0.08em] text-ink-3 uppercase">Chats</span>
                <Btn
                  kind="ghost"
                  size="sm"
                  iconOnly
                  title="New chat"
                  aria-label="New chat"
                  icon={<SquarePen size={15} />}
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                />
              </div>
              <div className="min-h-0 flex-1 space-y-0.5 overflow-auto px-2 pb-3">
                {chats.length === 0 && <p className="px-2 py-2 text-[12.5px] text-ink-3">No chats</p>}
                {chats.map((c) => {
                  const on = c.id === chatId;
                  const live = on && (waiting || sending);
                  return (
                    <div
                      key={c.id}
                      className={`group relative rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${ on ? "bg-surface shadow-card" : "hover:bg-pressed"
                      }`}
                    >
                      {editingChat === c.id ? (
                        <div className="px-1.5 py-1.5">
                          <input
                            autoFocus
                            className={`${inputClass} h-8 px-2 text-[13.5px]`}
                            value={editTitle}
                            onChange={(e) => setEditTitle(e.target.value)}
                            onBlur={() => {
                              if (renameCancel.current) {
                                renameCancel.current = false;
                                return;
                              }
                              void renameChat(c.id);
                            }}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault();
                                void renameChat(c.id);
                              }
                              if (e.key === "Escape") {
                                renameCancel.current = true;
                                setEditingChat("");
                              }
                            }}
                          />
                        </div>
                      ) : (
                        <NavLink
                          to={`/bots/${id}/run/${c.id}`}
                          onDoubleClick={(e) => {
                            e.preventDefault();
                            setEditingChat(c.id);
                            setEditTitle(c.title || "");
                          }}
                          className="block min-w-0 rounded-control px-3 py-2 pr-14"
                        >
                          <span className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-[13.5px] leading-5 font-medium text-ink">{c.title || "New chat"}</span>
                            {live ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
                          </span>
                          <span className={`block truncate text-[12.5px] leading-[18px] ${waiting && on ? "text-vermilion" : "text-ink-3"}`}>
                            {on && waiting ? "Waiting for you" : on && sending ? "Working…" : when(c.updatedAt)}
                          </span>
                        </NavLink>
                      )}
                      {editingChat !== c.id && (
                        <span className="absolute top-1.5 right-1.5 hidden items-center group-focus-within:flex group-hover:flex">
                          <button
                            type="button"
                            title="Rename chat"
                            className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
                            onClick={(e) => {
                              e.preventDefault();
                              setEditingChat(c.id);
                              setEditTitle(c.title || "");
                            }}
                          >
                            <Pencil size={13} />
                          </button>
                          <button
                            type="button"
                            title="Delete chat"
                            className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-vermilion-pale hover:text-vermilion"
                            onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                          >
                            <Trash2 size={13} />
                          </button>
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            </aside>
            <section className="flex min-w-0 flex-1 flex-col">
              <FadeScroll className="shrink-0 bg-well shadow-[inset_0_-1px_0_var(--color-line)] wide:hidden" innerClass="flex items-center gap-1 px-2 py-1.5" fade="from-well">
                <Btn
                  kind="ghost"
                  size="sm"
                  iconOnly
                  title="New chat"
                  aria-label="New chat"
                  className="h-10 w-10"
                  icon={<SquarePen size={15} />}
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                />
                {chats.map((c) => {
                  const on = c.id === chatId;
                  return (
                    <div
                      key={c.id}
                      className={`flex h-10 shrink-0 items-center rounded-control text-[13px] font-medium ${ on ? "bg-surface text-ink shadow-card" : "text-ink-2"
                      }`}
                    >
                      {editingChat === c.id ? (
                        <input
                          autoFocus
                          className={`${inputClass} h-8 w-40 px-2`}
                          value={editTitle}
                          onChange={(e) => setEditTitle(e.target.value)}
                          onBlur={() => {
                            if (renameCancel.current) {
                              renameCancel.current = false;
                              return;
                            }
                            void renameChat(c.id);
                          }}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              e.preventDefault();
                              void renameChat(c.id);
                            }
                            if (e.key === "Escape") {
                              renameCancel.current = true;
                              setEditingChat("");
                            }
                          }}
                        />
                      ) : (
                        <NavLink
                          to={`/bots/${id}/run/${c.id}`}
                          onDoubleClick={(e) => {
                            e.preventDefault();
                            setEditingChat(c.id);
                            setEditTitle(c.title || "");
                          }}
                          className={`flex h-10 max-w-[11rem] items-center gap-2 px-3 ${on ? "" : "rounded-control hover:bg-pressed hover:text-ink"}`}
                        >
                          <span className="truncate">{c.title || "New chat"}</span>
                          {on && (waiting || sending) ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
                        </NavLink>
                      )}
                      {on && editingChat !== c.id && (
                        <button
                          type="button"
                          title="Rename chat"
                          className="flex h-10 w-8 items-center justify-center text-ink-2 hover:text-ink"
                          onClick={(e) => {
                            e.preventDefault();
                            setEditingChat(c.id);
                            setEditTitle(c.title || "");
                          }}
                        >
                          <Pencil size={13} />
                        </button>
                      )}
                      {on && (
                        <button
                          type="button"
                          title="Delete chat"
                          className="flex h-10 w-8 items-center justify-center text-ink-2 hover:text-vermilion"
                          onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                        >
                          <Trash2 size={13} />
                        </button>
                      )}
                    </div>
                  );
                })}
              </FadeScroll>
              <Thread
                botId={id!}
                botName={bot.name}
                botCrest={bot.crest}
                chatId={chatId}
                events={events}
                sending={sending}
                fresh={fresh}
                onInspectArtifact={setInspect}
                onSaveSkill={(a) => void saveSkill(a)}
                onSelectPrompt={(p) => setText(p)}
                onEditMessage={(eid, t, a) => void editMessage(eid, t, a)}
                onDeleteMessage={(eid) => void deleteMessage(eid)}
                onDivergeChat={(eid) => void divergeChat(eid)}
              />
              <Composer
                text={text}
                setText={setText}
                atts={atts}
                onRemoveAtt={(path) => setAtts((xs) => xs.filter((x) => x.path !== path))}
                attachErr={attachErr}
                onAttach={attach}
                onSend={() => void send()}
                onStop={() => void stopRun()}
                sending={sending}
                chatId={chatId}
                workerConnected={bot.workerConnected}
                botName={bot.name}
                models={models}
                model={chats.find((c) => c.id === chatId)?.model || defaultModel}
                onModel={(m) => void pickModel(m)}
                usage={usage}
              />
            </section>
          </>
        )}
        {tab === "desktop" || keepDesk ? (
          <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "desktop" ? "flex" : "hidden"}`}>
            <MachinePane bot={bot} onStart={start} visible={tab === "desktop"} kind="desktop" />
            <p className="mt-2 text-[12.5px] text-ink-2">Same browser the Bot uses. You can type and click.</p>
          </section>
        ) : null}
        {tab === "console" || keepCon ? (
          <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "console" ? "flex" : "hidden"}`}>
            <MachinePane bot={bot} onStart={start} visible={tab === "console"} kind="console" />
            <p className="mt-2 text-[12.5px] text-ink-2">A shell on this Bot, started in /workspace.</p>
          </section>
        ) : null}
        {tab === "files" && (
          <section className="flex min-h-0 min-w-0 flex-1 flex-col">
            <FilesPane bot={bot} onStart={start} />
          </section>
        )}
        {tab === "connectors" && id && (
          <section className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotConnectors botId={id} onNeedAuth={setAuthPrompt} />
          </section>
        )}
        {tab === "channels" && id && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotChannels botId={id} sub={parts.slice(1)} />
          </div>
        )}
        {tab === "skills" && id && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotSkills botId={id} />
          </div>
        )}
        {tab === "secrets" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
          <div className="silo-page">
            <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Secrets</h2>
            <p className="mb-6 text-ink-2">Handed to the Bot only after you allow it. Masked before the model sees output.</p>
            <Panel title="Stored secrets" padded={false}>
              {secrets === null ? (
                <SkeletonRows rows={2} height={52} className="p-3" />
              ) : secrets.length === 0 ? (
                <p className="px-5 py-4 text-[13px] text-ink-3">No secrets on this Bot yet.</p>
              ) : (
                secrets.map((s) => (
                  <div key={s.id} className="flex items-center justify-between gap-3 px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
                    <div className="min-w-0">
                      <div className="truncate font-mono text-[13px] font-medium">{s.name}</div>
                      <div className="font-mono text-[12px] text-ink-3">•••••••• · {s.lastUsedAt || "never used"}</div>
                    </div>
                    <ArmedButton
                      kind="ghost"
                      size="sm"
                      onConfirm={async () => {
                        await ui.deleteSecret({ botId: id, id: s.id });
                        setSecrets((xs) => (xs ?? []).filter((x) => x.id !== s.id));
                      }}
                    >
                      Delete
                    </ArmedButton>
                  </div>
                ))
              )}
            </Panel>
            <Panel title="Add a secret" className="mt-4">
              <form
                className="flex flex-col gap-3 wide:flex-row wide:items-end"
                onSubmit={async (e) => {
                  e.preventDefault();
                  try {
                    await secretSaver.run(() => ui.addSecret({ botId: id, name: secName, value: secVal }));
                    setSecName("");
                    setSecVal("");
                    setSecrets((await ui.listSecrets({ botId: id })).secrets);
                  } catch (ex) {
                    setActErr(fail(ex));
                  }
                }}
              >
                <Field label="Name" className="flex-1">
                  <input className={`${inputClass} font-mono`} placeholder="vendor_password" value={secName} onChange={(e) => setSecName(e.target.value)} />
                </Field>
                <Field label="Value" className="flex-1">
                  <input type="password" className={`${inputClass} font-mono`} placeholder="••••••••" value={secVal} onChange={(e) => setSecVal(e.target.value)} />
                </Field>
                <SaveButton type="submit" state={secretSaver.state} savedLabel="Added" disabled={!secName.trim() || !secVal}>
                  Add
                </SaveButton>
              </form>
            </Panel>
          </div>
          </div>
        )}
        {tab === "container" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <ContainerPane bot={bot} onStart={start} onStop={stop} />
          </div>
        )}
        {tab === "memories" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <MemoriesPane bot={bot} onSaved={setBot} onError={setActErr} />
          </div>
        )}
        {tab === "settings" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <SettingsPane
              bot={bot}
              onSaved={(next) => {
                setBot(next);
                refresh();
              }}
              onError={setActErr}
              onRefresh={refresh}
            />
          </div>
        )}
        {tab === "rules" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <RulesPane botId={id} />
          </div>
        )}
        <SlipPresence show={!!(pending[0] || authPrompt?.connector)}>
          {pending[0] ? (
            <ApprovalSlip
              key={pending[0].id}
              bot={bot}
              approval={pending[0]}
              onDecide={async (decision) => {
                await ui.decideApproval({ id: pending[0].id, decision });
                setPending((xs) => xs.slice(1));
              }}
              onAutoApprove={async () => {
                try {
                  await ui.setRule({
                    botId: bot.id,
                    connector: pending[0].connector,
                    action: pending[0].action,
                    decision: "auto",
                  });
                  await ui.decideApproval({ id: pending[0].id, decision: "allow_once" });
                  setPending((xs) => xs.slice(1));
                } catch (e) {
                  setActErr(fail(e));
                }
              }}
            />
          ) : authPrompt?.connector ? (
            <ConnectorAuthSlip
              bot={bot}
              name={authPrompt.connector.name}
              onAuthorize={async () => {
                try {
                  await startConnectorAuth(bot.id, authPrompt.id);
                  setAuthPrompt(null);
                } catch (e) {
                  setActErr(fail(e));
                }
              }}
              onLater={() => setAuthPrompt(null)}
            />
          ) : null}
        </SlipPresence>
        {inspect && id ? (
          <ArtifactOverlay botId={id} artifact={inspect} onSave={(a) => void saveSkill(a)} onClose={() => setInspect(null)} />
        ) : null}
      </div>
    </div>
  );
}

function AdminGate() {
  const { admin } = useAuth();
  if (!admin) return <Navigate to="/" replace />;
  return <AdminLayout />;
}

function Authed() {
  const { email } = useAuth();
  return (
    <BotsProvider>
      <Routes>
        <Route
          path="/"
          element={
            <Shell page="bots">
              <BotsPage />
            </Shell>
          }
        />
        <Route
          path="/new"
          element={
            <Shell page="bots">
              <NewBotPage />
            </Shell>
          }
        />
        <Route
          path="/skills"
          element={
            <Shell page="skills">
              <SkillHub />
            </Shell>
          }
        />
        <Route
          path="/bots/:id/*"
          element={
            <Shell page="bots" fill>
              <BotPage />
            </Shell>
          }
        />
        <Route
          path="/admin"
          element={
            <Shell page="admin">
              <AdminGate />
            </Shell>
          }
        >
          <Route index element={<Navigate to="settings" replace />} />
          <Route path="settings" element={<AdminSettings />} />
          <Route path="connectors/*" element={<AdminConnectors />} />
          <Route path="skills" element={<AdminSkills />} />
          <Route path="search-extract" element={<AdminSearchExtract />} />
          <Route path="debug" element={<AdminDebug />} />
        </Route>
        <Route
          path="/account"
          element={
            <Shell page="account">
              <AccountPage email={email} />
            </Shell>
          }
        />
        <Route path="/signin" element={<Navigate to="/" />} />
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </BotsProvider>
  );
}

export default function App() {
  const [session, setSession] = useState<{ email: string; admin: boolean } | null | undefined>(undefined);
  useEffect(() => {
    ui.me({})
      .then((r) => setSession(r.user ? { email: r.user.email, admin: r.user.admin } : null))
      .catch(() => setSession(null));
  }, []);
  if (session === undefined) return null;
  return (
    <AuthCtx.Provider value={{ email: session?.email ?? "", admin: session?.admin ?? false, setSession }}>
      {session === null ? (
        <Routes>
          <Route path="/signin" element={<SignIn />} />
          <Route path="*" element={<Navigate to="/signin" />} />
        </Routes>
      ) : (
        <Authed />
      )}
    </AuthCtx.Provider>
  );
}
