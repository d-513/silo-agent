import { Link, useParams } from "@tanstack/react-router";
import { Inbox, LayoutGrid, Plus, Timer, UserPlus } from "lucide-react";
import { useBots } from "../bots";
import { btnClass } from "../Btn";
import { Crest } from "../Crest";
import { ago, nextWhen } from "../format";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp, StatusWord } from "../Lamp";
import { chatLink } from "../links";
import { sourceLabel } from "../sources";
import { toolMeta } from "../thread/toolInfo";
import { Initials } from "./People";
import { canAdmin, canBuild, useSpaces } from "./spaces";
import { roleWord, SpaceMark } from "./SpaceTile";

// The Workspace's overview (/w/$spaceId): its Bots you are on and what each is
// up to, live. The ones that need you or are at work come first; the rest
// rest below. A card opens the Bot; what is on it opens where it points.

type Mood = "needs_you" | "working" | "online" | "stopped";

function mood(b: Bot): Mood {
  if (b.status === "needs_you" || b.dashboardAsks > 0) return "needs_you";
  if (b.status === "working" || b.activity.length > 0) return "working";
  if (b.status === "stopped" || b.status === "" || b.status === "error") return "stopped";
  return "online";
}

const order: Record<Mood, number> = { needs_you: 0, working: 1, online: 2, stopped: 3 };

export function SpaceOverview() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/" });
  const { spaces } = useSpaces();
  const { bots: all, err } = useBots();
  const space = spaces?.find((s) => s.id === spaceId);
  const bots = all?.filter((b) => b.spaceId === spaceId) ?? null;
  const role = space?.role ?? "";
  const sorted = bots ? [...bots].sort((a, b) => order[mood(a)] - order[mood(b)]) : [];
  const busy = sorted.filter((b) => order[mood(b)] < 2);
  const quiet = sorted.filter((b) => order[mood(b)] >= 2);
  const count = (m: Mood) => sorted.filter((b) => mood(b) === m).length;

  return (
    <div className="mx-auto w-full max-w-[1280px] px-4 pb-16 pt-6 wide:px-8 wide:pt-9">
      <header className="rise mb-8 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex min-w-0 items-center gap-4">
          <SpaceMark space={space} size={52} />
          <div className="min-w-0">
            <h1 className="truncate text-[28px] font-semibold leading-9 tracking-[-0.02em] text-ink">{space?.name ?? t("Workspace")}</h1>
            <p className="mt-0.5 truncate text-[13px] text-ink-3">
              {space
                ? [t("{{count}} Bots", { count: space.bots }), t("{{count}} people", { count: space.people }), roleWord(role)].filter(Boolean).join(" · ")
                : " "}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {canAdmin(role) ? (
            <Link to="/w/$spaceId/people/invite" params={{ spaceId }} className={btnClass("secondary")}>
              <UserPlus size={15} />
              {t("Invite people")}
            </Link>
          ) : null}
          {canBuild(role) ? (
            <Link to="/w/$spaceId/new" params={{ spaceId }} className={btnClass("primary")}>
              <Plus size={16} />
              {t("New Bot")}
            </Link>
          ) : null}
        </div>
      </header>

      {err ? (
        <p role="alert" className="mb-4 text-[13px] text-vermilion">
          {err}
        </p>
      ) : null}

      {bots === null ? (
        <Grid>
          {[0, 1, 2].map((i) => (
            <CardSkeleton key={i} />
          ))}
        </Grid>
      ) : bots.length === 0 ? (
        <Empty spaceId={spaceId} build={canBuild(role)} />
      ) : (
        <>
          <Pulse needs={count("needs_you")} working={count("working")} resting={count("online")} off={count("stopped")} />
          {busy.length ? (
            <Section title={t("Now")}>
              {busy.map((b, i) => (
                <BotCard key={b.id} bot={b} i={i} />
              ))}
            </Section>
          ) : null}
          {quiet.length ? (
            <Section title={busy.length ? t("Resting") : t("Bots")}>
              {quiet.map((b, i) => (
                <BotCard key={b.id} bot={b} i={busy.length + i} />
              ))}
            </Section>
          ) : null}
        </>
      )}
    </div>
  );
}

