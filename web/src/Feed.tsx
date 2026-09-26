import { Inbox, MessageSquareQuote, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ui } from "./api";
import { Btn } from "./Btn";
import { ArmedButton } from "./Feedback";
import type { Bot, Chat, FeedPost } from "./gen/silo/v1/ui_pb";
import { Md } from "./Thread";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function stamp(iso: string) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const today = new Date().toDateString() === d.toDateString();
  return today
    ? d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
    : d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function sourceLabel(p: FeedPost) {
  switch (p.sourceKind) {
    case "automation":
      return `Automation · ${p.sourceName}`;
    case "channel":
      return `Channel · ${p.sourceName}`;
    case "chat":
      return p.sourceName || "Chat";
    default:
      return "";
  }
}

// FeedPane is the Bot's read-only inbox: posts it made with `feed`, newest
// first. Opening it marks everything read; posts that were unread keep their
// dot for this visit. The human can delete a post or quote it into a new chat.
export function FeedPane({ bot, onError, onQuoted }: { bot: Bot; onError: (s: string) => void; onQuoted: (c: Chat) => void }) {
  const [posts, setPosts] = useState<FeedPost[] | null>(null);
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  const [quoting, setQuoting] = useState("");
  const seq = useRef(0);

  // Reload whenever the unread count moves (the bot row is polled), so a post
  // that lands while the Feed is open shows up and is marked read.
  useEffect(() => {
    let dead = false;
    const n = ++seq.current;
    ui.listFeed({ botId: bot.id })
      .then((r) => {
        if (dead || n !== seq.current) return;
        setPosts(r.posts);
        const unread = r.posts.filter((p) => !p.read).map((p) => p.id);
        if (unread.length) {
          setFresh((cur) => new Set([...cur, ...unread]));
          ui.markFeedRead({ botId: bot.id }).catch(() => {});
        }
      })
      .catch((e) => {
        if (!dead) onError(fail(e));
      });
    return () => {
      dead = true;
    };
  }, [bot.id, bot.feedUnread]);

  useEffect(() => {
    setPosts(null);
    setFresh(new Set());
  }, [bot.id]);

  async function remove(id: string) {
    onError("");
    try {
      await ui.deleteFeedPost({ botId: bot.id, id });
      setPosts((cur) => (cur ?? []).filter((p) => p.id !== id));
    } catch (e) {
      onError(fail(e));
    }
  }

  async function quote(id: string) {
    onError("");
    setQuoting(id);
    try {
      const r = await ui.quoteFeedPost({ botId: bot.id, id });
      if (r.chat) onQuoted(r.chat);
    } catch (e) {
      onError(fail(e));
    } finally {
      setQuoting("");
    }
  }

  return (
    <div className="silo-page pb-12">
      <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Feed</h2>
      <p className="mb-6 text-ink-2">What this Bot posted for you to read later. Quote a post to talk about it in a new chat.</p>
      {posts === null ? (
        <p className="text-ink-3">Loading…</p>
      ) : posts.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <Inbox size={20} className="text-ink-3" />
          <p className="text-ink-2">Nothing yet.</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">The Bot posts here with the feed tool — from a chat, an automation, or a channel.</p>
        </div>
      ) : (
        <div className="space-y-3">
          {posts.map((p) => (
            <article key={p.id} className="group rounded-card bg-surface px-5 py-4 shadow-card">
              <header className="mb-2 flex items-start gap-3">
                <div className="min-w-0 flex-1">
                  {p.title ? <h3 className="text-[15px] leading-5 font-semibold tracking-[-0.01em] text-ink">{p.title}</h3> : null}
                  <p className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[12px] leading-4 text-ink-3">
                    {fresh.has(p.id) ? <span aria-label="New" className="h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" /> : null}
                    <span className="shrink-0 font-mono">{stamp(p.createdAt)}</span>
                    {sourceLabel(p) ? (
                      <>
                        <span aria-hidden>·</span>
                        {p.sourceKind === "chat" && p.chatId ? (
                          <Link to={`/bots/${bot.id}/run/${p.chatId}`} className="truncate hover:text-ink hover:underline">
                            {sourceLabel(p)}
                          </Link>
                        ) : (
                          <span className="truncate">{sourceLabel(p)}</span>
                        )}
                      </>
                    ) : null}
                  </p>
                </div>
                <span className="flex shrink-0 items-center gap-1">
                  <Btn
                    kind="ghost"
                    size="sm"
                    iconOnly
                    title="Quote in a new chat"
                    aria-label="Quote in a new chat"
                    disabled={!!quoting}
                    icon={<MessageSquareQuote size={14} />}
                    onClick={() => void quote(p.id)}
                  />
                  <ArmedButton kind="ghost" size="sm" iconOnly title="Delete post" icon={<Trash2 size={13} />} onConfirm={() => void remove(p.id)}>
                    Delete
                  </ArmedButton>
                </span>
              </header>
              <div className="silo-reply">
                <Md text={p.body} />
              </div>
            </article>
          ))}
        </div>
      )}
    </div>
  );
}
