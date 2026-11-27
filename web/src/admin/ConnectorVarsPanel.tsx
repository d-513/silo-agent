import { Plus, Trash2, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { SaveButton, useSave } from "../Feedback";
import { Btn } from "../Btn";
import { inputClass } from "../Field";
import { ConfigSource, type ConnectorVar } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Trans } from "../Trans.tsx";
import { Band, Empty, Group, Notice } from "./kit";

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
    <Group
      title={t("Connector variables")}
      note={t("Use these in a connector URL, headers, OAuth fields, command, arguments, or env values as ${NAME}.")}
      action={
        <SaveButton size="sm" state={varSaver.state} onClick={save}>
          {t("Save variables")}
        </SaveButton>
      }
    >
      <Notice tone="warn">
        <Trans i18nKey="These are variables, not secrets. Values are stored in plain text in <mono>silo.yaml</mono> and are not protected — anyone who can read the config can see them. Use a Bot Secret for credentials." components={{ mono: <span className="font-mono" /> }} />
      </Notice>
      {draft.length === 0 ? <Empty>{t("No variables yet.")}</Empty> : null}
      {draft.map((r, i) => {
        const locked = r.source === ConfigSource.ENV;
        return (
          <div key={i} className="flex flex-wrap items-center gap-2 px-5 py-2.5">
            <input className={`${inputClass} w-48 font-mono text-small`} placeholder={"NAME"} aria-label={t("Name")} value={r.name} disabled={locked} onChange={(e) => update(i, { name: e.target.value })} />
            <span className="text-ink-3">=</span>
            <input
              className={`${inputClass} min-w-[160px] flex-1 font-mono text-small`}
              placeholder={t("value")}
              aria-label={t("value")}
              value={r.value}
              disabled={locked}
              onChange={(e) => update(i, { value: e.target.value })}
            />
            {locked ? (
              <span className="inline-flex items-center gap-1 font-mono text-micro text-vermilion" title={r.envName}>
                <TriangleAlert size={12} />
                {r.envName}
              </span>
            ) : (
              <Btn kind="ghost" size="sm" iconOnly title={t("Remove")} aria-label={t("Remove")} className="hover:text-vermilion" onClick={() => setDraft((prev) => prev.filter((_, j) => j !== i))}>
                <Trash2 size={14} />
              </Btn>
            )}
          </div>
        );
      })}
      <Band>
        <Btn size="sm" onClick={() => setDraft((prev) => [...prev, { name: "", value: "", source: ConfigSource.DEFAULT, envName: "" }])}>
          <Plus size={14} /> {t("Add variable")}
        </Btn>
      </Band>
    </Group>
  );
}
