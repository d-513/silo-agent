import { TriangleAlert } from "lucide-react";
import { type ReactNode } from "react";
import { SettingRow, inputClass, textareaClass } from "../Field";
import { Select } from "../Select";
import { Switch } from "../Switch";
import { Tip } from "../Tip";
import { ConfigSource, type CaddyDNSProvider, type ConfigField, type Provider, type SearchEngine } from "../gen/silo/v1/ui_pb";
import { hintOf, labelOf, placeholderOf } from "./fields";
import { msg, t } from "../i18n.ts";

// caddy.mode's choices.
const caddyModes = [
  { value: "auto", label: msg("Automatic") },
  { value: "acme", label: msg("Let's Encrypt") },
  { value: "internal", label: msg("Silo's own certificate authority") },
  { value: "off", label: msg("Off") },
] as const;

// SourceChips says what a reader has to know about where a setting comes
// from, and nothing else: that it only applies after a restart, and that the
// environment sets it, so it cannot be changed here.
export function SourceChips({ field }: { field: ConfigField }) {
  return (
    <>
      {field.restartRequired ? (
        <Tip content={t("Takes effect when Silo next starts.")}>
          <span className="rounded-xs bg-well px-1.5 py-0.5 text-micro leading-4 font-normal text-ink-3">{t("restart")}</span>
        </Tip>
      ) : null}
      {field.source === ConfigSource.ENV ? (
        <Tip content={t("Set by the environment, which wins over silo.yaml. Change it there.")}>
          <span className="inline-flex items-center gap-1 font-mono text-micro leading-4 font-normal text-vermilion">
            <TriangleAlert size={12} />
            {field.envName}
          </span>
        </Tip>
      ) : null}
    </>
  );
}

// FieldRow is one operator setting as a row: what it is and its sentence on
// the left, its control on the right. A long text (a policy) takes the row's
// whole width under its label.
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
  const name = labelOf(field.key, engines, providers, dns);
  const note = hint ?? hintOf(field.key, engines, providers, dns);
  const label = (
    <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
      {name}
      <SourceChips field={field} />
    </span>
  );

  if (field.type === "bool") {
    return (
      <SettingRow label={label} hint={note}>
        <Switch on={value === "true"} disabled={locked} title={name} onChange={(v) => onChange(v ? "true" : "false")} />
      </SettingRow>
    );
  }

  let control: ReactNode;
  if (field.key === "search.engine") {
    control = <Select value={value} onChange={onChange} disabled={locked} ariaLabel={name} emptyLabel={t("No engines")} options={engines.map((e) => ({ value: e.id, label: e.name }))} />;
  } else if (field.key === "caddy.mode") {
    control = <Select value={value || "auto"} onChange={onChange} disabled={locked} ariaLabel={name} options={caddyModes.map((m) => ({ value: m.value, label: t(m.label) }))} />;
  } else if (field.key === "caddy.dns_provider") {
    control = (
      <Select
        value={value}
        onChange={onChange}
        disabled={locked}
        ariaLabel={name}
        options={[{ value: "", label: t("None: Let's Encrypt reaches this machine on port 80") }, ...dns.map((d) => ({ value: d.id, label: d.name }))]}
      />
    );
  } else if (field.type === "select") {
    const m = /^providers\.(.+)\.cache_ttl$/.exec(field.key);
    const ttls = (m && providers.find((p) => p.id === m[1])?.cacheTtls) || [];
    control = <Select value={value} onChange={onChange} disabled={locked} ariaLabel={name} options={ttls.map((t) => ({ value: t, label: t }))} />;
  } else if (field.type === "text") {
    return (
      <SettingRow label={label} hint={note} stack>
        <textarea
          className={`${textareaClass} h-[140px]`}
          autoComplete="off"
          aria-label={name}
          placeholder={placeholder ?? placeholderOf(field.key)}
          value={value}
          disabled={locked}
          onChange={(e) => onChange(e.target.value)}
        />
      </SettingRow>
    );
  } else {
    control = (
      <input
        className={inputClass}
        type={field.secret ? "password" : "text"}
        autoComplete="off"
        aria-label={name}
        placeholder={placeholder ?? placeholderOf(field.key)}
        value={value}
        disabled={locked}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <SettingRow label={label} hint={note}>
      {control}
    </SettingRow>
  );
}
