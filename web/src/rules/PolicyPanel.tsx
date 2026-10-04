import { SaveButton } from "../Feedback";
import { Panel, textareaClass } from "../Field";

// The free-text policy the approval model reads when a rule is set to Auto.
export function PolicyPanel({
  policy,
  saved,
  state,
  onChange,
  onSave,
}: {
  policy: string;
  saved: string;
  state: "idle" | "saving" | "saved";
  onChange: (v: string) => void;
  onSave: () => void;
}) {
  return (
    <Panel
      title="Auto-approve policy"
      note="Read by the approval model when a rule is set to Auto. Write the actions this Bot may run without a human. Empty means every Auto rule asks."
      className="mb-5"
    >
      <textarea
        className={`${textareaClass} h-[120px] font-mono text-[13px] leading-5`}
        placeholder="e.g. Auto-approve read-only file reads, web searches, and screenshots. Ask before anything that writes, deletes, sends a message, or touches a secret."
        value={policy}
        onChange={(e) => onChange(e.target.value)}
      />
      <div className="mt-3 flex items-center gap-3">
        <SaveButton state={state} onClick={onSave} disabled={state === "idle" && policy === saved}>
          Save policy
        </SaveButton>
      </div>
    </Panel>
  );
}
