import type { ReactNode } from "react";
import { type ConfigField } from "../gen/silo/v1/ui_pb";
import { FieldRow } from "./FieldRow";
import { Group } from "./kit";
import { useSettingsForm } from "./useAdminSettings";

// One group of the settings form: a row per field, saved by the page's Save
// bar. `children` are more rows of the same card, under the fields.
export function FieldGroup({
  title,
  note,
  rows,
  placeholders,
  hints,
  action,
  children,
}: {
  title: string;
  note?: string;
  rows: ConfigField[];
  // placeholders by field key, for defaults only the server knows.
  placeholders?: Record<string, string>;
  // hints by field key, for one that follows the value.
  hints?: Record<string, string | undefined>;
  action?: ReactNode;
  children?: ReactNode;
}) {
  const { values, engines, providers, dnsProviders, setValue } = useSettingsForm();
  if (rows.length === 0) return null;
  return (
    <Group title={title} note={note} action={action}>
      {rows.map((f) => (
        <FieldRow
          key={f.key}
          field={f}
          value={values[f.key] ?? ""}
          engines={engines}
          providers={providers}
          dns={dnsProviders}
          placeholder={placeholders?.[f.key]}
          hint={hints?.[f.key]}
          onChange={(v) => setValue(f.key, v)}
        />
      ))}
      {children}
    </Group>
  );
}
