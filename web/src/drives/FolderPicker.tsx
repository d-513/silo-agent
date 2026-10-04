import { FolderOpen } from "lucide-react";
import { useCallback, useRef, useState } from "react";
import { Btn } from "../Btn";
import { Field } from "../Field";
import { FolderNav } from "../FolderNav";
import type { BrowseDriveDir, DriveVar } from "../gen/silo/v1/ui_pb";
import { fail } from "../errors";

// FolderPicker browses the remote live through the sidecar. It walks one level
// at a time (a crumb trail plus the folders here) instead of a tree: a remote
// drive can be huge, and every level is a network round trip.
export function FolderPicker({
  v,
  value,
  onChange,
  browse,
  ready,
}: {
  v: DriveVar;
  value: string;
  onChange: (s: string) => void;
  browse: (path: string) => Promise<BrowseDriveDir[]>;
  ready: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [at, setAt] = useState(value);
  const [dirs, setDirs] = useState<BrowseDriveDir[] | null>(null);
  const [err, setErr] = useState("");
  const seq = useRef(0);

  const go = useCallback(
    async (path: string) => {
      const mine = ++seq.current;
      setAt(path);
      setDirs(null);
      setErr("");
      try {
        const got = await browse(path);
        if (mine === seq.current) setDirs(got);
      } catch (e) {
        if (mine === seq.current) {
          setErr(fail(e));
          setDirs([]);
        }
      }
    },
    [browse],
  );

  const rootLabel = v.placeholder || "Whole drive";

  return (
    <Field label={v.label} hint={v.help}>
      <div className="flex items-center gap-2">
        <div className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control bg-well px-3 text-[13px]">
          <FolderOpen size={15} className="shrink-0 text-ink-3" />
          <span className={`truncate font-mono text-[12.5px] ${value ? "text-ink" : "text-ink-3"}`}>{value ? `/${value}` : rootLabel}</span>
        </div>
        {value ? (
          <Btn kind="ghost" type="button" onClick={() => onChange("")}>
            Clear
          </Btn>
        ) : null}
        <Btn
          kind="secondary"
          type="button"
          disabled={!ready}
          aria-expanded={open}
          onClick={() => {
            const next = !open;
            setOpen(next);
            if (next) void go(value);
          }}
        >
          {open ? "Close" : "Browse"}
        </Btn>
      </div>
      {open ? (
        <div className="mt-2 overflow-hidden rounded-control bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]">
          <FolderNav rootLabel={rootLabel} at={at} dirs={dirs} err={err} onGo={(p) => void go(p)} />
          <div className="flex items-center justify-between gap-2 px-3 py-2 shadow-[inset_0_1px_0_var(--color-line)]">
            <span className="min-w-0 truncate font-mono text-[12px] text-ink-3">{at ? `/${at}` : rootLabel}</span>
            <Btn
              kind="primary"
              size="sm"
              type="button"
              onClick={() => {
                onChange(at);
                setOpen(false);
              }}
            >
              {at ? "Mount this folder" : "Mount everything"}
            </Btn>
          </div>
        </div>
      ) : null}
    </Field>
  );
}
