import { Inbox, MessageSquarePlus, MessageSquareQuote, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { apart, chatLink } from "./links";
import { ui } from "./api";
import { Btn } from "./Btn";
import { ArmedButton } from "./Feedback";
import { UI, type Bot, type Chat, type FeedPost } from "./gen/silo/v1/ui_pb";
import { feedStamp } from "./format";
import { Md } from "./Md";
import { fail } from "./errors";
import { patch, recheck, reloadBot } from "./query";
import { sourceLabel } from "./sources";
import { t } from "./i18n.ts";

// FeedPane is the Bot's read-only inbox: posts it made with `feed`, newest
// first. Opening it marks everything read; posts that were unread keep their
// dot for this visit. The human can delete a post or reply to it: in the main
// chat by default, or in a chat of its own.
const src = (p: FeedPost) => sourceLabel(p.sourceKind, p.sourceName);

export function FeedPane({ bot, onError, onQuoted }: { bot: Bot; onError: (s: string) => void; onQuoted: (c: Chat) => void }) {
  const q = useQuery(UI.method.listFeed, { botId: bot.id });
  const posts = q.data?.posts ?? null;
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  const [quoting, setQuoting] = useState("");

  // Reload whenever the unread count moves (the bot row is polled), so a post
  // that lands while the Feed is open shows up and is marked read.
  useEffect(() => {
    void recheck(UI.method.listFeed, { botId: bot.id });
  }, [bot.id, bot.feedUnread]);
  useEffect(() => {
    const unread = (q.data?.posts ?? []).filter((p) => !p.read).map((p) => p.id);
    if (!unread.length) return;
    setFresh((cur) => new Set([...cur, ...unread]));
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
  useEffect(() => setFresh(new Set()), [bot.id]);

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

  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">{t("Feed")}</h2>
      <p className="mb-6 text-ink-2">{t("What this Bot posted for you to read later. Reply to a post in the main chat, or in a chat of its own.")}</p>
      {posts === null ? (
        <p className="text-ink-3">{t("Loading…")}</p>
      ) : posts.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <Inbox size={20} className="text-ink-3" />
          <p className="text-ink-2">{t("Nothing yet.")}</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">{t("The Bot posts here with the feed tool — from a chat, an automation, or a channel.")}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {posts.map((p) => (
            <article key={p.id} className="group rounded-card bg-surface px-5 py-4 shadow-card">
              <header className="mb-2 flex items-start gap-3">
                <div className="min-w-0 flex-1">
                  {p.title ? <h3 className="text-card-title leading-5 text-ink">{p.title}</h3> : null}
                  <p className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[12px] leading-4 text-ink-3">
                    {fresh.has(p.id) ? <span aria-label={t("New")} className="h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" /> : null}
                    <span className="shrink-0 font-mono">{feedStamp(p.createdAt)}</span>
                    {src(p) ? (
                      <>
                        <span aria-hidden>·</span>
                        {p.sourceKind === "chat" && p.chatId ? (
                          <Link {...chatLink(bot.id, p.chatId)} className="truncate hover:text-ink hover:underline">
                            {src(p)}
                          </Link>
                        ) : (
                          <span className="truncate">{src(p)}</span>
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
                    title={t("Reply in the main chat")}
                    aria-label={t("Reply in the main chat")}
                    disabled={!!quoting}
                    icon={<MessageSquareQuote size={14} />}
                    onClick={(e) => void quote(p.id, apart(e))}
                  />
                  <Btn
                    kind="ghost"
                    size="sm"
                    iconOnly
                    title={t("Reply in a new chat")}
                    aria-label={t("Reply in a new chat")}
                    disabled={!!quoting}
                    icon={<MessageSquarePlus size={14} />}
                    onClick={() => void quote(p.id, true)}
                  />
                  <ArmedButton kind="ghost" size="sm" iconOnly title={t("Delete post")} icon={<Trash2 size={13} />} onConfirm={() => void remove(p.id)}>
                    {t("Delete")}
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
