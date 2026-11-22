import { TriangleAlert } from "lucide-react";
import { type ReactNode } from "react";
import { Field, inputClass, textareaClass } from "../Field";
import { Select } from "../Select";
import { ToggleRow } from "../Switch";
import { ConfigSource, type CaddyDNSProvider, type ConfigField, type Provider, type SearchEngine } from "../gen/silo/v1/ui_pb";
import { hintOf, labelOf, placeholderOf, sourceWord } from "./fields";
import { msg, t } from "../i18n.ts";

// caddy.mode's choices.
const caddyModes = [
  { value: "auto", label: msg("Automatic") },
  { value: "acme", label: msg("Let's Encrypt") },
  { value: "internal", label: msg("Silo's own certificate authority") },
  { value: "off", label: msg("Off") },
] as const;

export function SourceChips({ field }: { field: ConfigField }) {
  return (
    <>
      <span className="font-mono text-micro text-ink-3">{sourceWord(field.source)}</span>
      {field.restartRequired ? <span className="text-micro text-ink-3">{t("restart")}</span> : null}
      {field.source === ConfigSource.ENV ? (
        <span className="inline-flex items-center gap-1 text-micro text-vermilion" title={field.envName}>
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
  dns = [],
  placeholder,
  hint,
  onChange,
}: {
  field: ConfigField;
  value: string;
  engines: SearchEngine[];
  providers: Provider[];
  dns?: CaddyDNSProvider[];
  // placeholder overrides the static one, for a default only the server knows.
  placeholder?: string;
  // hint overrides the field's own, for one that follows the value.
  hint?: string;
  onChange: (v: string) => void;
}) {
  const locked = field.source === ConfigSource.ENV;
  const label = labelOf(field.key, engines, providers, dns);
  const note = hint ?? hintOf(field.key, engines, providers, dns);

  if (field.type === "bool") {
    return (
      <ToggleRow
        label={label}
        hint={note}
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
        emptyLabel={t("No engines")}
        options={engines.map((e) => ({ value: e.id, label: e.name }))}
      />
    );
  } else if (field.key === "caddy.mode") {
    control = <Select value={value || "auto"} onChange={onChange} disabled={locked} options={caddyModes.map((m) => ({ value: m.value, label: t(m.label) }))} />;
  } else if (field.key === "caddy.dns_provider") {
    control = (
      <Select
        value={value}
        onChange={onChange}
        disabled={locked}
        options={[{ value: "", label: t("None: Let's Encrypt reaches this machine on port 80") }, ...dns.map((d) => ({ value: d.id, label: d.name }))]}
      />
    );
  } else if (field.type === "select") {
    const m = /^providers\.(.+)\.cache_ttl$/.exec(field.key);
    const ttls = (m && providers.find((p) => p.id === m[1])?.cacheTtls) || [];
    control = <Select value={value} onChange={onChange} disabled={locked} options={ttls.map((t) => ({ value: t, label: t }))} />;
  } else if (field.type === "text") {
    control = (
      <textarea
        className={`${textareaClass} h-[140px]`}
        autoComplete="off"
        placeholder={placeholder ?? placeholderOf(field.key)}
        value={value}
        disabled={locked}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  } else {
    control = (
      <input
        className={inputClass}
        type={field.secret ? "password" : "text"}
        autoComplete="off"
        placeholder={placeholder ?? placeholderOf(field.key)}
        value={value}
        disabled={locked}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <Field label={label} hint={note} headerRight={<SourceChips field={field} />}>
      {control}
    </Field>
  );
}
