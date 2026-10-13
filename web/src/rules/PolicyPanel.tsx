import type { ReactNode } from "react";
import { SaveButton } from "../Feedback";
import { Panel, textareaClass } from "../Field";
import { t } from "../i18n.ts";

// One of the Bot's two free-text policies: what the approval model reads for a
// rule set to Auto, or what the filter model reads for a result set to Check.
// The caller words it; `extra` sits beside Save.
export function PolicyPanel({
  title,
  note,
  placeholder,
  policy,
  saved,
  state,
  onChange,
  onSave,
  extra,
}: {
  title: string;
  note: string;
  placeholder: string;
  policy: string;
  saved: string;
  state: "idle" | "saving" | "saved";
  onChange: (v: string) => void;
  onSave: () => void;
  extra?: ReactNode;
}) {
  return (
    <Panel title={title} note={note} className="mb-5">
      <textarea
        className={`${textareaClass} h-[120px] font-mono text-[13px] leading-5`}
        placeholder={placeholder}
        value={policy}
        onChange={(e) => onChange(e.target.value)}
      />
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <SaveButton state={state} onClick={onSave} disabled={state === "idle" && policy === saved}>
          {t("Save policy")}
        </SaveButton>
        {extra}
      </div>
    </Panel>
  );
}
