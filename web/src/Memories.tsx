import { Brain, MessageCircle, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ui } from "./api";
import { groupChats } from "./bot/chatGroups";
import { ArmedButton } from "./Feedback";
import { ErrorWell, SkeletonRows } from "./Field";
import { chatLink } from "./links";
import { SearchBox } from "./SearchBox";
import { useSearch } from "./useSearch";
import { UI, type Bot, type Memory } from "./gen/silo/v1/ui_pb";
import { fail } from "./errors";
import { patch } from "./query";
import { day, fullTime } from "./format";
import { t } from "./i18n.ts";

// match turns a cosine distance (0 = same, 2 = opposite) into a 0–100 score.
function match(distance: number) {
  return Math.round(Math.max(0, Math.min(1, 1 - distance)) * 100);
}

function MemoryRow({
  botId,
  m,
  score,
  onDelete,
}: {
  botId: string;
  m: Memory;
  score?: number;
  onDelete: () => void;
}) {
  const created = m.createdAt ? new Date(m.createdAt) : null;
  const full = created && !Number.isNaN(created.getTime()) ? fullTime(created) : undefined;
  return (
    <li className="group flex items-start gap-4 px-5 py-3.5 shadow-[inset_0_-1px_0_var(--color-line)] transition-colors duration-[160ms] ease-quiet last:shadow-none hover:bg-well/45">
      {score !== undefined ? (
        <span className="mt-0.5 shrink-0 rounded-xs bg-cobalt-pale px-1.5 py-0.5 font-mono text-[11px] font-medium tabular-nums text-cobalt">
          {t("{{percent}}% match", { percent: score })}
        </span>
      ) : null}
      <div className="min-w-0 flex-1">
        <p className="text-[14px] leading-[22px] break-words whitespace-pre-wrap text-ink">{m.content}</p>
        <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11.5px] leading-4 text-ink-3">
          {m.createdAt ? <span title={full}>{day(m.createdAt)}</span> : null}
          {m.chatId ? (
            <>
              <span aria-hidden>·</span>
              <Link
                {...chatLink(botId, m.chatId)}
                title={t("Open the chat it started")}
                className="inline-flex items-center gap-1 text-ink-3 transition-colors hover:text-cobalt hover:underline"
              >
                <MessageCircle size={11} />
                <span>{t("collected")}</span>
              </Link>
            </>
          ) : null}
          {m.lastUsedAt ? (
            <>
              <span aria-hidden>·</span>
              <span>{t("recalled {{date}}", { date: day(m.lastUsedAt) })}</span>
            </>
          ) : null}
        </p>
      </div>
      <ArmedButton
        kind="ghost"
        size="sm"
        iconOnly
        className="shrink-0 opacity-70 group-hover:opacity-100 focus-visible:opacity-100"
        title={t("Delete memory")}
        icon={<Trash2 size={13} />}
        onConfirm={onDelete}
      >
        {t("Delete")}
      </ArmedButton>
    </li>
  );
}

// MemoriesPane lists the Bot's memories, grouped by when they were saved, or
// ranks them by meaning for a query. The Bot writes them with `remember` (or
// the collector reads quiet chats); the human reads, searches and deletes.
export function MemoriesPane({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const botId = bot.id;
  const q = useQuery(UI.method.listMemories, { botId });
  const all = q.data?.memories ?? null;
  const [query, setQuery] = useState("");
  const { hits, setHits, searching, error: searchErr } = useSearch<Memory>(
    query,
    (s) => ui.searchMemories({ botId, query: s }).then((r) => r.memories),
    350,
    [botId],
  );

  useEffect(() => setQuery(""), [botId]);
  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);

  async function remove(id: string) {
    onError("");
    try {
      await ui.deleteMemory({ botId, id });
      patch(UI.method.listMemories, { botId }, (r) => ({ ...r, memories: r.memories.filter((m) => m.id !== id) }));
      setHits((cur) => (cur ? cur.filter((m) => m.id !== id) : cur));
    } catch (e) {
      onError(fail(e));
    }
  }

  const groups = useMemo(() => {
    if (!all) return [];
    return groupChats(all.map((m) => ({ ...m, updatedAt: m.createdAt })));
  }, [all]);

  const searchMode = query.trim() !== "";
  const rows = searchMode ? hits : all;

  return (
    <div className="silo-page pb-12">
      <div className="mb-1 flex items-center gap-2.5">
        <h2 className="text-title text-ink">{t("Memories")}</h2>
        {all && all.length > 0 ? (
          <span className="rounded-full bg-well px-2 py-0.5 font-mono text-[11px] font-medium tabular-nums text-ink-3">
            {all.length}
          </span>
        ) : null}
      </div>
      <p className="mb-6 text-ink-2">
        {t("Durable facts the Bot remembers across conversations. The Bot sees all of them while they are few, a summary once they are many, and searches them by meaning.")}
      </p>

      {(all === null || all.length > 0 || searchMode) && (
        <div className="mb-5">
          <SearchBox
            value={query}
            onChange={setQuery}
            searching={searching}
            placeholder={t("Search by meaning, e.g. “what does the user drink”")}
          />
          {searchErr ? <ErrorWell className="mt-2">{searchErr}</ErrorWell> : null}
        </div>
      )}

      {rows === null ? (
        <SkeletonRows rows={4} height={64} />
      ) : !searchMode && rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <Brain size={20} className="text-ink-3" />
          <p className="text-ink-2">{t("Nothing yet.")}</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">
            {t("Saved by the Bot with remember, or collected from chats once they go quiet, and found by meaning.")}
          </p>
        </div>
      ) : searchMode && rows.length === 0 ? (
        <div className="rounded-card bg-surface px-6 py-10 text-center text-ink-3 shadow-card">
          <p className="text-[13.5px] text-ink-2">{searchErr ? t("Search failed.") : t("Nothing matches.")}</p>
          <button type="button" className="mt-2 text-[12.5px] font-medium text-cobalt hover:underline" onClick={() => setQuery("")}>
            {t("Clear search")}
          </button>
        </div>
      ) : searchMode ? (
        <ul className="overflow-hidden rounded-card bg-surface shadow-card">
          {rows.map((m) => (
            <MemoryRow key={m.id} botId={botId} m={m} score={match(m.distance)} onDelete={() => void remove(m.id)} />
          ))}
        </ul>
      ) : (
        <div className="space-y-6">
          {groups.map((g) => (
            <section key={g.key}>
              <div className="mb-2 flex items-center justify-between px-1">
                <h3 className="text-label-caps uppercase text-ink-3">{g.label}</h3>
                <span className="font-mono text-[11px] tabular-nums text-ink-3">{g.chats.length}</span>
              </div>
              <ul className="overflow-hidden rounded-card bg-surface shadow-card">
                {g.chats.map((m) => (
                  <MemoryRow key={m.id} botId={botId} m={m} onDelete={() => void remove(m.id)} />
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