// Pulse is the Workspace at a glance: how many need you, work, rest, are off.
function Pulse({ needs, working, resting, off }: { needs: number; working: number; resting: number; off: number }) {
  const parts: { status: string; n: number; word: string }[] = [
    { status: "needs_you", n: needs, word: t("{{count}} need you", { count: needs }) },
    { status: "working", n: working, word: t("{{count}} at work", { count: working }) },
    { status: "online", n: resting, word: t("{{count}} resting", { count: resting }) },
    { status: "stopped", n: off, word: t("{{count}} switched off", { count: off }) },
  ];
  return (
    <div className="rise mb-8 flex flex-wrap items-center gap-x-5 gap-y-2 rounded-card bg-well px-4 py-3 shadow-[inset_0_0_0_1px_var(--color-line)]">
      {parts
        .filter((p) => p.n > 0)
        .map((p) => (
          <span key={p.status} className={`flex items-center gap-2 text-[13px] font-medium ${p.status === "needs_you" ? "text-vermilion" : "text-ink-2"}`}>
            <Lamp status={p.status} />
            {p.word}
          </span>
        ))}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-9">
      <h2 className="mb-3 text-label-caps uppercase text-ink-3">{title}</h2>
      <Grid>{children}</Grid>
    </section>
  );
}

function Grid({ children }: { children: React.ReactNode }) {
  return <div className="grid grid-cols-1 gap-4 min-[760px]:grid-cols-2 min-[1400px]:grid-cols-3">{children}</div>;
}

// BotCard is one Bot: who it is, what it is doing (or did last, and will do
// next), and what waits for you there. The whole card opens it; the rows on
// it are links of their own above that.
function BotCard({ bot: b, i }: { bot: Bot; i: number }) {
  const m = mood(b);
  const others = b.people.filter((p) => !p.you);
  return (
    <article
      style={{ animationDelay: `${Math.min(i * 40, 240)}ms` }}
      className="rise blink group relative flex min-h-[188px] flex-col overflow-hidden rounded-card bg-surface p-5 shadow-card transition-[box-shadow,transform] duration-[200ms] ease-quiet hover:-translate-y-px hover:shadow-float motion-reduce:hover:translate-y-0"
    >
      {m === "needs_you" ? <span aria-hidden className="absolute inset-y-0 left-0 w-[2px] bg-vermilion" /> : null}
      <Link to="/bots/$botId/run" params={{ botId: b.id }} aria-label={b.name} className="absolute inset-0 rounded-card outline-none focus-visible:shadow-[inset_0_0_0_2px_var(--color-cobalt)]" />

      <div className="pointer-events-none flex items-start gap-3.5">
        <span className={`relative ${m === "stopped" ? "opacity-70 grayscale-[.4]" : ""}`}>
          <Crest index={b.crest} size={44} />
        </span>
        <div className="min-w-0 flex-1 pt-0.5">
          <div className="flex items-center gap-2">
            <span className="min-w-0 truncate text-card-title leading-5 text-ink">{b.name}</span>
          </div>
          <div className="mt-1 flex items-center gap-1.5">
            <Lamp status={m === "needs_you" ? "needs_you" : b.status} />
            <StatusWord status={m === "needs_you" ? "needs_you" : b.status} />
          </div>
        </div>
        {others.length ? (
          <div className="flex shrink-0 -space-x-1.5 pt-0.5" title={b.people.map((p) => p.email).join(", ")}>
            {b.people.slice(0, 3).map((p) => (
              <span key={p.userId} className="rounded-full ring-2 ring-surface">
                <Initials email={p.email} size={22} />
              </span>
            ))}
            {b.people.length > 3 ? <span className="pl-2.5 text-[11px] leading-[22px] text-ink-3">{`+${b.people.length - 3}`}</span> : null}
          </div>
        ) : null}
      </div>

      {b.description ? <p className="pointer-events-none mt-3 line-clamp-2 text-[13px] leading-[19px] text-ink-2">{b.description}</p> : null}

      <div className="mt-auto pt-4">
        <Doing bot={b} />
        <Waiting bot={b} />
      </div>
    </article>
  );
}

