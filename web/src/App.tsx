import { ArrowCounterClockwise, ArrowUp, CaretDown, ChatCircle, Cube, Folder, Key, ListChecks, Monitor, PencilSimple, Plugs, Plus, Power, SignOut, SlidersHorizontal, SquaresFour, Stop, TerminalWindow, Trash, User, Wrench } from "@phosphor-icons/react";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Link, Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { COLOR_COUNT, Crest, CrestPicker, packCrest, SHAPE_COUNT } from "./Crest";
import { ApprovalSlip, ConnectorAuthSlip } from "./Approval";
import { ConsoleTerm } from "./Console";
import { FilesPane } from "./Files";
import { NeedMachine } from "./NeedMachine";
import { Thread, type Ev } from "./Thread";
import { AdminLayout, AccountPage, AdminSettings } from "./Admin";
import { AdminConnectors } from "./AdminConnectors";
import { BotConnectors, startConnectorAuth } from "./BotConnectors";
import { RulesPane } from "./Rules";
import type { Approval, Bot, BotConnector, Chat, Container, SecretMeta } from "./gen/silo/v1/ui_pb";

const tabs = ["run", "desktop", "files", "connectors", "secrets", "rules", "container", "settings"] as const;
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

const tabMeta: Record<NavTab, { label: string; icon: typeof ChatCircle }> = {
  run: { label: "Chat", icon: ChatCircle },
  desktop: { label: "Desktop", icon: Monitor },
  files: { label: "Files", icon: Folder },
  connectors: { label: "Connectors", icon: Plugs },
  secrets: { label: "Secrets", icon: Key },
  rules: { label: "Rules", icon: ListChecks },
  container: { label: "Container", icon: Cube },
  settings: { label: "Settings", icon: SlidersHorizontal },
};

function randomCrest() {
  return packCrest(Math.floor(Math.random() * SHAPE_COUNT), Math.floor(Math.random() * COLOR_COUNT));
}

