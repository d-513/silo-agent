import { Panel } from "../Field";
import { type ConfigField, type Provider, type SearchEngine } from "../gen/silo/v1/ui_pb";
import { FieldRow } from "./FieldRow";

export function FieldGroup({
  title,
  note,
  rows,
  values,
  engines,
  providers,
  onChange,
}: {
  title: string;
  note?: string;
  rows: ConfigField[];
  values: Record<string, string>;
  engines: SearchEngine[];
  providers: Provider[];
  onChange: (key: string, v: string) => void;
}) {
  if (rows.length === 0) return null;
  return (
    <Panel title={title} note={note} padded={false}>
      <div className="divide-y divide-line-strong">
        {rows.map((f) => (
          <div key={f.key} className="px-4 py-3">
            <FieldRow
              field={f}
              value={values[f.key] ?? ""}
              engines={engines}
              providers={providers}
              onChange={(v) => onChange(f.key, v)}
            />
          </div>
        ))}
      </div>
    </Panel>
  );
}
