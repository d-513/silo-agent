import { Plus, Trash2, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { SaveButton, useSave } from "../Feedback";
import { Btn } from "../Btn";
import { Panel, inputClass } from "../Field";
import { ConfigSource, type ConnectorVar } from "../gen/silo/v1/ui_pb";

type VarDraft = { name: string; value: string; source: ConfigSource; envName: string };

export function ConnectorVarsPanel({
  vars,
  onSave,
}: {
  vars: ConnectorVar[];
  onSave: (list: { name: string; value: string }[]) => Promise<void>;
}) {
  const [draft, setDraft] = useState<VarDraft[]>([]);
  const varSaver = useSave();
  useEffect(() => {
    setDraft(vars.map((v) => ({ name: v.name, value: v.value, source: v.source, envName: v.envName })));
  }, [vars]);

  function update(i: number, patch: Partial<VarDraft>) {
    setDraft((prev) => prev.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  }

  function save() {
    const out = draft
      .filter((r) => r.source !== ConfigSource.ENV)
      .map((r) => ({ name: r.name.trim(), value: r.value }))
      .filter((r) => r.name);
    void varSaver.run(() => onSave(out)).catch(() => {});
  }

  return (
    <Panel
      title="Connector variables"
      note="Use these in a connector URL, headers, OAuth fields, command, arguments, or env values as ${NAME}."
    >
      <p className="mb-4 flex items-start gap-2 rounded-sm border-l-4 border-vermilion bg-well px-3 py-2 text-[13px] text-ink">
        <TriangleAlert className="mt-0.5 shrink-0 text-vermilion" size={16} />
        <span>
          These are variables, not secrets. Values are stored in plain text in <span className="font-mono">silo.yaml</span> and are not
          protected — anyone who can read the config can see them. Use a Bot Secret for credentials.
        </span>
      </p>
      {draft.length === 0 ? (
        <p className="mb-3 text-[13px] text-ink-2">No variables yet.</p>
      ) : (
        <div className="mb-3 grid gap-2">
          {draft.map((r, i) => {
            const locked = r.source === ConfigSource.ENV;
            return (
              <div key={i} className="flex flex-wrap items-center gap-2">
                <input
                  className={`${inputClass} w-48 font-mono text-[13px]`}
                  placeholder="NAME"
                  value={r.name}
                  disabled={locked}
                  onChange={(e) => update(i, { name: e.target.value })}
                />
                <span className="text-ink-3">=</span>
                <input
                  className={`${inputClass} min-w-[160px] flex-1 font-mono text-[13px]`}
                  placeholder="value"
                  value={r.value}
                  disabled={locked}
                  onChange={(e) => update(i, { value: e.target.value })}
                />
                {locked ? (
                  <span className="inline-flex items-center gap-1 text-[11px] text-vermilion" title={r.envName}>
                    <TriangleAlert size={14} />
                    {r.envName}
                  </span>
                ) : (
                  <button
                    type="button"
                    title="Remove"
                    className="rounded-sm p-1 text-ink-2 transition-colors hover:bg-pressed hover:text-vermilion"
                    onClick={() => setDraft((prev) => prev.filter((_, j) => j !== i))}
                  >
                    <Trash2 size={14} />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
      <div className="flex items-center gap-3">
        <Btn onClick={() => setDraft((prev) => [...prev, { name: "", value: "", source: ConfigSource.DEFAULT, envName: "" }])}>
          <Plus size={14} /> Add variable
        </Btn>
        <SaveButton state={varSaver.state} onClick={save}>
          Save variables
        </SaveButton>
      </div>
    </Panel>
  );
}
