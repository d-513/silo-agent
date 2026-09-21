import { ArrowsClockwise } from "@phosphor-icons/react";
import { useEffect, useMemo, useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Select } from "./Select";
import type { Bot, LLMLog } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

const labelClass: Record<string, string> = {
  chat: "bg-bindery-pale text-iron",
  title: "bg-cloth text-stone",
  approval: "bg-slate/20 text-slate",
};

function LabelChip({ label }: { label: string }) {
  return (
    <span className={`rounded-[6px] px-1.5 py-0.5 text-[11px] font-medium ${labelClass[label] ?? "bg-cloth text-stone"}`}>
      {label || "model"}
    </span>
  );
}

function LogRow({ log }: { log: LLMLog }) {
  return (
    <details className="group rounded-[10px] border border-thread bg-folio">
      <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2 text-[12px] [&::-webkit-details-marker]:hidden">
        <span className="font-mono text-stone">{log.at}</span>
        <LabelChip label={log.label} />
        <span className="font-mono text-iron">{log.model}</span>
        <span className="text-stone">{log.provider}</span>
        {log.error ? <span className="font-medium text-carmine">error</span> : null}
        <span className="ml-auto flex items-center gap-2 font-mono text-[11px] text-stone">
          <span>in {log.inputTokens}</span>
          <span>out {log.outputTokens}</span>
          <span>{log.durationMs}ms</span>
        </span>
      </summary>
      <div className="space-y-3 border-t border-thread-2 bg-cloth/30 p-3">
        {log.error ? <p className="break-words font-mono text-[12px] text-carmine">{log.error}</p> : null}
        <div>
          <div className="mb-1 text-[11px] font-medium tracking-wide text-stone uppercase">Request</div>
          <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-[6px] border border-thread-2 bg-folio p-3 font-mono text-[12px] leading-5 text-iron">
            {log.request || "(empty)"}
          </pre>
        </div>
        <div>
          <div className="mb-1 text-[11px] font-medium tracking-wide text-stone uppercase">Response</div>
          <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-[6px] border border-thread-2 bg-folio p-3 font-mono text-[12px] leading-5 text-iron">
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
        <h2 className="mb-2 text-[22px] font-medium">Debug</h2>
        <p className="text-stone">
          Debug logging is off. Set <span className="font-mono text-iron">debug: true</span> in{" "}
          <span className="font-mono text-iron">silo.yaml</span> (or{" "}
          <span className="font-mono text-iron">SILO_DEBUG=true</span>) and reload.
        </p>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-[22px] font-medium">Debug</h2>
          <p className="mt-0.5 text-stone">Raw model requests and responses from every feature, including auto-approval.</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-[200px]">
            <Select
              value={botId}
              onChange={setBotId}
              options={[{ value: "", label: "All Bots" }, ...bots.map((b) => ({ value: b.id, label: b.name }))]}
            />
          </div>
          <Btn kind="secondary" icon={<ArrowsClockwise size={12} />} onClick={() => void load()}>
            Refresh
          </Btn>
        </div>
      </div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {logs.length === 0 ? (
        <div className="rounded-[10px] border border-dashed border-thread bg-folio p-8 text-center text-stone">
          {loading ? "Loading…" : "No model calls captured yet."}
        </div>
      ) : (
        <div className="grid gap-2">
          {logs.map((l) => (
            <div key={l.id}>
              {botId === "" && <div className="mb-1 px-1 text-[11px] font-medium text-stone">{botName(l.botId)}</div>}
              <LogRow log={l} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
