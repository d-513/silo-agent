import { ArrowUp, Book, Box, ChevronDown, ChevronLeft, ChevronRight, Folder, Key, LayoutGrid, ListChecks, LogOut, MessageCircle, Monitor, Paperclip, Pencil, Plug, Plus, Power, Radio, SlidersHorizontal, Square, SquareTerminal, Trash2, User, Wrench, X } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Link, Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { COLOR_COUNT, Crest, CrestPicker, packCrest, SHAPE_COUNT } from "./Crest";
import { ArtifactOverlay, type Artifact } from "./Artifact";
import { ApprovalSlip, ConnectorAuthSlip } from "./Approval";
import { ConsoleTerm } from "./Console";
import { FilesPane } from "./Files";
import { inputClass, textareaClass } from "./Field";
import { joinPath } from "./fs";
import { NeedMachine } from "./NeedMachine";
import { Thread, type Ev } from "./Thread";
import { Composer } from "./Composer";
import { AdminLayout, AccountPage, AdminDebug, AdminSettings, AdminSearchExtract } from "./Admin";
import { SettingsPane } from "./Settings";
import { AdminConnectors } from "./AdminConnectors";
import { BotConnectors, startConnectorAuth } from "./BotConnectors";
import { BotChannels } from "./BotChannels";
import { RulesPane } from "./Rules";
import { AdminSkills, BotSkills, SkillHub } from "./Skills";
import type { Approval, Bot, BotConnector, Chat, Container, ModelOption, SecretMeta } from "./gen/silo/v1/ui_pb";

const tabs = ["run", "desktop", "files", "connectors", "channels", "skills", "secrets", "rules", "container", "settings"] as const;
type NavTab = (typeof tabs)[number];
type Tab = NavTab | "console";

function isNavTab(s: string | undefined): s is NavTab {
  return !!s && (tabs as readonly string[]).includes(s);
}

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function lampClass(status: string) {
  if (status === "working") return "bg-pine lamp-working";
  if (status === "needs_you") return "bg-carmine";
  if (status === "online" || status === "idle") return "bg-pine";
  if (status === "stopped" || status === "starting") return "bg-thread";
  return "bg-stone";
}

function statusWord(status: string) {
  if (status === "working" || status === "online" || status === "idle") return "text-pine";
  if (status === "needs_you") return "text-carmine";
  return "text-stone";
}

function statusLabel(status: string) {
  if (status === "idle") return "online";
  return status.replaceAll("_", " ");
}

