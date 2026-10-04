import { Field, inputClass, textareaClass } from "../Field";
import { Select } from "../Select";
import { ToggleRow } from "../Switch";
import type { DriveVar } from "../gen/silo/v1/ui_pb";

export function VarInput({
  v,
  value,
  onChange,
  isSet,
}: {
  v: DriveVar;
  value: string;
  onChange: (s: string) => void;
  isSet?: boolean;
}) {
  if (v.type === "bool") {
    return <ToggleRow className="rounded-control bg-well px-3.5 py-3" label={v.label} hint={v.help} on={value === "true"} onChange={(x) => onChange(x ? "true" : "false")} />;
  }
  const secretHint = v.secret ? (isSet ? "Saved. Type to replace it." : "Stays on the Silo server; the Bot never sees it.") : "";
  return (
    <Field label={v.label} required={v.required} hint={[v.help, secretHint].filter(Boolean).join(" ")}>
      {v.type === "select" ? (
        <Select value={value || v.defaultValue} onChange={onChange} options={v.options.map((o) => ({ value: o.value, label: o.label, hint: o.detail }))} />
      ) : v.type === "textarea" ? (
        <textarea
          className={`${textareaClass} min-h-[96px] font-mono text-[12.5px] leading-5`}
          value={value}
          spellCheck={false}
          placeholder={v.secret && isSet ? "•••••••• saved — paste to replace" : v.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <input
          className={`${inputClass} ${v.secret ? "font-mono" : ""}`}
          type={v.secret ? "password" : v.type === "number" ? "number" : v.type === "url" ? "url" : "text"}
          inputMode={v.type === "url" ? "url" : undefined}
          autoComplete={v.secret ? "new-password" : "off"}
          spellCheck={false}
          value={value}
          placeholder={v.secret && isSet ? "•••••••• saved" : v.placeholder || v.defaultValue}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
    </Field>
  );
}
