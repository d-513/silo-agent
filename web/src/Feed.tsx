import { Inbox, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { apart, chatLink } from "./links";
import { ui } from "./api";
import { btnClass } from "./Btn";
import { Count } from "./Count";
import { ArmedButton } from "./Feedback";
import { SkeletonRows } from "./Field";
import { FilterChip } from "./FilterChip";
import { byDay, chips, isLong, matches, opensWith, shows, type Day, type Filter } from "./feedView";
import { UI, type Bot, type Chat, type FeedPost } from "./gen/silo/v1/ui_pb";
import { clockTime, fullTime } from "./format";
import { Md } from "./Md";
import { fail } from "./errors";
import { patch, recheck, reloadBot } from "./query";
import { SearchBox } from "./SearchBox";
import { sourceLabel } from "./sources";
import { locale, t } from "./i18n.ts";

// FeedPane is what the Bot posted with `feed`: a journal, newest first, one
// sheet per day, every post readable where it stands (a long one cut until
// asked for). It is not a conversation: a post is read here and taken to a
// chat (the main one, or one of its own) to be talked about, and its source
// line leads back to the automation or chat that posted it. Opening the Feed
// marks everything read; what was unread keeps its dot for this visit and is
// shown whole. The chips narrow by source, the field by words.

// PAGE is how many posts are drawn until the reader asks for older ones.
const PAGE = 30;

function dayLabel(d: Day<FeedPost>): string {
  if (d.days === 0) return t("Today");
  if (d.days === 1) return t("Yesterday");
  if (!d.date) return t("Earlier");
  const year = d.date.getFullYear() === new Date().getFullYear() ? {} : ({ year: "numeric" } as const);
  return d.date.toLocaleDateString(locale(), { weekday: "long", month: "short", day: "numeric", ...year });
}

function stamp(iso: string): { clock: string; full?: string } {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? { clock: "" } : { clock: clockTime(d), full: fullTime(d) };
}

// One post, whole: when and where it came from, its title, then what the Bot
// wrote in the reply serif. Nothing opens or closes; only a long post is cut
// until asked for. The actions under it are quiet words, not a reply box: the
// Feed is read, and a post is taken to a chat to be talked about.
function PostEntry({
  bot,
  post: p,
  first,
  whole,
  fresh,
  busy,
  onWhole,
  onQuote,
  onDelete,
}: {
  bot: Bot;
  post: FeedPost;
  first: boolean;
  whole: boolean;
  fresh: boolean;
  busy: boolean;
  onWhole: () => void;
  onQuote: (newChat: boolean) => void;
  onDelete: () => void;
}) {
  const src = sourceLabel(p.sourceKind, p.sourceName);
  const at = stamp(p.createdAt);
  const long = isLong(p.body);
  const quiet = "text-meta font-medium text-ink-3 transition-colors duration-[160ms] ease-quiet hover:text-ink disabled:opacity-50";
  return (
    <li className={`px-5 py-4 ${first ? "" : "shadow-[inset_0_1px_0_var(--color-line)]"}`}>
      <article>
        <p className="flex min-w-0 items-center gap-1.5 text-label leading-4 text-ink-3">
          {fresh ? <span aria-label={t("New")} title={t("New")} className="mr-0.5 h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" /> : null}
          <span title={at.full} className="shrink-0 font-mono tabular-nums">
            {at.clock}
          </span>
          {src ? (
            <>
              <span aria-hidden>·</span>
              {p.automationId ? (
                <Link to="/bots/$botId/automations/$automationId" params={{ botId: bot.id, automationId: p.automationId }} className="truncate hover:text-ink hover:underline">
                  {src}
                </Link>
              ) : (p.sourceKind === "chat" || p.sourceKind === "main") && p.chatId ? (
                <Link {...chatLink(bot.id, p.chatId)} className="truncate hover:text-ink hover:underline">
                  {src}
                </Link>
              ) : (
                <span className="truncate">{src}</span>
              )}
            </>
          ) : null}
        </p>
        {p.title ? <h4 className="mt-1.5 text-card-title leading-5 text-ink">{p.title}</h4> : null}
        <div className={`silo-reply mt-1 max-w-[760px] ${long && !whole ? "max-h-[252px] overflow-hidden" : ""}`}>
          <Md text={p.body} />
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-x-4 gap-y-1">
          {long ? (
            <button type="button" aria-expanded={whole} className="text-meta font-medium text-cobalt hover:text-cobalt-deep hover:underline" onClick={onWhole}>
              {whole ? t("Show less") : t("Read more")}
            </button>
          ) : null}
          <button type="button" className={quiet} disabled={busy} title={t("Take it up in the main chat")} onClick={(e) => onQuote(apart(e))}>
            {t("Discuss")}
          </button>
          <button type="button" className={quiet} disabled={busy} onClick={() => onQuote(true)}>
            {t("Discuss in a new chat")}
          </button>
          {bot.role === "manage" ? (
            <ArmedButton kind="ghost" size="sm" iconOnly className="ml-auto opacity-70 hover:opacity-100 focus-visible:opacity-100" title={t("Delete post")} icon={<Trash2 size={13} />} onConfirm={onDelete}>
              {t("Delete")}
            </ArmedButton>
          ) : null}
        </div>
      </article>
    </li>
  );
}

export function FeedPane({ bot, onError, onQuoted }: { bot: Bot; onError: (s: string) => void; onQuoted: (c: Chat) => void }) {
  const q = useQuery(UI.method.listFeed, { botId: bot.id });
  const posts = q.data?.posts ?? null;
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [quoting, setQuoting] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(PAGE);

  // Reload whenever the unread count moves (the bot row is polled), so a post
  // that lands while the Feed is open shows up and is marked read.
  useEffect(() => {
    void recheck(UI.method.listFeed, { botId: bot.id });
  }, [bot.id, bot.feedUnread]);
  useEffect(() => {
    const unread = (q.data?.posts ?? []).filter((p) => !p.read).map((p) => p.id);
    if (!unread.length) return;
    setFresh((cur) => new Set([...cur, ...unread]));
    setOpen((cur) => new Set([...cur, ...opensWith(unread)]));
    ui.markFeedRead({ botId: bot.id })
      .then(() => {
        patch(UI.method.listFeed, { botId: bot.id }, (r) => ({ ...r, posts: r.posts.map((p) => ({ ...p, read: true })) }));
        reloadBot(bot.id);
      })
      .catch(() => {});
  }, [q.data, bot.id]);
  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);
  useEffect(() => {
    setFresh(new Set());
    setOpen(new Set());
    setFilter("all");
    setQuery("");
  }, [bot.id]);
  useEffect(() => setLimit(PAGE), [bot.id, filter, query]);

  async function remove(id: string) {
    onError("");
    try {
      await ui.deleteFeedPost({ botId: bot.id, id });
      patch(UI.method.listFeed, { botId: bot.id }, (r) => ({ ...r, posts: r.posts.filter((p) => p.id !== id) }));
    } catch (e) {
      onError(fail(e));
    }
  }

  async function quote(id: string, newChat: boolean) {
    onError("");
    setQuoting(id);
    try {
      const r = await ui.quoteFeedPost({ botId: bot.id, id, newChat });
      if (r.chat) onQuoted(r.chat);
    } catch (e) {
      onError(fail(e));
    } finally {
      setQuoting("");
    }
  }

  const toggle = (id: string) =>
    setOpen((cur) => {
      const next = new Set(cur);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const sources = useMemo(() => chips(posts ?? []), [posts]);
  // A chip whose last post was deleted is no longer offered: fall back to All.
  const picked = sources.some((c) => c.key === filter) ? filter : "all";
  const shown = useMemo(() => (posts ?? []).filter((p) => shows(picked, p) && matches(p, query)), [posts, picked, query]);
  const days = useMemo(() => byDay(shown.slice(0, limit)), [shown, limit]);
  const older = shown.length - Math.min(shown.length, limit);
  const narrowed = picked !== "all" || query.trim() !== "";

  return (
    <div className="silo-page">
      <div className="mb-1 flex items-center gap-2.5">
        <h2 className="text-title text-ink">{t("Feed")}</h2>
        {posts && posts.length > 0 ? <Count n={posts.length} /> : null}
      </div>
      <p className="mb-4 text-ink-2">{t("What {{name}} reported from work you were not watching. Discuss a post to take it up in a chat.", { name: bot.name })}</p>

      {posts && posts.length > 0 ? (
        <div className="mb-5">
          <SearchBox value={query} onChange={setQuery} searching={false} placeholder={t("Search the Feed")} />
          {sources.length > 0 ? (
            <div className="mt-3 flex flex-wrap items-center gap-1.5">
              {sources.map((c) => (
                <FilterChip key={c.key} on={picked === c.key} onClick={() => setFilter(c.key)}>
                  {c.key === "all" ? t(c.label) : `${t(c.label)} (${c.n})`}
                </FilterChip>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      {posts === null ? (
        <SkeletonRows rows={4} height={72} />
      ) : posts.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-5 py-8 text-center">
          <Inbox size={20} className="text-ink-3" />
          <p className="text-ink-2">{t("No posts yet")}</p>
          <p className="max-w-sm text-meta text-ink-3">
            {t("{{name}} posts here what an automation found, what it finished in the background, and news that needs no answer.", { name: bot.name })}
          </p>
        </div>
      ) : shown.length === 0 ? (
        <div className="rounded-card bg-surface px-5 py-8 text-center shadow-card">
          <p className="text-row text-ink-2">{t("Nothing matches.")}</p>
          {narrowed ? (
            <button
              type="button"
              className="mt-2 text-meta font-medium text-cobalt hover:underline"
              onClick={() => {
                setQuery("");
                setFilter("all");
              }}
            >
              {t("Show all")}
            </button>
          ) : null}
        </div>
      ) : (
        <div className="space-y-6">
          {days.map((d) => (
            <section key={d.key}>
              <div className="mb-2 flex items-center justify-between px-1">
                <h3 className="text-section text-ink-3">{dayLabel(d)}</h3>
                <span className="font-mono text-micro tabular-nums text-ink-3">{d.posts.length}</span>
              </div>
              <ul className="overflow-hidden rounded-card bg-surface shadow-card">
                {d.posts.map((p, i) => (
                  <PostEntry
                    key={p.id}
                    bot={bot}
                    post={p}
                    first={i === 0}
                    whole={open.has(p.id)}
                    fresh={fresh.has(p.id)}
                    busy={quoting !== ""}
                    onWhole={() => toggle(p.id)}
                    onQuote={(newChat) => void quote(p.id, newChat)}
                    onDelete={() => void remove(p.id)}
                  />
                ))}
              </ul>
            </section>
          ))}
          {older > 0 ? (
            <div className="flex justify-center">
              <button type="button" className={btnClass("secondary", "", "sm")} onClick={() => setLimit((n) => n + PAGE)}>
                {t("Show older posts ({{n}})", { n: older })}
              </button>
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}
