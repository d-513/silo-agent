import { TriangleAlert } from "lucide-react";
import { type ReactNode } from "react";
import { Field, inputClass } from "../Field";
import { Select } from "../Select";
import { ToggleRow } from "../Switch";
import { ConfigSource, type ConfigField, type Provider, type SearchEngine } from "../gen/silo/v1/ui_pb";
import { HINTS, PLACEHOLDERS, labelOf, sourceWord } from "./fields";

export function SourceChips({ field }: { field: ConfigField }) {
  return (
    <>
      <span className="font-mono text-[11px] text-ink-3">{sourceWord(field.source)}</span>
      {field.restartRequired ? <span className="text-[11px] text-ink-3">restart</span> : null}
      {field.source === ConfigSource.ENV ? (
        <span className="inline-flex items-center gap-1 text-[11px] text-vermilion" title={field.envName}>
          <TriangleAlert size={14} />
          {field.envName}
        </span>
      ) : null}
    </>
  );
}

export function FieldRow({
  field,
  value,
  engines,
  providers,
  placeholder,
  onChange,
}: {
  field: ConfigField;
  value: string;
  engines: SearchEngine[];
  providers: Provider[];
  // placeholder overrides the static one, for a default only the server knows.
  placeholder?: string;
  onChange: (v: string) => void;
}) {
  const locked = field.source === ConfigSource.ENV;
  const label = labelOf(field.key, engines, providers);

  if (field.type === "bool") {
    return (
      <ToggleRow
        label={label}
        meta={<SourceChips field={field} />}
        on={value === "true"}
        disabled={locked}
        onChange={(v) => onChange(v ? "true" : "false")}
      />
    );
  }

  let control: ReactNode;
  if (field.key === "search.engine") {
    control = (
      <Select
        value={value}
        onChange={onChange}
        disabled={locked}
        emptyLabel="No engines"
        options={engines.map((e) => ({ value: e.id, label: e.name }))}
      />
    );
  } else if (field.type === "select") {
    const m = /^providers\.(.+)\.cache_ttl$/.exec(field.key);
    const ttls = (m && providers.find((p) => p.id === m[1])?.cacheTtls) || [];
    control = <Select value={value} onChange={onChange} disabled={locked} options={ttls.map((t) => ({ value: t, label: t }))} />;
  } else {
    control = (
      <input
        className={inputClass}
        type={field.secret ? "password" : "text"}
        autoComplete="off"
        placeholder={placeholder ?? PLACEHOLDERS[field.key]}
        value={value}
        disabled={locked}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <Field label={label} hint={HINTS[field.key]} headerRight={<SourceChips field={field} />}>
      {control}
    </Field>
  );
}
