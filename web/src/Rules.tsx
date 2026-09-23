import { CaretRight, Check, MagnifyingGlass, Question, Robot, X } from "@phosphor-icons/react";
import { useEffect, useMemo, useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Panel, textareaClass } from "./Field";
import type { Rule, RuleSection } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function sectionDecision(s: RuleSection): string {
  if (s.rules.length === 0) return "";
  const d = s.rules[0].decision;
  return s.rules.every((r) => r.decision === d) ? d : "";
}

function RuleDecisionSegment({
  value,
  onChange,
  disabled,
  size = "md",
}: {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  size?: "sm" | "md";
}) {
  const modes = [
    { id: "allow", label: "Allow", icon: Check },
    { id: "auto", label: "Auto", icon: Robot },
    { id: "ask", label: "Ask", icon: Question },
    { id: "deny", label: "Deny", icon: X },
  ];

  return (
    <div className="flex select-none items-center rounded-[6px] border border-thread/80 bg-cloth p-0.5 shadow-[inset_0_1px_2px_rgba(0,0,0,0.03)]">
      {modes.map((m) => {
        const active = value === m.id;
        const Icon = m.icon;

        let activeStyle = "";
        let inactiveHover = "";
        if (m.id === "allow") {
          activeStyle = "bg-pine text-plaster shadow-xs font-medium";
          inactiveHover = "hover:text-pine hover:bg-linen/60";
        } else if (m.id === "auto") {
          activeStyle = "bg-slate text-plaster shadow-xs font-medium";
          inactiveHover = "hover:text-slate hover:bg-linen/60";
        } else if (m.id === "deny") {
          activeStyle = "bg-carmine text-plaster shadow-xs font-medium";
          inactiveHover = "hover:text-carmine hover:bg-carmine/10";
        } else {
          activeStyle = "bg-folio text-iron font-medium border border-thread/80 shadow-xs";
          inactiveHover = "hover:text-iron hover:bg-linen/60";
        }

        return (
          <button
            key={m.id}
            type="button"
            disabled={disabled}
            className={`flex flex-1 items-center justify-center gap-1 rounded-[6px] transition-[transform,background-color,color] duration-150 active:scale-[0.96] ${
              size === "sm" ? "h-7 px-1.5 text-[11px]" : "h-8 px-1.5 text-[12px]"
            } ${
              active ? activeStyle : `text-stone ${inactiveHover}`
            } ${disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onChange(m.id);
            }}
          >
            <Icon size={size === "sm" ? 11 : 12} weight="bold" className={active ? "opacity-90" : "opacity-50"} />
            <span>{m.label}</span>
          </button>
        );
      })}
    </div>
  );
}

export function RulesPane({ botId }: { botId: string }) {
  const [sections, setSections] = useState<RuleSection[]>([]);
  const [open, setOpen] = useState<Record<string, boolean>>({ bot: true });
  const [filter, setFilter] = useState("");
  const [err, setErr] = useState("");
  const [policy, setPolicy] = useState("");
  const [savedPolicy, setSavedPolicy] = useState("");
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [policySaved, setPolicySaved] = useState(false);

  async function load() {
    const [r, b] = await Promise.all([ui.listRules({ botId }), ui.getBot({ id: botId })]);
    setSections(r.sections);
    setPolicy(b.autoApprove);
    setSavedPolicy(b.autoApprove);
  }

  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
  }, [botId]);

  async function savePolicy() {
    setErr("");
    setSavingPolicy(true);
    setPolicySaved(false);
    try {
      const b = await ui.getBot({ id: botId });
      const next = await ui.updateBot({
        id: botId,
        name: b.name,
        description: b.description,
        soul: b.soul,
        memory: b.memory,
        autoApprove: policy,
        model: b.model,
      });
      setPolicy(next.autoApprove);
      setSavedPolicy(next.autoApprove);
      setPolicySaved(true);
    } catch (e) {
      setErr(fail(e));
    } finally {
      setSavingPolicy(false);
    }
  }

  async function setDecision(r: Rule, decision: string) {
    setErr("");
    const prev = sections;
    // Optimistic update
    setSections((curr) =>
      curr.map((s) => ({
        ...s,
        rules: s.rules.map((rule) =>
          rule.connector === r.connector && rule.action === r.action
            ? ({ ...rule, decision } as Rule)
            : rule,
        ),
      })),
    );

    try {
      await ui.setRule({ botId, connector: r.connector, action: r.action, decision });
      await load();
    } catch (e) {
      setSections(prev);
      setErr(fail(e));
    }
  }

  async function setSection(s: RuleSection, decision: string) {
    if (s.rules.every((r) => r.decision === decision)) return;
    setErr("");
    const prev = sections;
    // Optimistic update
    setSections((curr) =>
      curr.map((sec) =>
        sec.id === s.id
          ? ({
              ...sec,
              rules: sec.rules.map((rule) => ({ ...rule, decision } as Rule)),
            } as RuleSection)
          : sec,
      ),
    );

    try {
      await Promise.all(
        s.rules.map((r) => ui.setRule({ botId, connector: r.connector, action: r.action, decision })),
      );
      await load();
    } catch (e) {
      setSections(prev);
      setErr(fail(e));
    }
  }

  const query = filter.trim().toLowerCase();
  const visibleSections = useMemo(() => {
    if (!query) return sections;
    return sections
      .map((s) => {
        const titleMatch = s.title.toLowerCase().includes(query);
        const matchingRules = s.rules.filter(
          (r) =>
            r.title.toLowerCase().includes(query) ||
            r.action.toLowerCase().includes(query) ||
            r.connector.toLowerCase().includes(query),
        );
        if (titleMatch || matchingRules.length > 0) {
          return {
            ...s,
            rules: titleMatch ? s.rules : matchingRules,
          } as RuleSection;
        }
        return null;
      })
      .filter((s): s is RuleSection => s !== null);
  }, [sections, query]);

  const totalRules = useMemo(
    () => sections.reduce((sum, s) => sum + s.rules.length, 0),
    [sections],
  );

  return (
    <div className="silo-page">
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-baseline sm:justify-between">
        <div>
          <h2 className="text-[22px] font-medium text-iron">Rules</h2>
          <p className="mt-0.5 text-stone">Permissions for tools, secrets, and system actions</p>
        </div>
        {totalRules > 5 && (
          <div className="relative w-full sm:w-[220px]">
            <MagnifyingGlass
              size={14}
              className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-stone"
            />
            <input
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter rules..."
              className="h-8 w-full rounded-[6px] border border-thread bg-folio pr-7 pl-8 text-[13px] text-iron placeholder:text-stone/70 focus:border-bindery focus:outline-none"
            />
            {filter && (
              <button
                type="button"
                onClick={() => setFilter("")}
                className="absolute top-1/2 right-2 -translate-y-1/2 text-stone hover:text-iron"
                title="Clear filter"
              >
                <X size={12} weight="bold" />
              </button>
            )}
          </div>
        )}
      </div>

      <div className="mb-5 rounded-[10px] border border-thread-2 bg-cloth/40 p-3.5">
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-4">
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-pine" />
            <div>
              <div className="text-[13px] font-medium text-iron">Allow</div>
              <div className="text-[12px] leading-snug text-stone">Runs immediately without asking</div>
            </div>
          </div>
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-slate" />
            <div>
              <div className="text-[13px] font-medium text-iron">Auto</div>
              <div className="text-[12px] leading-snug text-stone">The approval model decides from your policy</div>
            </div>
          </div>
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-thread" />
            <div>
              <div className="text-[13px] font-medium text-iron">Ask</div>
              <div className="text-[12px] leading-snug text-stone">Pauses run and opens an approval slip</div>
            </div>
          </div>
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-carmine" />
            <div>
              <div className="text-[13px] font-medium text-iron">Deny</div>
              <div className="text-[12px] leading-snug text-stone">Refuses execution automatically</div>
            </div>
          </div>
        </div>
      </div>

      <Panel
        title="Auto-approve policy"
        note="Read by the approval model when a rule is set to Auto. Write the actions this Bot may run without a human. Empty means every Auto rule asks."
        className="mb-5"
      >
        <textarea
          className={`${textareaClass} h-[120px] font-mono text-[13px] leading-5`}
          placeholder="e.g. Auto-approve read-only file reads, web searches, and screenshots. Ask before anything that writes, deletes, sends a message, or touches a secret."
          value={policy}
          onChange={(e) => {
            setPolicy(e.target.value);
            setPolicySaved(false);
          }}
        />
        <div className="mt-3 flex items-center gap-3">
          <Btn kind="primary" onClick={() => void savePolicy()} disabled={savingPolicy || policy === savedPolicy}>
            {savingPolicy ? "Saving…" : "Save policy"}
          </Btn>
          {policySaved && <span className="text-stone">Saved</span>}
        </div>
      </Panel>

      {err && <p className="mb-3 text-carmine">{err}</p>}

      {visibleSections.length === 0 && (
        <div className="rounded-[10px] border border-dashed border-thread bg-folio p-8 text-center text-stone">
          No matching rules found for &ldquo;{filter}&rdquo;
        </div>
      )}

      {visibleSections.map((s) => {
        const isOpen = query ? true : (open[s.id] ?? false);
        return (
          <details
            key={s.id}
            className="group mb-5 rounded-[10px] border border-thread bg-folio shadow-[0_1px_2px_rgba(0,0,0,0.02)]"
            open={isOpen}
            onToggle={(e) => {
              if (query) return;
              const next = e.currentTarget.open;
              setOpen((o) => (o[s.id] === next ? o : { ...o, [s.id]: next }));
            }}
          >
            <summary className="flex cursor-pointer list-none flex-wrap items-start gap-3 p-4 transition-colors hover:bg-cloth/30 [&::-webkit-details-marker]:hidden">
              <CaretRight size={14} className="mt-1.5 shrink-0 text-stone transition-transform group-open:rotate-90" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <h3 className="text-[16px] font-medium text-iron">{s.title}</h3>
                  {s.rules.length > 0 && (
                    <span className="rounded-[6px] bg-cloth px-1.5 py-0.5 font-mono text-[11px] text-stone">
                      {s.rules.length} {s.rules.length === 1 ? "action" : "actions"}
                    </span>
                  )}
                </div>
                <p className="mt-1 text-[13px] text-stone">{s.summary}</p>
              </div>
              {s.rules.length > 0 && (
                <div
                  className="flex w-full items-center justify-between gap-2 pt-2 wide:w-auto wide:justify-end wide:pt-0"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                  }}
                >
                  <span className="text-[11px] font-medium uppercase tracking-wider text-stone wide:hidden">
                    All
                  </span>
                  {sectionDecision(s) === "" && (
                    <span className="hidden text-[11px] font-medium text-stone wide:inline">
                      Mixed
                    </span>
                  )}
                  <div
                    className="w-full wide:w-[260px] wide:shrink-0"
                    title={
                      sectionDecision(s) === ""
                        ? "Mixed. Pick one to apply to every action below."
                        : "Apply to every action below."
                    }
                  >
                    <RuleDecisionSegment
                      size="sm"
                      value={sectionDecision(s)}
                      onChange={(v) => void setSection(s, v)}
                    />
                  </div>
                </div>
              )}
            </summary>
            <div className="px-4 pb-4">
              {s.rules.length === 0 ? (
                <p className="py-2 text-stone">Nothing to set here yet.</p>
              ) : (
                s.rules.map((r) => {
                  const isAllow = r.decision === "allow";
                  const isDeny = r.decision === "deny";

                  return (
                    <div
                      key={`${r.connector}.${r.action}`}
                      className="group/row -mx-2.5 flex flex-col gap-2 rounded-md border-t border-thread-2 px-2.5 py-2.5 transition-colors hover:bg-cloth/40 wide:flex-row wide:items-center wide:gap-4"
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2.5">
                        <span
                          className={`h-2 w-2 shrink-0 rounded-full transition-colors ${
                            isAllow ? "bg-pine" : isDeny ? "bg-carmine" : "bg-thread"
                          }`}
                          title={`Status: ${r.decision || "Ask"}`}
                        />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <span
                              className={
                                r.connector === "secrets"
                                  ? "font-mono text-[13px] font-medium text-iron"
                                  : "text-[14px] font-medium text-iron"
                              }
                            >
                              {r.title || r.action}
                            </span>
                            {r.connector !== "secrets" &&
                              r.title &&
                              r.action &&
                              r.title !== r.action &&
                              r.action !== "*" && (
                                <span className="rounded-[6px] bg-cloth px-1.5 py-0.5 font-mono text-[11px] text-stone">
                                  {r.action}
                                </span>
                              )}
                            {r.connector === "secrets" && (
                              <span className="rounded-[6px] bg-cloth px-1.5 py-0.5 text-[11px] text-stone">
                                Secret
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                      <div className="w-full wide:w-[260px] wide:shrink-0">
                        <RuleDecisionSegment
                          value={r.decision}
                          onChange={(v) => void setDecision(r, v)}
                        />
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </details>
        );
      })}
    </div>
  );
}