const row = "relative z-10 flex h-8 min-w-0 items-center gap-2 rounded-sm px-2 text-[12.5px] transition-colors duration-[160ms] hover:bg-pressed";

// Doing is what it is up to: each live conversation and its tool, else when
// it last worked and what it will do next on its own.
function Doing({ bot: b }: { bot: Bot }) {
  const runs = b.activity.filter((a) => a.kind !== "subagent");
  const agents = b.activity.filter((a) => a.kind === "subagent").length;
  if (runs.length || agents) {
    return (
      <ul className="-mx-2 space-y-px">
        {runs.slice(0, 2).map((a) => (
          <li key={a.chatId}>
            <Link {...chatLink(b.id, a.chatId)} className={`${row} text-ink-2 hover:text-ink`}>
              <Lamp status="working" />
              <span className="min-w-0 truncate">{sourceLabel(a.kind, a.title, t("New chat"))}</span>
              {a.step ? <span className="ml-auto shrink-0 rounded-xs bg-well px-1.5 py-px font-mono text-[11px] text-ink-3">{toolMeta(a.step).app}</span> : null}
            </Link>
          </li>
        ))}
        {runs.length > 2 ? <li className="px-2 text-[12px] text-ink-3">{t("…and {{count}} more", { count: runs.length - 2 })}</li> : null}
        {agents ? <li className="flex h-7 items-center gap-2 px-2 text-[12px] text-ink-3">{t("{{count}} subagents at work", { count: agents })}</li> : null}
      </ul>
    );
  }
  return (
    <div className="pointer-events-none space-y-1 text-[12.5px] leading-[18px] text-ink-3">
      <p>{b.lastActiveAt ? t("Last active {{when}}", { when: ago(b.lastActiveAt) }) : t("Has not worked yet")}</p>
      {b.nextRunAt ? (
        <p className="flex min-w-0 items-center gap-1.5">
          <Timer size={13} className="shrink-0" />
          <span className="min-w-0 truncate">{`${b.nextRunName} · ${nextWhen(b.nextRunAt)}`}</span>
        </p>
      ) : null}
    </div>
  );
}

// Waiting is what the Bot left for you: asks on its Dashboard, unread posts.
function Waiting({ bot: b }: { bot: Bot }) {
  if (!b.dashboardAsks && !b.feedUnread) return null;
  return (
    <div className="-mx-2 mt-2 flex flex-wrap gap-1 border-t border-line pt-2">
      {b.dashboardAsks ? (
        <Link to="/bots/$botId/dashboard" params={{ botId: b.id }} className={`${row} font-medium text-vermilion`}>
          <LayoutGrid size={14} />
          {t("{{count}} questions", { count: b.dashboardAsks })}
        </Link>
      ) : null}
      {b.feedUnread ? (
        <Link to="/bots/$botId/feed" params={{ botId: b.id }} className={`${row} font-medium text-cobalt`}>
          <Inbox size={14} />
          {t("{{count}} new in Feed", { count: b.feedUnread })}
        </Link>
      ) : null}
    </div>
  );
}

function CardSkeleton() {
  return (
    <div className="flex min-h-[188px] flex-col rounded-card bg-surface p-5 shadow-card" aria-hidden>
      <div className="flex gap-3.5">
        <div className="skeleton h-11 w-11 rounded-card" />
        <div className="flex-1 pt-1">
          <div className="skeleton mb-2.5 h-4 w-28 rounded-xs" />
          <div className="skeleton h-3 w-16 rounded-xs" />
        </div>
      </div>
      <div className="skeleton mt-auto h-3 w-40 rounded-xs" />
    </div>
  );
}

function Empty({ spaceId, build }: { spaceId: string; build: boolean }) {
  return (
    <div className="rise pt-[8vh]">
      <p className="mb-2 text-[40px] font-medium leading-[48px] tracking-[-0.02em]">{t("No Bots yet")}</p>
      <p className="mb-7 max-w-[460px] text-[14px] text-ink-2">{t("A Bot is its own machine. It does not share files with the others.")}</p>
      {build ? (
        <Link to="/w/$spaceId/new" params={{ spaceId }} className={btnClass("primary")}>
          <Plus size={16} />
          {t("New Bot")}
        </Link>
      ) : null}
    </div>
  );
}
