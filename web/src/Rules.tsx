import { CaretRight } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { Segmented } from "./ConnectorForm";
import type { Rule, RuleSection } from "./gen/silo/v1/ui_pb";

const modes = [
  { id: "allow", label: "Allow" },
  { id: "ask", label: "Ask" },
  { id: "deny", label: "Deny" },
];

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function RulesPane({ botId }: { botId: string }) {
  const [sections, setSections] = useState<RuleSection[]>([]);
  const [open, setOpen] = useState<Record<string, boolean>>({ bot: true });
  const [err, setErr] = useState("");

  async function load() {
    const r = await ui.listRules({ botId });
    setSections(r.sections);
  }

  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
  }, [botId]);

  async function setDecision(r: Rule, decision: string) {
    setErr("");
    try {
      await ui.setRule({ botId, connector: r.connector, action: r.action, decision });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }

  return (
    <div className="silo-page">
      <h2 className="mb-2 text-[22px] font-medium">Rules</h2>
      <p className="mb-2 text-stone">
        <span className="font-medium text-iron">Allow</span> runs without asking.{" "}
        <span className="font-medium text-iron">Ask</span> pauses the run and opens a slip (Allow once, Always, or Deny).{" "}
        <span className="font-medium text-iron">Deny</span> refuses. Always on a slip keeps only that action or secret.
      </p>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {sections.map((s) => (
        <details
          key={s.id}
          className="group mb-5 rounded-[10px] border border-thread bg-folio"
          open={open[s.id] ?? false}
          onToggle={(e) => {
            const next = e.currentTarget.open;
            setOpen((o) => (o[s.id] === next ? o : { ...o, [s.id]: next }));
          }}
        >
          <summary className="flex cursor-pointer list-none items-start gap-2 p-4 [&::-webkit-details-marker]:hidden">
            <CaretRight size={14} className="mt-1 shrink-0 text-stone transition-transform group-open:rotate-90" />
            <div className="min-w-0 flex-1">
              <h3 className="text-[16px] font-medium">{s.title}</h3>
              <p className="mt-1 text-stone">{s.summary}</p>
            </div>
          </summary>
          <div className="px-4 pb-4">
            {s.rules.length === 0 ? (
              <p className="text-stone">Nothing to set here yet.</p>
            ) : (
              s.rules.map((r) => (
                <div key={`${r.connector}.${r.action}`} className="flex flex-col gap-2 border-t border-thread-2 py-2 wide:flex-row wide:items-center wide:gap-4">
                  <div className="min-w-0 flex-1">
                    <div className={r.connector === "secrets" ? "font-mono" : "font-medium"}>{r.title || r.action}</div>
                  </div>
                  <div className="w-full wide:w-[220px] wide:shrink-0">
                    <Segmented value={r.decision} onChange={(v) => void setDecision(r, v)} options={modes} />
                  </div>
                </div>
              ))
            )}
          </div>
        </details>
      ))}
    </div>
  );
}
