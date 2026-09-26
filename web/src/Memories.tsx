import { Search, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ui } from "./api";
import { ArmedButton, SaveButton, Spinner, useSave } from "./Feedback";
import { Panel, inputClass } from "./Field";
import { PromptWell } from "./Settings";
import type { Bot, Memory } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function day(iso: string) {
  return iso ? new Date(iso).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "";
}

// match turns a cosine distance (0 = same, 2 = opposite) into a 0–100 score.
function match(distance: number) {
  return Math.round(Math.max(0, Math.min(1, 1 - distance)) * 100);
}

function CorePanel({ bot, onSaved, onError }: { bot: Bot; onSaved: (b: Bot) => void; onError: (s: string) => void }) {
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
      onSaved(next);
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
  const [all, setAll] = useState<Memory[] | null>(null);
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<Memory[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [searchErr, setSearchErr] = useState("");
  const seq = useRef(0);

  useEffect(() => {
    let dead = false;
    setAll(null);
    setQuery("");
    ui.listMemories({ botId })
      .then((r) => {
        if (!dead) setAll(r.memories);
      })
      .catch((e) => {
        if (!dead) onError(fail(e));
      });
    return () => {
      dead = true;
    };
  }, [botId]);

  useEffect(() => {
    const q = query.trim();
    const n = ++seq.current;
    setSearchErr("");
    if (!q) {
      setHits(null);
      setSearching(false);
      return;
    }
    setSearching(true);
    const t = setTimeout(() => {
      ui.searchMemories({ botId, query: q })
        .then((r) => {
          if (n === seq.current) setHits(r.memories);
        })
        .catch((e) => {
          if (n === seq.current) {
            setHits([]);
            setSearchErr(fail(e));
          }
        })
        .finally(() => {
          if (n === seq.current) setSearching(false);
        });
    }, 350);
    return () => clearTimeout(t);
  }, [query, botId]);

  async function remove(id: string) {
    onError("");
    try {
      await ui.deleteMemory({ botId, id });
      setAll((cur) => (cur ?? []).filter((m) => m.id !== id));
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
      note={`${count}Saved by the Bot with remember and found by meaning. Only the closest few reach a run.`}
      padded={false}
      className="mt-4"
    >
      <div className="px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)]">
        <label className="relative block">
          <Search size={14} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-3" />
          <input
            className={`${inputClass} w-full pr-9 pl-8`}
            placeholder="Search by meaning, e.g. “what does the user drink”"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setQuery("");
            }}
          />
          <span className="absolute top-1/2 right-2.5 -translate-y-1/2">
            {searching ? (
              <Spinner size={13} />
            ) : searchMode ? (
              <button type="button" className="text-ink-3 hover:text-ink" title="Clear search" onClick={() => setQuery("")}>
                <X size={14} />
              </button>
            ) : null}
          </span>
        </label>
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
                {searchMode ? `${match(m.distance)}% match · ` : ""}
                {day(m.createdAt)}
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

export function MemoriesPane({ bot, onSaved, onError }: { bot: Bot; onSaved: (b: Bot) => void; onError: (s: string) => void }) {
  return (
    <div className="silo-page pb-12">
      <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Memories</h2>
      <p className="mb-6 text-ink-2">Core memory rides in every prompt. Long-term memories are unlimited and recalled by meaning.</p>
      <CorePanel bot={bot} onSaved={onSaved} onError={onError} />
      <LongTermPanel botId={bot.id} onError={onError} />
    </div>
  );
}
