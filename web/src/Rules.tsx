import { Bot, Check, ChevronRight, CircleHelp, Search, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { SaveButton, useSave } from "./Feedback";
import { Panel, SkeletonRows, textareaClass } from "./Field";
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
    { id: "auto", label: "Auto", icon: Bot },
    { id: "ask", label: "Ask", icon: CircleHelp },
    { id: "deny", label: "Deny", icon: X },
  ];

  return (
    <div role="radiogroup" className="flex select-none items-center gap-0.5 rounded-control bg-well p-[3px]">
      {modes.map((m) => {
        const active = value === m.id;
        const Icon = m.icon;
        // Selected segment is a surface chip; only the text carries the tone.
        const tone = m.id === "allow" ? "text-emerald" : m.id === "deny" ? "text-vermilion" : m.id === "auto" ? "text-ink-2" : "text-ink";
        return (
          <button
            key={m.id}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            className={`flex flex-1 items-center justify-center gap-1 rounded-sm font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${
              size === "sm" ? "h-7 px-1.5 text-[11.5px]" : "h-8 px-2 text-[12.5px]"
            } ${active ? `bg-surface shadow-card ${tone}` : "text-ink-3 hover:bg-pressed hover:text-ink"} ${
              disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"
            }`}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onChange(m.id);
            }}
          >
            <Icon size={size === "sm" ? 11 : 12} />
            <span>{m.label}</span>
          </button>
        );
      })}
    </div>
  );
}

