import { patchAt, setAt, type ConnectorDraft } from "./draft";

type Edit = <K extends keyof ConnectorDraft>(k: K, v: ConnectorDraft[K]) => void;

// A monospace box of the given width classes.
const mono = (width: string) => `h-9 ${width} rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-2 font-mono`;

// The STDIO command's arguments, one field each.
export function ArgsEditor({ value, set }: { value: ConnectorDraft; set: Edit }) {
  return (
    <>
      <p className="mb-2 text-ink-2">Arguments, one per field. The command runs directly, without a shell.</p>
      {value.stdioArgs.map((arg, i) => (
        <input
          key={i}
          className="mb-2 h-9 w-full rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3 font-mono"
          value={arg}
          onChange={(e) => set("stdioArgs", setAt(value.stdioArgs, i, e.target.value))}
          placeholder={i === 0 ? "-y" : ""}
        />
      ))}
      <button type="button" className="mb-3 text-cobalt" onClick={() => set("stdioArgs", [...value.stdioArgs, ""])}>
        Add argument
      </button>
    </>
  );
}

// The STDIO sidecar's environment: a name, a value, and optionally the Bot
// secret to read the value from.
export function EnvEditor({ value, existing, set }: { value: ConnectorDraft; existing?: boolean; set: Edit }) {
  return (
    <>
      <div className="mb-1 text-[12px] font-medium text-ink-3">Environment</div>
      <p className="mb-2 text-ink-2">Values stay on the Control Plane. A secret name reads that Bot secret when the sidecar starts. Docker inspect can still see injected env.</p>
      {value.env.map((e, i) => (
        <div key={i} className="mb-2 flex flex-wrap gap-2">
          <input
            className={mono("w-36")}
            placeholder="NAME"
            value={e.name}
            onChange={(ev) => set("env", patchAt(value.env, i, { name: ev.target.value }))}
          />
          <input
            className={mono("min-w-0 flex-1")}
            placeholder={existing ? "unchanged" : "Value"}
            type="password"
            value={e.value}
            onChange={(ev) => set("env", patchAt(value.env, i, { value: ev.target.value }))}
          />
          <input
            className={mono("w-36")}
            placeholder="secret name"
            value={e.secret}
            onChange={(ev) => set("env", patchAt(value.env, i, { secret: ev.target.value }))}
          />
        </div>
      ))}
      <button type="button" className="mb-6 text-cobalt" onClick={() => set("env", [...value.env, { name: "", value: "", secret: "" }])}>
        Add env
      </button>
    </>
  );
}

// Extra HTTP headers; the values are write-only.
export function HeadersEditor({ value, existing, set }: { value: ConnectorDraft; existing?: boolean; set: Edit }) {
  return (
    <>
      <div className="mb-1 text-[12px] font-medium text-ink-3">Extra headers</div>
      <p className="mb-2 text-ink-2">Values are stored on the Control Plane and never shown again.</p>
      {value.headers.map((h, i) => (
        <div key={i} className="mb-2 flex gap-2">
          <input
            className={mono("w-40")}
            placeholder="Name"
            value={h.name}
            onChange={(e) => set("headers", patchAt(value.headers, i, { name: e.target.value }))}
          />
          <input
            className={mono("min-w-0 flex-1")}
            placeholder={existing ? "unchanged" : "Value"}
            type="password"
            value={h.value}
            onChange={(e) => set("headers", patchAt(value.headers, i, { value: e.target.value }))}
          />
        </div>
      ))}
      <button type="button" className="mb-6 text-cobalt" onClick={() => set("headers", [...value.headers, { name: "", value: "" }])}>
        Add header
      </button>
    </>
  );
}
