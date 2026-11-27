import { Plus, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { ArmedButton, SaveButton, useSave } from "../Feedback";
import { Btn } from "../Btn";
import { inputClass } from "../Field";
import { UI, type OperatorRule } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { Select } from "../Select";
import { Switch } from "../Switch";
import { t } from "../i18n.ts";
import { Band, Block, Empty, Group } from "./kit";

const callOptions = [
  { value: "", label: t("No call rule") },
  { value: "allow", label: t("Allow") },
  { value: "auto", label: t("Auto") },
  { value: "ask", label: t("Ask") },
  { value: "deny", label: t("Deny") },
];

const outputOptions = [
  { value: "", label: t("No output rule") },
  { value: "pass", label: t("Pass") },
  { value: "check", label: t("Check") },
  { value: "flag", label: t("Flag") },
  { value: "withhold", label: t("Withhold") },
];

// The operator's layer of the rules engine: one row per connector.action (or
// connector.*) that every Bot obeys. A locked row is a floor — an owner may
// tighten it, never loosen it; an unlocked one is only a default.
export function OperatorRulesPanel() {
  const q = useQuery(UI.method.listOperatorRules, {});
  const rules = q.data?.rules ?? [];
  const saver = useSave();
  const [err, setErr] = useState("");
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ connector: "", action: "", decision: "ask", output: "", locked: false });

  async function write(row: { connector: string; action: string; decision: string; output: string; locked: boolean }) {
    setErr("");
    await saver.run(async () => {
      await ui.setOperatorRule(row);
      await reload(UI.method.listOperatorRules, {});
    });
  }

  function run(fn: () => Promise<unknown>) {
    setErr("");
    void saver.run(fn).catch((e) => setErr(fail(e)));
  }

  return (
    <Group
      title={t("Operator rules")}
      note={t("A rule here applies to every Bot, on top of whatever its owner set. A locked row is a floor: an owner may make it stricter, never looser. An unlocked one is only a default the owner overrides. Use * as the action to cover a whole connector.")}
      action={
        adding ? null : (
          <Btn size="sm" onClick={() => setAdding(true)}>
            <Plus size={14} /> {t("Add rule")}
          </Btn>
        )
      }
    >
      {err ? (
        <Block>
          <p className="text-small leading-5 text-vermilion">{err}</p>
        </Block>
      ) : null}
      {rules.length === 0 && !adding ? <Empty>{t("No operator rules yet.")}</Empty> : null}
      {rules.map((r) => (
        <RuleLine
          key={`${r.connector}.${r.action}`}
          rule={r}
          onWrite={(next) => run(() => write(next))}
          onDelete={() =>
            run(async () => {
              await ui.deleteOperatorRule({ connector: r.connector, action: r.action });
              await reload(UI.method.listOperatorRules, {});
            })
          }
        />
      ))}
      {adding ? (
        <Band className="grid gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              className={`${inputClass} max-w-[180px] font-mono text-small`}
              placeholder={t("connector")}
              aria-label={t("connector")}
              value={draft.connector}
              onChange={(e) => setDraft({ ...draft, connector: e.target.value })}
            />
            <span className="text-ink-3">.</span>
            <input
              className={`${inputClass} max-w-[180px] font-mono text-small`}
              placeholder={t("action")}
              aria-label={t("action")}
              value={draft.action}
              onChange={(e) => setDraft({ ...draft, action: e.target.value })}
            />
            <Select className="max-w-[150px]" value={draft.decision} onChange={(v) => setDraft({ ...draft, decision: v })} options={callOptions} ariaLabel={t("Call rule")} />
            <Select className="max-w-[150px]" value={draft.output} onChange={(v) => setDraft({ ...draft, output: v })} options={outputOptions} ariaLabel={t("Output rule")} />
            <label className="flex items-center gap-2 text-small text-ink-2">
              <Switch on={draft.locked} onChange={(v) => setDraft({ ...draft, locked: v })} />
              {t("Locked")}
            </label>
          </div>
          <div className="flex items-center gap-2">
            <SaveButton
              size="sm"
              state={saver.state}
              disabled={draft.connector.trim() === "" || draft.action.trim() === ""}
              onClick={() =>
                run(async () => {
                  await write({ ...draft, connector: draft.connector.trim(), action: draft.action.trim() });
                  setDraft({ connector: "", action: "", decision: "ask", output: "", locked: false });
                  setAdding(false);
                })
              }
            >
              {t("Add rule")}
            </SaveButton>
            <Btn kind="ghost" size="sm" onClick={() => setAdding(false)}>
              {t("Cancel")}
            </Btn>
          </div>
        </Band>
      ) : null}
    </Group>
  );
}

// One row, written as it is changed: there is nothing to save in a batch.
function RuleLine({ rule, onWrite, onDelete }: { rule: OperatorRule; onWrite: (next: OperatorRule) => void; onDelete: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-5 py-3">
      <span className="min-w-[160px] flex-1 truncate font-mono text-small text-ink">
        {rule.connector}.{rule.action}
      </span>
      <Select className="w-[150px]" value={rule.decision} onChange={(v) => onWrite({ ...rule, decision: v })} options={callOptions} ariaLabel={t("Call rule")} />
      <Select className="w-[150px]" value={rule.output} onChange={(v) => onWrite({ ...rule, output: v })} options={outputOptions} ariaLabel={t("Output rule")} />
      <label className="flex items-center gap-2 text-small text-ink-2">
        <Switch on={rule.locked} onChange={(v) => onWrite({ ...rule, locked: v })} />
        {t("Locked")}
      </label>
      <ArmedButton iconOnly icon={<Trash2 size={14} />} title={t("Remove")} kind="ghost" onConfirm={onDelete} />
    </div>
  );
}
