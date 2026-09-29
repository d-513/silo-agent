import { Field, inputClass, textareaClass } from "./Field";
import { Select } from "./Select";
import { ToggleRow } from "./Switch";
import type { ChannelField } from "./gen/silo/v1/ui_pb";

// FieldInput renders one code-declared config field (channel adapters and
// built-in connectors). A set secret shows as a placeholder; blank keeps it.
export function FieldInput({
  field,
  value,
  setValue,
  isSet,
}: {
  field: ChannelField;
  value: string;
  setValue: (v: string) => void;
  isSet?: boolean;
}) {
  if (field.type === "toggle") {
    return (
      <div>
        <ToggleRow
          className="rounded-card shadow-card bg-surface px-3.5 py-3"
          label={field.label}
          hint={field.description || "On or off."}
          on={value === "true"}
          onChange={(v) => setValue(v ? "true" : "false")}
        />
      </div>
    );
  }
  return (
    <Field label={field.label} required={field.required} hint={field.description}>
      {field.type === "select" ? (
        <Select
          value={value}
          onChange={setValue}
          placeholder="—"
          options={field.options.map((o) => ({ value: o.value, label: o.label }))}
        />
      ) : field.type === "textarea" ? (
        <textarea className={`${textareaClass} min-h-[80px]`} value={value} onChange={(e) => setValue(e.target.value)} />
      ) : (
        <input
          className={inputClass}
          type={field.secret ? "password" : field.type === "number" ? "number" : "text"}
          value={value}
          placeholder={field.secret && isSet ? "•••• set — type to replace" : ""}
          onChange={(e) => setValue(e.target.value)}
        />
      )}
    </Field>
  );
}
