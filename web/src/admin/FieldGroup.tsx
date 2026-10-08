import type { ReactNode } from "react";
import { Panel } from "../Field";
import { type ConfigField } from "../gen/silo/v1/ui_pb";
import { FieldRow } from "./FieldRow";
import { useSettingsForm } from "./useAdminSettings";

// One panel of the settings form: a row per field, saved by the page's Save
// bar. `children` sit under the rows, inside the same panel.
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
  const { values, engines, providers, setValue } = useSettingsForm();
  if (rows.length === 0) return null;
  return (
    <Panel title={title} note={note} action={action} padded={false}>
      <div className="divide-y divide-line-strong">
        {rows.map((f) => (
          <div key={f.key} className="px-5 py-3.5">
            <FieldRow
              field={f}
              value={values[f.key] ?? ""}
              engines={engines}
              providers={providers}
              placeholder={placeholders?.[f.key]}
              hint={hints?.[f.key]}
              onChange={(v) => setValue(f.key, v)}
            />
          </div>
        ))}
      </div>
      {children}
    </Panel>
  );
}