export function RulesPane({ botId }: { botId: string }) {
  const [sections, setSections] = useState<RuleSection[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [open, setOpen] = useState<Record<string, boolean>>({ bot: true });
  const [filter, setFilter] = useState("");
  const [err, setErr] = useState("");
  const [policy, setPolicy] = useState("");
  const [savedPolicy, setSavedPolicy] = useState("");
  const saver = useSave();

  async function load() {
    const [r, b] = await Promise.all([ui.listRules({ botId }), ui.getBot({ id: botId })]);
    setSections(r.sections);
    setLoaded(true);
    setPolicy(b.autoApprove);
    setSavedPolicy(b.autoApprove);
  }

  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
  }, [botId]);

  async function savePolicy() {
    setErr("");
    try {
      const next = await saver.run(async () => {
      const b = await ui.getBot({ id: botId });
      return ui.updateBot({
        id: botId,
        name: b.name,
        description: b.description,
        soul: b.soul,
        memory: b.memory,
        autoApprove: policy,
        model: b.model,
      });
      });
      setPolicy(next.autoApprove);
      setSavedPolicy(next.autoApprove);
    } catch (e) {
      setErr(fail(e));
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
          <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em] text-ink">Rules</h2>
          <p className="mt-0.5 text-ink-2">Permissions for tools, secrets, and system actions</p>
        </div>
        {totalRules > 5 && (
          <div className="relative w-full sm:w-[220px]">
            <Search
              size={14}
              className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3"
            />
            <input
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter rules..."
              className="h-8 w-full rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface pr-7 pl-8 text-[13px] text-ink placeholder:text-ink-3 focus:outline-none"
            />
            {filter && (
              <button
                type="button"
                onClick={() => setFilter("")}
                className="absolute top-1/2 right-2 -translate-y-1/2 text-ink-2 hover:text-ink"
                title="Clear filter"
              >
                <X size={12} />
              </button>
            )}
          </div>
        )}
      </div>

      <div className="mb-5 rounded-card bg-well p-3.5">
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-4">
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-lamp" />
            <div>
              <div className="text-[13px] font-medium text-ink">Allow</div>
              <div className="text-[12px] leading-snug text-ink-3">Runs immediately without asking</div>
            </div>
          </div>
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-ink-2" />
            <div>
              <div className="text-[13px] font-medium text-ink">Auto</div>
              <div className="text-[12px] leading-snug text-ink-3">The approval model decides from your policy</div>
            </div>
          </div>
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-line" />
            <div>
              <div className="text-[13px] font-medium text-ink">Ask</div>
              <div className="text-[12px] leading-snug text-ink-3">Pauses run and opens an approval slip</div>
            </div>
          </div>
          <div className="flex items-start gap-2.5">
            <span className="mt-1 flex h-2 w-2 shrink-0 rounded-full bg-vermilion" />
            <div>
              <div className="text-[13px] font-medium text-ink">Deny</div>
              <div className="text-[12px] leading-snug text-ink-3">Refuses execution automatically</div>
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
          onChange={(e) => setPolicy(e.target.value)}
        />
        <div className="mt-3 flex items-center gap-3">
          <SaveButton state={saver.state} onClick={() => void savePolicy()} disabled={saver.state === "idle" && policy === savedPolicy}>
            Save policy
          </SaveButton>
        </div>
      </Panel>

      {err && <p className="mb-3 text-vermilion">{err}</p>}

      {!loaded && !err ? <SkeletonRows rows={4} height={52} /> : null}

      {loaded && visibleSections.length === 0 && (
        <div className="rounded-card border border-dashed border-line bg-surface p-8 text-center text-ink-3">
          No matching rules found for &ldquo;{filter}&rdquo;
        </div>
      )}

      {visibleSections.map((s) => {
        const isOpen = query ? true : (open[s.id] ?? false);
        return (
          <details
            key={s.id}
            className="group mb-5 rounded-card shadow-card bg-surface"
            open={isOpen}
            onToggle={(e) => {
              if (query) return;
              const next = e.currentTarget.open;
              setOpen((o) => (o[s.id] === next ? o : { ...o, [s.id]: next }));
            }}
          >
            <summary className="flex cursor-pointer list-none flex-wrap items-start gap-3 p-4 transition-colors hover:bg-well [&::-webkit-details-marker]:hidden">
              <ChevronRight size={14} className="mt-1.5 shrink-0 text-ink-3 transition-transform group-open:rotate-90" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <h3 className="text-[16px] font-medium text-ink">{s.title}</h3>
                  {s.rules.length > 0 && (
                    <span className="rounded-sm bg-well px-1.5 py-0.5 font-mono text-[11px] text-ink-3">
                      {s.rules.length} {s.rules.length === 1 ? "action" : "actions"}
                    </span>
                  )}
                </div>
                <p className="mt-1 text-[13px] text-ink-2">{s.summary}</p>
              </div>
              {s.rules.length > 0 && (
                <div
                  className="flex w-full items-center justify-between gap-2 pt-2 wide:w-auto wide:justify-end wide:pt-0"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                  }}
                >
                  <span className="text-[11px] font-medium uppercase tracking-wider text-ink-3 wide:hidden">
                    All
                  </span>
                  {sectionDecision(s) === "" && (
                    <span className="hidden text-[11px] font-medium text-ink-3 wide:inline">
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
                <p className="py-2 text-ink-2">Nothing to set here yet.</p>
              ) : (
                s.rules.map((r) => {
                  const isAllow = r.decision === "allow";
                  const isDeny = r.decision === "deny";

                  return (
                    <div
                      key={`${r.connector}.${r.action}`}
                      className="group/row -mx-2.5 flex flex-col gap-2 rounded-sm border-t border-line px-2.5 py-2.5 transition-colors hover:bg-well wide:flex-row wide:items-center wide:gap-4"
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2.5">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <span
                              className={
                                r.connector === "secrets"
                                  ? "font-mono text-[13px] font-medium text-ink"
                                  : "text-[14px] font-medium text-ink"
                              }
                            >
                              {r.title || r.action}
                            </span>
                            {r.connector !== "secrets" &&
                              r.title &&
                              r.action &&
                              r.title !== r.action &&
                              r.action !== "*" && (
                                <span className="rounded-sm bg-well px-1.5 py-0.5 font-mono text-[11px] text-ink-3">
                                  {r.action}
                                </span>
                              )}
                            {r.connector === "secrets" && (
                              <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] text-ink-3">
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

