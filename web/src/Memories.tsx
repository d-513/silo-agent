import { Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState, type FormEvent } from "react";
import { ui } from "./api";
import { ArmedButton, SaveButton, useSave } from "./Feedback";
import { Panel } from "./Field";
import { SearchBox } from "./SearchBox";
import { PromptWell } from "./Settings";
import { useSearch } from "./useSearch";
import { UI, type Bot, type Memory } from "./gen/silo/v1/ui_pb";
import { fail } from "./errors";
import { patch, setBot } from "./query";
import { day } from "./format";

// match turns a cosine distance (0 = same, 2 = opposite) into a 0–100 score.
function match(distance: number) {
  return Math.round(Math.max(0, Math.min(1, 1 - distance)) * 100);
}

function CorePanel({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [memory, setMemory] = useState(bot.memory);
  const saver = useSave();
  useEffect(() => {
    setMemory(bot.memory);
  }, [bot.id, bot.memory]);
  async function save(e: FormEvent) {
    e.preventDefault();
    onError("");
    try {
      const next = await saver.run(() =>
        ui.updateBot({
          id: bot.id,
          name: bot.name,
          description: bot.description,
          soul: bot.soul,
          memory,
          autoApprove: bot.autoApprove,
          model: bot.model,
        }),
      );
      setBot(next);
    } catch (ex) {
      onError(fail(ex));
    }
  }
  return (
    <form onSubmit={save}>
      <Panel title="Core memory" note="Always in the prompt, every run. Keep only what every conversation needs; the Bot edits it with core_memory.">
        <PromptWell label="CORE MEMORY" hint="Lasting facts. Compact past 8000." value={memory} onChange={setMemory} />
        <div className="mt-4 flex items-center gap-3">
          <SaveButton type="submit" state={saver.state} disabled={memory === bot.memory}>
            Save core memory
          </SaveButton>
        </div>
      </Panel>
    </form>
  );
}

// LongTermPanel lists the Bot's pgvector memories, newest first, or ranks them
// by meaning for a query. The Bot writes them with `remember`; the human reads,
// searches and deletes.
function LongTermPanel({ botId, onError }: { botId: string; onError: (s: string) => void }) {
  const q = useQuery(UI.method.listMemories, { botId });
  const all = q.data?.memories ?? null;
  const [query, setQuery] = useState("");
  const { hits, setHits, searching, error: searchErr } = useSearch<Memory>(query, (q) => ui.searchMemories({ botId, query: q }).then((r) => r.memories), 350, [botId]);

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

  const searchMode = query.trim() !== "";
  const rows = searchMode ? hits : all;
  const count = all ? `${all.length} saved. ` : "";
  return (
    <Panel
      title="Long-term memories"
      note={`${count}Saved by the Bot with remember, or collected from chats once they go quiet, and found by meaning. Lessons are pitfalls and what worked. Only the closest few reach a run.`}
      padded={false}
      className="mt-4"
    >
      <div className="px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)]">
        <SearchBox value={query} onChange={setQuery} searching={searching} placeholder="Search by meaning, e.g. “what does the user drink”" />
        {searchErr && <p className="mt-2 text-[12.5px] text-vermilion">{searchErr}</p>}
      </div>
      {rows === null ? (
        <p className="px-5 py-4 text-ink-3">{searchMode ? "Searching…" : "Loading…"}</p>
      ) : rows.length === 0 ? (
        <p className="px-5 py-4 text-ink-3">
          {searchMode ? (searchErr ? "Search failed." : "Nothing matches.") : "None yet. The Bot adds them as it learns durable facts."}
        </p>
      ) : (
        rows.map((m) => (
          <div key={m.id} className="flex items-start gap-4 px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
            <div className="min-w-0 flex-1">
              <p className="break-words whitespace-pre-wrap">{m.content}</p>
              <p className="mt-0.5 font-mono text-[11px] text-ink-3">
                {m.kind === "lesson" ? <span className="mr-1.5 rounded-xs bg-cobalt-pale px-1.5 py-px text-cobalt-deep">lesson</span> : null}
                {searchMode ? `${match(m.distance)}% match · ` : ""}
                {day(m.createdAt)}
                {m.chatId ? " · collected" : ""}
                {m.lastUsedAt ? ` · recalled ${day(m.lastUsedAt)}` : ""}
              </p>
            </div>
            <ArmedButton
              kind="ghost"
              size="sm"
              iconOnly
              className="shrink-0"
              title="Delete memory"
              icon={<Trash2 size={13} />}
              onConfirm={() => void remove(m.id)}
            >
              Delete
            </ArmedButton>
          </div>
        ))
      )}
    </Panel>
  );
}

export function MemoriesPane({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">Memories</h2>
      <p className="mb-6 text-ink-2">Core memory rides in every prompt. Long-term memories are unlimited and recalled by meaning.</p>
      <CorePanel bot={bot} onError={onError} />
      <LongTermPanel botId={bot.id} onError={onError} />
    </div>
  );
}