const tabMeta: Record<NavTab, { label: string; icon: typeof MessageCircle }> = {
  run: { label: "Chat", icon: MessageCircle },
  desktop: { label: "Desktop", icon: Monitor },
  files: { label: "Files", icon: Folder },
  connectors: { label: "Connectors", icon: Plug },
  channels: { label: "Channels", icon: Radio },
  skills: { label: "Skills", icon: Book },
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
    <div className="flex shrink-0 items-center gap-2 px-3 text-iron max-wide:h-12 wide:flex-col wide:gap-1.5 wide:px-2 wide:pt-5">
      <SiloGlyph className="max-wide:h-5 max-wide:w-5 wide:h-7 wide:w-7" />
      <span className="font-medium tracking-[0.02em] max-wide:text-[13px] wide:text-[12px]">Silo</span>
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

function railHit(active: boolean, extra = "") {
  return `relative flex h-10 w-10 shrink-0 items-center justify-center rounded-[6px] transition-colors duration-150 ease-quiet max-wide:mx-0 wide:mx-2 ${
    active ? "bg-bindery-pale text-iron" : "text-stone hover:bg-linen/70 hover:text-iron"
  } ${extra}`;
}

function railBar(on: boolean, needsYou = false) {
  if (!on && !needsYou) return null;
  return (
    <span
      className={`absolute max-wide:inset-x-1.5 max-wide:bottom-0.5 max-wide:h-0.5 wide:top-1 wide:bottom-1 wide:left-0 wide:w-0.5 ${
        needsYou ? "bg-carmine" : "bg-bindery"
      }`}
    />
  );
}

function Rail({ page }: { page: "bots" | "admin" | "account" | "skills" }) {
  const { admin, setSession } = useAuth();
  const { bots } = useBots();
  const loc = useLocation();
  const botMatch = loc.pathname.match(/^\/bots\/([^/]+)/);
  const activeBotId = botMatch?.[1];
  const homeActive = page === "bots" && !activeBotId && loc.pathname !== "/new";
  return (
    <aside className="flex shrink-0 border-thread-2 bg-cloth max-wide:h-[calc(3rem+env(safe-area-inset-top))] max-wide:w-full max-wide:flex-row max-wide:items-center max-wide:border-b max-wide:pt-[env(safe-area-inset-top)] wide:w-16 wide:flex-col wide:items-center wide:border-r">
      <SiloMark />
      <nav className="flex min-h-0 min-w-0 flex-1 items-center max-wide:flex-row wide:mt-6 wide:flex-col">
        <Link to="/" title="Bots" className={railHit(homeActive)}>
          {railBar(homeActive)}
          <LayoutGrid size={20} />
        </Link>
        <Link to="/skills" title="Skills" className={railHit(page === "skills") + " wide:mt-1"}>
          {railBar(page === "skills")}
          <Book size={20} />
        </Link>
        <div className="silo-scroll-x flex min-h-0 min-w-0 flex-1 max-wide:flex-row max-wide:items-center wide:mt-2 wide:flex-col wide:overflow-x-hidden wide:overflow-y-auto">
          {(bots ?? []).map((b) => {
            const on = b.id === activeBotId;
            return (
              <Link
                key={b.id}
                to={`/bots/${b.id}/run`}
                title={b.name}
                className={railHit(on, "wide:mb-1")}
              >
                {railBar(on, b.status === "needs_you")}
                <span className="relative">
                  <Crest index={b.crest} size={28} />
                  <span
                    className={`absolute -right-0.5 -bottom-0.5 h-[8px] w-[8px] rounded-full ring-2 ring-cloth ${lampClass(b.status)}`}
                  />
                </span>
              </Link>
            );
          })}
        </div>
        <Link to="/new" title="New Bot" className={railHit(loc.pathname === "/new") + " wide:mt-1"}>
          {railBar(loc.pathname === "/new")}
          <Plus size={20} />
        </Link>
      </nav>
      <div className="flex items-center max-wide:pr-1 wide:mb-4 wide:flex-col wide:gap-1">
        {admin && (
          <Link to="/admin" title="Admin" className={railHit(page === "admin")}>
            {railBar(page === "admin")}
            <Wrench size={20} />
          </Link>
        )}
        <Link to="/account" title="Account" className={railHit(page === "account")}>
          {railBar(page === "account")}
          <User size={20} />
        </Link>
        <button
          title="Sign out"
          className={railHit(false)}
          onClick={async () => {
            await ui.signOut({});
            setSession(null);
          }}
        >
          <LogOut size={20} />
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
    <div className="flex min-h-dvh items-start justify-center bg-plaster px-4 pt-[18vh]">
      <form onSubmit={onSubmit} className="silo-enter w-full max-w-[400px] rounded-[10px] border border-thread bg-folio p-8">
        <div className="mb-7 flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-[8px] bg-cloth text-iron">
            <SiloGlyph className="h-5 w-5" />
          </span>
          <span className="text-[14px] font-medium">Silo Agent</span>
        </div>
        <h1 className="mb-6 text-[22px] font-medium">Sign in</h1>
        <label htmlFor="silo-email" className="mb-1.5 block text-[12px] font-medium text-stone">
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
        <label htmlFor="silo-pass" className="mb-1.5 block text-[12px] font-medium text-stone">
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
          <p role="alert" className="mb-3 flex items-start gap-2 text-[13px] text-carmine">
            <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-carmine" />
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
    <div className="relative flex gap-4 overflow-hidden rounded-[10px] border border-thread bg-folio p-4">
      <div className="h-14 w-14 shrink-0 rounded-[14px] bg-cloth" />
      <div className="min-w-0 flex-1 py-1.5">
        <div className="mb-2.5 h-3.5 w-28 rounded-[4px] bg-linen" />
        <div className="h-3 w-44 rounded-[4px] bg-cloth" />
      </div>
      <span aria-hidden className="silo-shimmer pointer-events-none absolute inset-0" />
    </div>
  );
}

function BotsPage() {
  const { bots, err } = useBots();
  const loading = bots === null;
  return (
    <div className="p-4 wide:p-7">
      <div className="mb-1 flex items-center justify-between gap-3">
        <h1 className="text-[22px] font-medium">Bots</h1>
        <Link to="/new" className={btnClass("primary")}>
          <Plus size={16} />
          New Bot
        </Link>
      </div>
      <p className="mb-6 text-[13px] text-stone">Machines you can open.</p>
      {err && (
        <p role="alert" className="mb-4 text-[13px] text-carmine">
          {err}
        </p>
      )}
      {loading ? (
        <div className="grid grid-cols-1 gap-5 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
          <FolioSkeleton />
          <FolioSkeleton />
          <FolioSkeleton />
        </div>
      ) : bots.length === 0 ? (
        <div className="silo-enter py-20 text-center">
          <div className="mb-7 flex justify-center opacity-20">
            <Crest index={packCrest(0, 10)} size={88} />
          </div>
          <p className="mb-2 text-[28px] font-medium wide:text-[40px]">No Bots yet</p>
          <p className="mb-7 text-[13px] text-stone">A Bot is its own machine. It does not share files with the others.</p>
          <Link to="/new" className={btnClass("primary")}>
            <Plus size={16} />
            New Bot
          </Link>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-5 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
          {bots.map((b, i) => (
            <Link
              key={b.id}
              to={`/bots/${b.id}/run`}
              style={{ animationDelay: `${Math.min(i * 45, 270)}ms` }}
              className="silo-enter group relative flex gap-4 overflow-hidden rounded-[10px] border border-thread bg-folio p-4 transition-[border-color,background-color,transform] duration-200 ease-quiet hover:border-hover hover:bg-folio active:scale-[0.995]"
            >
              {b.status === "needs_you" ? (
                <span aria-hidden className="absolute inset-y-0 left-0 w-[2px] bg-carmine" />
              ) : null}
              <Crest index={b.crest} size={56} />
              <div className="min-w-0 flex-1">
                <div className="text-[16px] font-medium">{b.name}</div>
                {b.description ? <div className="mt-0.5 truncate text-[13px] text-stone">{b.description}</div> : null}
                <div className="mt-1.5 flex items-center gap-2 text-[12px] font-medium">
                  <span className={`inline-block h-[7px] w-[7px] shrink-0 rounded-full ${lampClass(b.status)}`} />
                  <span className={statusWord(b.status)}>{statusLabel(b.status)}</span>
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
    <div className="silo-page silo-page-sm silo-enter">
      <h1 className="mb-6 text-[22px] font-medium">New Bot</h1>
      <form onSubmit={create}>
        <div className="mb-5 flex flex-col items-center gap-4">
          <Crest index={crest} size={88} />
          <span className="text-[12px] text-stone">Pick a crest for this machine</span>
        </div>
        <div className="mb-6">
          <CrestPicker value={crest} onChange={setCrest} />
        </div>
        <label htmlFor="bot-name" className="mb-1.5 block text-[12px] font-medium text-stone">
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
        <label htmlFor="bot-desc" className="mb-1.5 block text-[12px] font-medium text-stone">
          Description
        </label>
        <textarea
          id="bot-desc"
          className={`${textareaClass} mb-3 min-h-[72px]`}
          placeholder="What this machine is for"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <p className="mb-6 text-[13px] text-stone">A Bot is its own machine. It does not share files with the others.</p>
        {err && (
          <p role="alert" className="mb-3 text-[13px] text-carmine">
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
        next.background = "#0B0F19";
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
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center font-mono text-[13px] text-stone">
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
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[10px] bg-hatch">
      <div className="flex h-8 shrink-0 items-center gap-2 px-3 text-[13px] text-plaster/80">
        <Crest index={bot.crest} size={20} />
        <span className="min-w-0 truncate">{bot.name}</span>
        <span className="text-plaster/50">{label}</span>
        <span className={`inline-block h-1.5 w-1.5 rounded-full ${lampClass(bot.status)}`} />
        <span className="ml-auto font-mono text-[12px] text-plaster/80">{statusLabel(bot.status)}</span>
      </div>
      <div className="min-h-0 flex-1 bg-matte p-2">
        {kind === "console" ? <ConsoleTerm botId={bot.id} live visible={visible} /> : <Hatch botId={bot.id} live visible={visible} />}
      </div>
    </div>
  );
}

const machineBtnCompact =
  "@max-[1280px]:h-10 @max-[1280px]:w-10 @max-[1280px]:justify-center @max-[1280px]:gap-0 @max-[1280px]:px-0 @max-[1280px]:pr-0";

function FadeScroll({
  className = "",
  innerClass = "",
  fade = "from-plaster",
  children,
}: {
  className?: string;
  innerClass?: string;
  fade?: "from-plaster" | "from-cloth";
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
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
  }, []);
  function nudge(dir: -1 | 1) {
    const el = ref.current;
    if (!el) return;
    el.scrollBy({ left: dir * Math.max(160, el.clientWidth * 0.7), behavior: "smooth" });
  }
  return (
    <div className={`relative min-w-0 ${className}`}>
      <div ref={ref} className={`silo-scroll-x h-full ${innerClass}`}>
        {children}
      </div>
      {edge.start ? (
        <div className={`absolute inset-y-0 left-0 z-10 flex w-8 items-stretch bg-gradient-to-r ${fade} to-transparent`}>
          <button
            type="button"
            title="Previous"
            className="flex w-8 items-center justify-center text-stone hover:text-iron"
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
            className="flex w-8 items-center justify-center text-stone hover:text-iron"
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
  return `flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 text-[14px] transition-colors duration-150 ease-quiet ${
    compact && !on ? "min-w-10 justify-center px-2" : "px-3"
  } ${on ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`;
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
    <NavLink to={to} title={label} className={tabClass(on, compact)}>
      <Icon size={16} />
      {(!compact || on) && label}
    </NavLink>
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
    if (r) setBox({ top: r.bottom + 4, left: r.left });
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
      className={`relative flex h-full shrink-0 items-stretch border-b-2 ${
        onMachine ? "border-bindery" : "border-transparent"
      }`}
    >
      <NavLink
        to={href}
        className={`flex shrink-0 items-center gap-1.5 whitespace-nowrap px-3 text-[14px] ${
          onMachine ? "text-iron" : "text-stone hover:text-iron"
        }`}
      >
        <Icon size={16} />
        {label}
      </NavLink>
      <button
        type="button"
        title={otherLabel}
        aria-expanded={open}
        className={`flex items-center px-1 ${onMachine ? "text-iron" : "text-stone hover:text-iron"}`}
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronDown size={12} />
      </button>
      {open &&
        createPortal(
          <div
            ref={menu}
            className="z-50 w-40 rounded-[6px] border border-thread bg-folio py-1"
            style={{ position: "fixed", top: box.top, left: box.left }}
          >
            <Link
              to={other}
              onClick={() => setOpen(false)}
              className="flex items-center gap-2 px-3 py-1.5 text-[14px] text-iron hover:bg-linen"
            >
              <OtherIcon size={16} />
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
    <div className="h-1.5 overflow-hidden rounded-full bg-linen">
      <div className="h-full bg-bindery" style={{ width: `${pct}%` }} />
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
      <h2 className="text-[22px] font-medium">Container</h2>
      <p className="mb-6 text-stone">This Bot’s machine. Usage from Docker.</p>
      {err && <p className="mb-4 text-carmine">{err}</p>}
      <div className="mb-6 flex items-center gap-3">
        <span className={`inline-block h-[7px] w-[7px] rounded-full ${lampClass(bot.status)}`} />
        <span className={`text-[12px] font-medium ${statusWord(bot.status)}`}>{statusLabel(bot.status)}</span>
      </div>
      <div className="mb-6 grid max-w-[560px] gap-4">
        <div className="rounded-[10px] border border-thread bg-folio p-4">
          <div className="mb-1 text-[11px] font-medium tracking-wide text-stone">CPU</div>
          <div className="mb-2 font-mono text-[20px] font-medium tracking-tight">
            {box?.running ? `${cpu.toFixed(1)}%` : "—"}
          </div>
          <Meter value={box?.running ? cpu : 0} max={100} />
        </div>
        <div className="rounded-[10px] border border-thread bg-folio p-4">
          <div className="mb-1 text-[11px] font-medium tracking-wide text-stone">RAM</div>
          <div className="mb-2 font-mono text-[20px] font-medium tracking-tight">
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
  const [secrets, setSecrets] = useState<SecretMeta[]>([]);
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
            if (!dead) push({ id: ev.id, kind: ev.kind, body: ev.body, tool: ev.tool, runId: ev.runId, attachments: ev.attachments.map((a) => ({ name: a.name, path: a.path, size: Number(a.size) })) });
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
    ui.listSecrets({ botId: id }).then((r) => setSecrets(r.secrets)).catch(console.error);
  }, [id, tab]);

  if (!id) return <Navigate to="/" />;
  if (!tabParam) return <Navigate to={`/bots/${id}/run`} replace />;
  if (!chatId && tabParam && tabParam !== "console" && !isNavTab(tabParam)) return <Navigate to={`/bots/${id}/run`} replace />;
  if (loadErr) {
    return (
      <div className="p-7">
        <p className="mb-3 text-carmine">{loadErr}</p>
        <Link to="/" className="text-bindery">
          Back to Bots
        </Link>
      </div>
    );
  }
  if (!bot) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-11 items-center gap-3 border-b border-thread-2 px-4 wide:h-14">
          <div className="hidden h-7 w-7 rounded-[8px] bg-cloth wide:block" />
          <div className="hidden h-5 w-32 rounded-[6px] bg-linen wide:block" />
        </div>
        <div className="p-4 text-stone">Opening…</div>
      </div>
    );
  }

  async function send(e?: FormEvent) {
    e?.preventDefault();
    if (!id || (!text.trim() && atts.length === 0)) return;
    const msg = text.trim();
    setText("");
    setSending(true);
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
      <header className="@container flex h-11 shrink-0 items-center gap-1 border-b border-thread-2 px-2 wide:h-14 wide:gap-3 wide:px-4">
        <span className="hidden wide:inline-flex">
          <Crest index={bot.crest} size={28} />
        </span>
        <h1 className="sr-only min-w-0 truncate text-[16px] font-medium wide:not-sr-only wide:max-w-[12rem]">{bot.name}</h1>
        <span className={`hidden h-[7px] w-[7px] shrink-0 rounded-full wide:inline-block ${lampClass(bot.status)}`} />
        <span className={`hidden shrink-0 text-[12px] font-medium wide:inline ${statusWord(bot.status)}`}>{statusLabel(bot.status)}</span>
        <FadeScroll className="hidden min-h-0 flex-1 self-stretch wide:block" innerClass="flex h-full items-stretch gap-1">
          <BotTabs id={id} tab={tab} chatId={chatId} />
        </FadeScroll>
        <FadeScroll className="min-h-0 flex-1 self-stretch wide:hidden" innerClass="flex h-full items-stretch gap-0.5">
          <BotTabs id={id} tab={tab} chatId={chatId} splitMachine compact />
        </FadeScroll>
        <div className="shrink-0">
            {bot.workerConnected ? (
              <Btn
                kind="ghost"
                title="Stop Bot"
                aria-label="Stop Bot"
                onClick={stop}
                className={machineBtnCompact}
                icon={<Power size={12} />}
              >
                <span className="@max-[1280px]:hidden">Stop Bot</span>
              </Btn>
            ) : (
              <Btn
                kind="primary"
                title={bot.status === "starting" ? "Starting…" : "Start Bot"}
                aria-label={bot.status === "starting" ? "Starting" : "Start Bot"}
                onClick={start}
                disabled={bot.status === "starting"}
                className={machineBtnCompact}
                icon={<Power size={12} />}
              >
                <span className="@max-[1280px]:hidden">{bot.status === "starting" ? "Starting…" : "Start Bot"}</span>
              </Btn>
            )}
        </div>
      </header>
      {actErr && <div className="border-b border-thread-2 bg-folio px-4 py-2 text-carmine">{actErr}</div>}
      <div className="relative flex min-h-0 flex-1">
        {tab === "run" && (
          <>
            <aside className="hidden w-[240px] shrink-0 flex-col border-r border-thread-2 bg-cloth wide:flex">
              <div className="flex items-center justify-between px-3 py-3">
                <span className="text-[11px] font-medium tracking-wide text-stone">Chats</span>
                <button
                  className="inline-flex items-center gap-1 rounded-[4px] px-1 py-0.5 text-[12px] font-medium text-bindery transition-colors duration-150 ease-quiet hover:text-bindery-deep"
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                >
                  <Plus size={13} />
                  New
                </button>
              </div>
              <div className="min-h-0 flex-1 overflow-auto px-2 pb-3">
                {chats.length === 0 && <p className="px-2 py-2 text-stone">No chats</p>}
                {chats.map((c) => (
                  <div key={c.id} className="group mb-0.5 flex items-center">
                    {editingChat === c.id ? (
                      <input
                        autoFocus
                        className="min-w-0 flex-1 rounded-[6px] bg-folio px-2 py-1.5 text-[14px] outline-none ring-1 ring-bindery"
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
                        className={`min-w-0 flex-1 truncate rounded-[6px] px-2 py-1.5 transition-colors duration-150 ease-quiet ${
                          c.id === chatId ? "bg-bindery-pale text-iron" : "text-stone hover:bg-linen hover:text-iron"
                        }`}
                      >
                        {c.title || "New chat"}
                      </NavLink>
                    )}
                    {editingChat !== c.id && (
                      <button
                        title="Rename chat"
                        className="hidden px-1 text-stone transition-colors duration-150 hover:text-iron group-hover:block"
                        onClick={(e) => {
                          e.preventDefault();
                          setEditingChat(c.id);
                          setEditTitle(c.title || "");
                        }}
                      >
                        <Pencil size={14} />
                      </button>
                    )}
                    <button
                      title="Delete chat"
                      className="hidden px-1 text-stone transition-colors duration-150 hover:text-carmine group-hover:block"
                      onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
              </div>
            </aside>
            <section className="flex min-w-0 flex-1 flex-col">
              <FadeScroll className="shrink-0 border-b border-thread-2 bg-cloth wide:hidden" innerClass="flex items-center gap-1 px-2 py-1.5" fade="from-cloth">
                <button
                  className="inline-flex shrink-0 items-center gap-1 px-2 py-1.5 text-[12px] text-bindery hover:text-bindery-deep"
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                >
                  <Plus size={14} />
                  New
                </button>
                {chats.map((c) => (
                  <div
                    key={c.id}
                    className={`flex shrink-0 items-center rounded-[6px] ${
                      c.id === chatId ? "bg-bindery-pale text-iron" : "text-stone"
                    }`}
                  >
                    {editingChat === c.id ? (
                      <input
                        autoFocus
                        className="w-36 rounded-[6px] bg-folio px-2 py-1.5 text-[14px] outline-none ring-1 ring-bindery"
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
                        className={`max-w-[10rem] truncate px-2.5 py-1.5 ${
                          c.id === chatId ? "" : "rounded-[6px] hover:bg-linen hover:text-iron"
                        }`}
                      >
                        {c.title || "New chat"}
                      </NavLink>
                    )}
                    {c.id === chatId && editingChat !== c.id && (
                      <button
                        title="Rename chat"
                        className="px-1 py-1.5 text-stone hover:text-iron"
                        onClick={(e) => {
                          e.preventDefault();
                          setEditingChat(c.id);
                          setEditTitle(c.title || "");
                        }}
                      >
                        <Pencil size={14} />
                      </button>
                    )}
                    {c.id === chatId && (
                      <button
                        title="Delete chat"
                        className="py-1.5 pr-2 pl-0.5 text-stone hover:text-carmine"
                        onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                      >
                        <Trash2 size={14} />
                      </button>
                    )}
                  </div>
                ))}
              </FadeScroll>
              <Thread
                botId={id!}
                botName={bot.name}
                botCrest={bot.crest}
                chatId={chatId}
                events={events}
                sending={sending}
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
            <p className="mt-2 text-stone">Same browser the Bot uses. You can type and click.</p>
          </section>
        ) : null}
        {tab === "console" || keepCon ? (
          <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "console" ? "flex" : "hidden"}`}>
            <MachinePane bot={bot} onStart={start} visible={tab === "console"} kind="console" />
            <p className="mt-2 text-stone">A shell on this Bot, started in /workspace.</p>
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
            <h2 className="text-[22px] font-medium">Secrets</h2>
            <p className="mb-4 text-stone">Handed to the Bot only after you allow it. Masked before the model sees output.</p>
            {secrets.length === 0 && <p className="mb-4 text-stone">No secrets on this Bot yet.</p>}
            {secrets.map((s) => (
              <div key={s.id} className="mb-2 flex items-center justify-between rounded-[6px] border border-thread bg-folio px-3 py-2">
                <div>
                  <div className="font-medium">{s.name}</div>
                  <div className="font-mono text-stone">•••••••• · {s.lastUsedAt || "never used"}</div>
                </div>
                <Btn
                  kind="ghost"
                  className="text-carmine hover:text-carmine"
                  onClick={async () => {
                    await ui.deleteSecret({ botId: id, id: s.id });
                    setSecrets((xs) => xs.filter((x) => x.id !== s.id));
                  }}
                >
                  Delete
                </Btn>
              </div>
            ))}
            <form
              className="mt-6 flex flex-col gap-2 wide:flex-row"
              onSubmit={async (e) => {
                e.preventDefault();
                await ui.addSecret({ botId: id, name: secName, value: secVal });
                setSecName("");
                setSecVal("");
                setSecrets((await ui.listSecrets({ botId: id })).secrets);
              }}
            >
              <input className="h-9 flex-1 rounded-[6px] border border-thread bg-folio px-3" placeholder="Name" value={secName} onChange={(e) => setSecName(e.target.value)} />
              <input type="password" className="h-9 flex-1 rounded-[6px] border border-thread bg-folio px-3" placeholder="Value" value={secVal} onChange={(e) => setSecVal(e.target.value)} />
              <Btn kind="primary" type="submit" icon={<Plus size={12} />}>
                Add
              </Btn>
            </form>
          </div>
          </div>
        )}
        {tab === "container" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <ContainerPane bot={bot} onStart={start} onStop={stop} />
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
        {(pending[0] || authPrompt?.connector) && (
          <div className="pointer-events-none absolute inset-0 z-[9] bg-iron/15 wide:bg-transparent" />
        )}
        {pending[0] ? (
          <ApprovalSlip
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
        ) : (
          authPrompt?.connector && (
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
          )
        )}
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
