import { Check, PackagePlus, Upload } from "lucide-react";
import { useRef, useState, type DragEvent, type FormEvent } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { ErrorWell, inputClass } from "../Field";

const zipLimit = 10 << 20;

// Where a skill comes from: a GitHub URL or owner/repo, or a zip dropped on
// the panel or picked from disk. A well band, not a card, so it reads as a
// tool above the list rather than another skill.
export function InstallPanel({ scope, onDone }: { scope: "library" | "personal"; onDone: () => void }) {
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [note, setNote] = useState("");
  const [over, setOver] = useState(false);
  const file = useRef<HTMLInputElement>(null);

  function report(r: { installed: string[]; skipped: string[] }) {
    const parts = [];
    if (r.installed.length) parts.push("Installed " + r.installed.join(", "));
    if (r.skipped.length) parts.push("already had " + r.skipped.join(", "));
    setNote(parts.join("; ").replace(/^./, (c) => c.toUpperCase()) || "Nothing new");
    onDone();
  }
  async function go(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setNote("");
    setBusy(true);
    try {
      report(await ui.installSkill({ scope, url: url.trim() }));
      setUrl("");
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  async function upload(list: FileList | null) {
    const f = list?.[0];
    if (!f) return;
    setErr("");
    setNote("");
    if (f.size > zipLimit) {
      setErr("The zip is larger than 10 MB.");
      return;
    }
    setBusy(true);
    try {
      report(await ui.installSkill({ scope, archive: new Uint8Array(await f.arrayBuffer()), filename: f.name }));
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  function drop(e: DragEvent) {
    e.preventDefault();
    setOver(false);
    if (!busy) void upload(e.dataTransfer.files);
  }

  return (
    <section
      className={`mb-6 rounded-card bg-well p-4 transition-shadow duration-[160ms] ease-quiet sm:p-5 ${over ? "shadow-[inset_0_0_0_1.5px_var(--color-cobalt)]" : ""}`}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          setOver(true);
        }
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false);
      }}
      onDrop={drop}
    >
      <div className="flex items-start gap-3.5">
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-surface text-ink-2 shadow-card">
          <PackagePlus size={19} />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-card-title leading-5 text-ink">{scope === "library" ? "Add to the library" : "Install a skill"}</h3>
          <p className="mt-0.5 text-[12.5px] leading-[18px] text-ink-2">
            {over
              ? "Drop the zip to install it."
              : scope === "library"
                ? "Paste a GitHub URL or owner/repo, or drop a skill zip. Every Bot can then enable it."
                : "Paste a GitHub URL or owner/repo, or drop a skill zip here. It stays yours until you enable it on a Bot."}
          </p>
          <form className="mt-3 flex flex-wrap items-center gap-2" onSubmit={(e) => void go(e)}>
            <input
              className={`${inputClass} min-w-0 flex-1 wide:min-w-[240px]`}
              placeholder="GitHub URL or owner/repo"
              aria-label="GitHub URL or owner/repo"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            <Btn kind="primary" type="submit" disabled={busy || !url.trim()}>
              {busy ? "Installing…" : "Install"}
            </Btn>
            <Btn kind="secondary" type="button" disabled={busy} icon={<Upload size={12} />} onClick={() => file.current?.click()}>
              Upload zip
            </Btn>
            <input
              ref={file}
              type="file"
              accept=".zip,.tgz,.tar.gz,application/zip"
              className="hidden"
              onChange={(e) => {
                void upload(e.target.files);
                e.target.value = "";
              }}
            />
          </form>
          {err ? <ErrorWell className="mt-3">{err}</ErrorWell> : null}
          {note ? (
            <p role="status" className="mt-3 flex items-center gap-1.5 text-[13px] font-medium text-emerald">
              <Check size={14} className="shrink-0" />
              {note}
            </p>
          ) : null}
        </div>
      </div>
    </section>
  );
}
