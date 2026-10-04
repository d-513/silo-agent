import { useEffect, useState } from "react";
import { Field } from "../Field";
import { Select } from "../Select";
import type { DriveOption, DriveVar } from "../gen/silo/v1/ui_pb";
import { fail } from "../errors";

export function PickInput({
  v,
  value,
  onPick,
  load,
  ready,
}: {
  v: DriveVar;
  value: string;
  onPick: (o: DriveOption | null) => void;
  load: () => Promise<DriveOption[]>;
  ready: boolean;
}) {
  const [opts, setOpts] = useState<DriveOption[] | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!ready) return;
    let dead = false;
    setErr("");
    load()
      .then((o) => {
        if (!dead) setOpts(o);
      })
      .catch((e) => {
        if (!dead) {
          setErr(fail(e));
          setOpts([]);
        }
      });
    return () => {
      dead = true;
    };
  }, [ready, load]);
  const empty = v.required ? [] : [{ value: "", label: v.placeholder || "Default" }];
  const options = [...empty, ...(opts ?? []).map((o) => ({ value: o.value, label: o.label, hint: o.detail }))];
  if (value && !options.some((o) => o.value === value)) options.push({ value, label: value });
  return (
    <Field label={v.label} required={v.required} hint={err ? undefined : v.help} error={err || undefined}>
      <Select
        value={value}
        disabled={!ready || opts === null}
        placeholder={opts === null && ready ? "Loading…" : v.placeholder || "Choose…"}
        onChange={(val) => onPick((opts ?? []).find((o) => o.value === val) ?? (val ? null : null))}
        options={options}
      />
    </Field>
  );
}

// DriveForm is add and edit in one: Connect → Choose what to mount → Name &
// access. Adding keeps a server-side draft from the first step that needs the
// server (sign-in, test, browse), so those work before the drive exists.
