import { RefreshCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ui } from "./api";
import { SkeletonRows } from "./Field";
import { Btn } from "./Btn";
import { Select } from "./Select";
import type { Bot, LLMLog } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

const labelClass: Record<string, string> = {
  chat: "bg-cobalt-pale text-ink",
  title: "bg-well text-ink-3",
  approval: "bg-ink-2/20 text-ink-2",
};

function LabelChip({ label }: { label: string }) {
  return (
    <span className={`rounded-sm px-1.5 py-0.5 text-[11px] font-medium ${labelClass[label] ?? "bg-well text-ink-3"}`}>
      {label || "model"}
    </span>
  );
}

function LogRow({ log }: { log: LLMLog }) {
  return (
    <details className="group rounded-card shadow-card bg-surface">
      <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2 text-[12px] [&::-webkit-details-marker]:hidden">
        <span className="font-mono text-ink-3">{log.at}</span>
        <LabelChip label={log.label} />
        <span className="font-mono text-ink">{log.model}</span>
        <span className="text-ink-3">{log.provider}</span>
        {log.error ? <span className="font-medium text-vermilion">error</span> : null}
        <span className="ml-auto flex items-center gap-2 font-mono text-[11px] text-ink-3">
          <span>in {log.inputTokens}</span>
          <span>out {log.outputTokens}</span>
          <span>{log.durationMs}ms</span>
        </span>
      </summary>
      <div className="space-y-3 border-t border-line-strong bg-well p-3">
        {log.error ? <p className="break-words font-mono text-[12px] text-vermilion">{log.error}</p> : null}
        <div>
          <div className="mb-1 text-[11px] font-medium tracking-wide text-ink-3 uppercase">Request</div>
          <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface p-3 font-mono text-[12px] leading-5 text-ink">
            {log.request || "(empty)"}
          </pre>
        </div>
        <div>
          <div className="mb-1 text-[11px] font-medium tracking-wide text-ink-3 uppercase">Response</div>
          <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface p-3 font-mono text-[12px] leading-5 text-ink">
            {log.response || "(empty)"}
          </pre>
        </div>
      </div>
    </details>
  );
}

export function AdminDebug() {
  const [logs, setLogs] = useState<LLMLog[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [bots, setBots] = useState<Bot[]>([]);
  const [botId, setBotId] = useState("");
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");

  async function load() {
    try {
      const r = await ui.listLLMLogs({ botId });
      setLogs(r.logs);
      setEnabled(r.enabled);
      setErr("");
    } catch (e) {
      setErr(fail(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    ui.listBots({})
      .then((r) => setBots(r.bots))
      .catch(() => {});
  }, []);

  useEffect(() => {
    setLoading(true);
    void load();
    const t = setInterval(() => void load(), 2000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [botId]);

  const botName = useMemo(() => {
    const m = new Map(bots.map((b) => [b.id, b.name]));
    return (id: string) => m.get(id) || id;
  }, [bots]);

  if (!enabled && !loading) {
    return (
      <div>
        <h2 className="mb-2 text-[22px] leading-7 font-medium tracking-[-0.015em]">Debug</h2>
        <p className="text-ink-2">
          Debug logging is off. Set <span className="font-mono text-ink">debug: true</span> in{" "}
          <span className="font-mono text-ink">silo.yaml</span> (or{" "}
          <span className="font-mono text-ink">SILO_DEBUG=true</span>) and reload.
        </p>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Debug</h2>
          <p className="mt-0.5 text-ink-2">Raw model requests and responses from every feature, including auto-approval.</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-[200px]">
            <Select
              value={botId}
              onChange={setBotId}
              options={[{ value: "", label: "All Bots" }, ...bots.map((b) => ({ value: b.id, label: b.name }))]}
            />
          </div>
          <Btn kind="secondary" icon={<RefreshCw size={12} />} onClick={() => void load()}>
            Refresh
          </Btn>
        </div>
      </div>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      {logs.length === 0 && loading ? (
        <SkeletonRows rows={3} height={52} />
      ) : logs.length === 0 ? (
        <div className="rounded-card bg-well p-8 text-center text-[13px] text-ink-3">No model calls captured yet.</div>
      ) : (
        <div className="grid gap-2">
          {logs.map((l) => (
            <div key={l.id}>
              {botId === "" && <div className="mb-1 px-1 text-[11px] font-medium text-ink-3">{botName(l.botId)}</div>}
              <LogRow log={l} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
