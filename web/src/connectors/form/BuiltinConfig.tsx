import { ChevronRight } from "lucide-react";
import type { ChannelField } from "../../gen/silo/v1/ui_pb";
import { FieldInput } from "../../FieldInput";
import type { ConnectorDraft } from "./draft";

// BuiltinConfig is a built-in connector's account form, generated from the
// fields its Go code declares. Advanced fields fold under "More settings".
export function BuiltinConfig({ value, onChange }: { value: ConnectorDraft; onChange: (next: ConnectorDraft) => void }) {
  const input = (f: ChannelField) => (
    <FieldInput
      key={f.key}
      field={f}
      value={value.config[f.key] ?? ""}
      isSet={value.secretsSet.includes(f.key)}
      setValue={(v) => onChange({ ...value, config: { ...value.config, [f.key]: v } })}
    />
  );
  const more = value.fields.filter((f) => f.advanced);
  return (
    <div className="mb-3 space-y-3">
      {value.fields.filter((f) => !f.advanced).map(input)}
      {more.length > 0 && (
        <details className="group">
          <summary className="flex cursor-pointer items-center gap-2 rounded-sm bg-well px-3 py-2 text-[12px] font-medium tracking-wide text-ink-3">
            <ChevronRight size={12} className="shrink-0 transition-transform group-open:rotate-90" />
            More settings
          </summary>
          <div className="mt-3 space-y-3">{more.map(input)}</div>
        </details>
      )}
    </div>
  );
}