function SiloMark() {
  return (
    <div className="flex flex-col items-center gap-1 px-2 pt-4">
      <div className="h-7 w-3 rounded-sm bg-iron" />
      <div className="px-1 text-center text-[11px] font-medium leading-tight tracking-wide">Silo Agent</div>
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
  return `relative mx-2 flex h-10 w-10 shrink-0 items-center justify-center rounded ${
    active ? "bg-bindery-pale text-iron" : "text-stone hover:text-iron"
  } ${extra}`;
}

function Rail({ page }: { page: "bots" | "admin" | "account" }) {
  const { admin, setSession } = useAuth();
  const { bots } = useBots();
  const loc = useLocation();
  const botMatch = loc.pathname.match(/^\/bots\/([^/]+)/);
  const activeBotId = botMatch?.[1];
  const homeActive = page === "bots" && !activeBotId && loc.pathname !== "/new";
  return (
    <aside className="flex w-16 shrink-0 flex-col items-center border-r border-thread-2 bg-cloth">
      <SiloMark />
      <nav className="mt-6 flex min-h-0 flex-1 flex-col items-center">
        <Link to="/" title="Bots" className={railHit(homeActive)}>
          {homeActive && <span className="absolute top-1 bottom-1 left-0 w-0.5 bg-bindery" />}
          <SquaresFour size={20} weight="regular" />
        </Link>
        <div className="mt-2 min-h-0 flex-1 overflow-y-auto">
          {(bots ?? []).map((b) => {
            const on = b.id === activeBotId;
            return (
              <Link
                key={b.id}
                to={`/bots/${b.id}/run`}
                title={b.name}
                className={railHit(on, "mb-1")}
              >
                {b.status === "needs_you" ? (
                  <span className="absolute top-1 bottom-1 left-0 w-0.5 bg-carmine" />
                ) : (
                  on && <span className="absolute top-1 bottom-1 left-0 w-0.5 bg-bindery" />
                )}
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
        <Link to="/new" title="New Bot" className={railHit(loc.pathname === "/new") + " mt-1"}>
          {loc.pathname === "/new" && <span className="absolute top-1 bottom-1 left-0 w-0.5 bg-bindery" />}
          <Plus size={20} weight="regular" />
        </Link>
      </nav>
      <div className="mb-4 flex flex-col items-center gap-1">
        {admin && (
          <Link to="/admin" title="Admin" className={railHit(page === "admin")}>
            {page === "admin" && <span className="absolute top-1 bottom-1 left-0 w-0.5 bg-bindery" />}
            <Wrench size={20} weight="regular" />
          </Link>
        )}
        <Link to="/account" title="Account" className={railHit(page === "account")}>
          {page === "account" && <span className="absolute top-1 bottom-1 left-0 w-0.5 bg-bindery" />}
          <User size={20} weight="regular" />
        </Link>
        <button
          title="Sign out"
          className={railHit(false)}
          onClick={async () => {
            await ui.signOut({});
            setSession(null);
          }}
        >
          <SignOut size={20} weight="regular" />
        </button>
      </div>
    </aside>
  );
}

function Shell({ page, fill, children }: { page: "bots" | "admin" | "account"; fill?: boolean; children: ReactNode }) {
  return (
    <div className="flex h-dvh overflow-hidden">
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
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      const r = await ui.signIn({ email, password });
      setSession({ email: r.user?.email ?? email, admin: r.user?.admin ?? false });
      nav("/");
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  return (
    <div className="flex min-h-dvh items-start justify-center bg-plaster pt-[18vh]">
      <form onSubmit={onSubmit} className="w-[400px] rounded-[10px] border border-thread bg-folio p-8">
        <div className="mb-6 flex items-center gap-3">
          <div className="h-7 w-3 rounded-sm bg-iron" />
          <span className="text-[13px] font-medium">Silo Agent</span>
        </div>
        <h1 className="mb-6 text-[22px] font-medium tracking-tight">Sign in</h1>
        <label className="mb-1 block text-[12px] font-medium text-stone">Email</label>
        <input
          className="mb-4 w-full rounded border border-thread bg-folio px-3 py-2 outline-none focus:border-bindery"
          value={email}
          onChange={(e) => setEm(e.target.value)}
          autoComplete="username"
        />
        <label className="mb-1 block text-[12px] font-medium text-stone">Password</label>
        <input
          type="password"
          className="mb-6 w-full rounded border border-thread bg-folio px-3 py-2 outline-none focus:border-bindery"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />
        {err && <p className="mb-3 text-carmine">{err}</p>}
        <Btn kind="primary" className="w-full justify-center" type="submit">
          Sign in
        </Btn>
      </form>
    </div>
  );
}

function FolioSkeleton() {
  return (
    <div className="flex gap-4 rounded-[10px] border border-thread bg-folio p-4">
      <div className="h-14 w-14 shrink-0 rounded-[14px] bg-cloth" />
      <div className="min-w-0 flex-1 py-1">
        <div className="mb-2 h-4 w-28 rounded bg-linen" />
        <div className="h-3.5 w-44 rounded bg-cloth" />
      </div>
    </div>
  );
}

function BotsPage() {
  const { bots, err } = useBots();
  return (
    <div className="p-7">
      <div className="mb-1 flex items-center justify-between">
        <h1 className="text-[22px] font-medium tracking-tight">Bots</h1>
        <Link to="/new" className={btnClass("primary")}>
          <Plus size={16} />
          New Bot
        </Link>
      </div>
      <p className="mb-6 text-stone">Machines you can open.</p>
      {err && <p className="mb-4 text-carmine">{err}</p>}
      {bots === null ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          <FolioSkeleton />
          <FolioSkeleton />
          <FolioSkeleton />
        </div>
      ) : bots.length === 0 ? (
        <div className="py-20 text-center">
          <p className="mb-2 text-[40px] font-medium tracking-tight">No Bots yet</p>
          <p className="mb-6 text-stone">A Bot is its own machine. It does not share files with the others.</p>
          <Link to="/new" className={btnClass("primary")}>
            <Plus size={16} />
            New Bot
          </Link>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {bots.map((b) => (
            <Link
              key={b.id}
              to={`/bots/${b.id}/run`}
              className={`flex gap-4 rounded-[10px] border border-thread bg-folio p-4 hover:border-[#B9B3A6] ${
                b.status === "needs_you" ? "border-l-2 border-l-carmine" : ""
              }`}
            >
              <Crest index={b.crest} size={56} />
              <div className="min-w-0">
                <div className="text-[16px] font-medium">{b.name}</div>
                {b.description ? <div className="truncate text-stone">{b.description}</div> : null}
                <div className="mt-1 flex items-center gap-2 text-[12px] font-medium">
                  <span className={`inline-block h-[7px] w-[7px] rounded-full ${lampClass(b.status)}`} />
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
    <div className="mx-auto w-[560px] p-7">
      <h1 className="mb-6 text-[22px] font-medium tracking-tight">New Bot</h1>
      <form onSubmit={create}>
        <div className="mb-5 flex flex-col items-center">
          <Crest index={crest} size={88} />
        </div>
        <div className="mb-6">
          <CrestPicker value={crest} onChange={setCrest} />
        </div>
        <label className="mb-1 block text-[12px] font-medium text-stone">Name</label>
        <input
          className="mb-2 h-9 w-full rounded border border-thread bg-folio px-3 outline-none focus:border-bindery"
          placeholder="Scout"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />
        <label className="mb-1 mt-4 block text-[12px] font-medium text-stone">Description</label>
        <textarea
          className="mb-2 min-h-[72px] w-full rounded border border-thread bg-folio px-3 py-2 outline-none focus:border-bindery"
          placeholder="What this machine is for"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <p className="mb-6 text-stone">A Bot is its own machine. It does not share files with the others.</p>
        {err && <p className="mb-3 text-carmine">{err}</p>}
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
    if (ev.runId && ev.kind === "done") open.delete(ev.runId);
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
        next.background = "#12141A";
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
        <span>{bot.name}</span>
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

function MachineNav({ id, tab }: { id: string; tab: Tab }) {
  const onMachine = tab === "desktop" || tab === "console";
  const label = tab === "console" ? "Console" : "Desktop";
  const Icon = tab === "console" ? TerminalWindow : Monitor;
  const href = tab === "console" ? `/bots/${id}/console` : `/bots/${id}/desktop`;
  return (
    <div className="relative flex items-stretch">
      <NavLink
        to={href}
        className={`flex items-center gap-1.5 border-b-2 px-3 text-[14px] ${
          onMachine ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"
        }`}
      >
        <Icon size={16} />
        {label}
      </NavLink>
      <details key={tab} className="relative">
        <summary
          title="Desktop or Console"
          className={`flex h-full cursor-pointer items-center border-b-2 px-1 ${
            onMachine ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"
          }`}
        >
          <CaretDown size={12} />
        </summary>
        <div className="absolute left-0 top-full z-20 mt-1 w-40 rounded border border-thread bg-folio py-1">
          <Link
            to={`/bots/${id}/desktop`}
            className={`flex items-center gap-2 px-3 py-1.5 text-[14px] hover:bg-linen ${
              tab === "desktop" ? "bg-bindery-pale text-iron" : "text-iron"
            }`}
          >
            <Monitor size={16} />
            Desktop
          </Link>
          <Link
            to={`/bots/${id}/console`}
            className={`flex items-center gap-2 px-3 py-1.5 text-[14px] hover:bg-linen ${
              tab === "console" ? "bg-bindery-pale text-iron" : "text-iron"
            }`}
          >
            <TerminalWindow size={16} />
            Console
          </Link>
        </div>
      </details>
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
    <div className="mx-auto w-[760px] p-7">
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

function PromptWell({
  label,
  hint,
  value,
  onChange,
}: {
  label: string;
  hint: string;
  value: string;
  onChange: (s: string) => void;
}) {
  const over = value.length > 8000;
  return (
    <div className="min-w-0">
      <div className="mb-1 flex items-baseline justify-between gap-3">
        <label className="text-[12px] font-medium text-stone">{label}</label>
        <span className={`font-mono text-[11px] ${over ? "text-carmine" : "text-stone"}`}>{value.length}/8000</span>
      </div>
      <p className="mb-1 text-[12px] text-stone">{hint}</p>
      <textarea
        className="h-[240px] w-full resize-y rounded border border-thread bg-folio px-3 py-2 font-mono text-[13px] leading-5 outline-none focus:border-bindery"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}

function SettingsPane({
  bot,
  onSaved,
  onError,
}: {
  bot: Bot;
  onSaved: (b: Bot) => void;
  onError: (s: string) => void;
}) {
  const nav = useNavigate();
  const { refresh } = useBots();
  const [name, setName] = useState(bot.name);
  const [description, setDescription] = useState(bot.description);
  const [soul, setSoul] = useState(bot.soul);
  const [memory, setMemory] = useState(bot.memory);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [arm, setArm] = useState<"reset" | "delete" | "">("");
  const [dangerBusy, setDangerBusy] = useState(false);
  useEffect(() => {
    setName(bot.name);
    setDescription(bot.description);
    setSoul(bot.soul);
    setMemory(bot.memory);
    setArm("");
    setDangerBusy(false);
  }, [bot.id]);
  async function save(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setSaved(false);
    onError("");
    try {
      const next = await ui.updateBot({
        id: bot.id,
        name: name.trim(),
        description: description.trim(),
        soul,
        memory,
      });
      onSaved(next);
      setSoul(next.soul);
      setMemory(next.memory);
      setSaved(true);
    } catch (ex) {
      onError(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  async function go(which: "reset" | "delete") {
    if (arm !== which) {
      setArm(which);
      return;
    }
    setDangerBusy(true);
    onError("");
    try {
      if (which === "reset") {
        onSaved(await ui.resetContainer({ id: bot.id }));
        setArm("");
        refresh();
      } else {
        await ui.deleteBot({ id: bot.id });
        refresh();
        nav("/");
        return;
      }
    } catch (ex) {
      onError(fail(ex));
    } finally {
      setDangerBusy(false);
    }
  }
  return (
    <div className="mx-auto w-[760px] p-7 pb-12">
      <h2 className="text-[22px] font-medium tracking-tight">Settings</h2>
      <p className="mb-6 text-stone">This Bot only. SOUL and MEMORY are in the prompt; the Bot can edit them too.</p>
      <form onSubmit={save}>
        <label className="mb-1 block text-[12px] font-medium text-stone">Name</label>
        <input
          className="mb-4 h-9 w-full rounded border border-thread bg-folio px-3 outline-none focus:border-bindery"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <label className="mb-1 block text-[12px] font-medium text-stone">Description</label>
        <textarea
          className="mb-6 h-[72px] w-full resize-y rounded border border-thread bg-folio px-3 py-2 outline-none focus:border-bindery"
          placeholder="What this machine is for"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <div className="mb-5 grid grid-cols-2 gap-4">
          <PromptWell label="SOUL" hint="Identity, tone, hard rules." value={soul} onChange={setSoul} />
          <PromptWell label="MEMORY" hint="Lasting facts. Compact past 8000." value={memory} onChange={setMemory} />
        </div>
        <div className="flex items-center gap-3">
          <Btn kind="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? "Saving…" : "Save"}
          </Btn>
          {saved && <span className="text-stone">Saved</span>}
        </div>
      </form>
      <div className="mt-10">
        <h3 className="mb-1 text-[12px] font-medium text-carmine">Dangerous</h3>
        <p className="mb-3 text-[12px] text-stone">Second click confirms. These cannot be undone from here.</p>
        <div className="overflow-hidden rounded-[10px] border border-thread bg-folio">
          <div className="flex items-center gap-4 border-b border-thread-2 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="font-medium">Reset container</div>
              <p className="text-[12px] text-stone">Stops and deletes the box. Workspace and Chrome profile stay. Start Bot makes a new one.</p>
            </div>
            <Btn kind="secondary" type="button" className="shrink-0" disabled={dangerBusy} icon={<ArrowCounterClockwise size={12} />} onClick={() => void go("reset")}>
              {arm === "reset" ? "Reset?" : "Reset"}
            </Btn>
          </div>
          <div className="flex items-center gap-4 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="font-medium">Delete Bot</div>
              <p className="text-[12px] text-stone">Chats, secrets, connectors, the container, and files on disk.</p>
            </div>
            <Btn kind="deny" type="button" className="shrink-0" disabled={dangerBusy} icon={<Trash size={12} />} onClick={() => void go("delete")}>
              {arm === "delete" ? "Delete?" : "Delete"}
            </Btn>
          </div>
        </div>
      </div>
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
  const [keepDesk, setKeepDesk] = useState(tab === "desktop");
  const [keepCon, setKeepCon] = useState(tab === "console");

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
    return () => {
      dead = true;
    };
  }, [id, tab, chatId, nav]);

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
            if (ev.kind === "done") {
              ui.listChats({ botId: id }).then((r) => {
                if (!dead) setChats(r.chats);
              }).catch(() => {});
            }
            if (!dead) push({ id: ev.id, kind: ev.kind, body: ev.body, tool: ev.tool, runId: ev.runId });
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
  }, [id, chatId]);

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
        <div className="flex h-14 items-center gap-3 border-b border-thread-2 px-4">
          <div className="h-7 w-7 rounded-[8px] bg-cloth" />
          <div className="h-5 w-32 rounded bg-linen" />
        </div>
        <div className="p-4 text-stone">Opening…</div>
      </div>
    );
  }

  async function send(e: FormEvent) {
    e.preventDefault();
    if (sending || !id || !text.trim()) return;
    const msg = text.trim();
    setText("");
    setSending(true);
    setActErr("");
    try {
      await ui.send({ botId: id, chatId: chatId || "", text: msg });
      ui.getBot({ id }).then(setBot);
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
    } catch (ex) {
      setSending(false);
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
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-thread-2 px-4">
        <Crest index={bot.crest} size={28} />
        <div className="text-[22px] font-medium tracking-tight">{bot.name}</div>
        <span className={`inline-block h-[7px] w-[7px] rounded-full ${lampClass(bot.status)}`} />
        <span className={`text-[12px] font-medium ${statusWord(bot.status)}`}>{statusLabel(bot.status)}</span>
        <nav className="ml-4 flex h-full items-stretch gap-1">
          {tabs.map((t) => {
            if (t === "desktop") return <MachineNav key="desktop" id={id} tab={tab} />;
            const { label, icon: Icon } = tabMeta[t];
            return (
              <NavLink
                key={t}
                to={t === "run" && chatId ? `/bots/${id}/run/${chatId}` : `/bots/${id}/${t}`}
                className={() =>
                  `flex items-center gap-1.5 border-b-2 px-3 text-[14px] ${
                    tab === t ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"
                  }`
                }
              >
                <Icon size={16} />
                {label}
              </NavLink>
            );
          })}
        </nav>
        <div className="ml-auto flex items-center gap-2">
          {bot.workerConnected ? (
            <Btn kind="ghost" title="Stop this Bot's machine" onClick={stop} icon={<Power size={12} />}>
              Stop Bot
            </Btn>
          ) : (
            <Btn kind="primary" title="Start this Bot's machine" onClick={start} disabled={bot.status === "starting"} icon={<Power size={12} />}>
              {bot.status === "starting" ? "Starting…" : "Start Bot"}
            </Btn>
          )}
        </div>
      </header>
      {actErr && <div className="border-b border-thread-2 bg-folio px-4 py-2 text-carmine">{actErr}</div>}
      <div className="relative flex min-h-0 flex-1">
        {tab === "run" && (
          <>
            <aside className="flex w-[240px] shrink-0 flex-col border-r border-thread-2 bg-cloth">
              <div className="flex items-center justify-between px-3 py-3">
                <span className="text-[11px] font-medium tracking-wide text-stone">Chats</span>
                <button
                  className="inline-flex items-center gap-1 text-[12px] text-bindery hover:text-bindery-deep"
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                >
                  <Plus size={14} />
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
                        className="min-w-0 flex-1 rounded bg-folio px-2 py-1.5 text-[14px] outline-none ring-1 ring-bindery"
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
                        className={`min-w-0 flex-1 truncate rounded px-2 py-1.5 ${
                          c.id === chatId ? "bg-bindery-pale text-iron" : "text-stone hover:bg-linen hover:text-iron"
                        }`}
                      >
                        {c.title || "New chat"}
                      </NavLink>
                    )}
                    {editingChat !== c.id && (
                      <button
                        title="Rename chat"
                        className="hidden px-1 text-stone hover:text-iron group-hover:block"
                        onClick={(e) => {
                          e.preventDefault();
                          setEditingChat(c.id);
                          setEditTitle(c.title || "");
                        }}
                      >
                        <PencilSimple size={14} />
                      </button>
                    )}
                    <button
                      title="Delete chat"
                      className="hidden px-1 text-stone hover:text-carmine group-hover:block"
                      onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                    >
                      <Trash size={14} />
                    </button>
                  </div>
                ))}
              </div>
            </aside>
            <section className="flex min-w-0 flex-1 flex-col">
              <Thread botId={id!} events={events} sending={sending} />
              <form onSubmit={send} className="flex gap-2 border-t border-thread-2 p-3">
                <input
                  className="min-w-0 flex-1 rounded bg-cloth px-3 py-2 outline-none focus:border-bindery focus:ring-1 focus:ring-bindery"
                  placeholder="Ask this Bot…"
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  disabled={!chatId}
                />
                {sending ? (
                  <Btn kind="secondary" type="button" title="Stop this reply" onClick={() => void stopRun()} icon={<Stop size={12} weight="fill" />}>
                    Stop
                  </Btn>
                ) : (
                  <Btn kind="primary" type="submit" disabled={!text.trim() || !chatId} icon={<ArrowUp size={12} />}>
                    Send
                  </Btn>
                )}
              </form>
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
        {tab === "secrets" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
          <div className="mx-auto w-[760px] p-7">
            <h2 className="text-[22px] font-medium">Secrets</h2>
            <p className="mb-4 text-stone">Handed to the Bot only after you allow it. Masked before the model sees output.</p>
            {secrets.length === 0 && <p className="mb-4 text-stone">No secrets on this Bot yet.</p>}
            {secrets.map((s) => (
              <div key={s.id} className="mb-2 flex items-center justify-between rounded border border-thread bg-folio px-3 py-2">
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
              className="mt-6 flex gap-2"
              onSubmit={async (e) => {
                e.preventDefault();
                await ui.addSecret({ botId: id, name: secName, value: secVal });
                setSecName("");
                setSecVal("");
                setSecrets((await ui.listSecrets({ botId: id })).secrets);
              }}
            >
              <input className="h-9 flex-1 rounded border border-thread bg-folio px-3" placeholder="Name" value={secName} onChange={(e) => setSecName(e.target.value)} />
              <input type="password" className="h-9 flex-1 rounded border border-thread bg-folio px-3" placeholder="Value" value={secVal} onChange={(e) => setSecVal(e.target.value)} />
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
            />
          </div>
        )}
        {tab === "rules" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <RulesPane botId={id} />
          </div>
        )}
        {pending[0] ? (
          <ApprovalSlip
            bot={bot}
            approval={pending[0]}
            onDecide={async (decision) => {
              await ui.decideApproval({ id: pending[0].id, decision });
              setPending((xs) => xs.slice(1));
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
