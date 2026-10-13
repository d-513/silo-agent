import { useMemo, useState } from "react";
import { btnClass } from "../Btn";
import { SkeletonRows } from "../Field";
import { TabPill, TabPills } from "../TabPills";
import { DecisionLegend } from "./DecisionLegend";
import { countRules, filterSections, sideSections, type Side } from "./model";
import { PolicyPanel } from "./PolicyPanel";
import { RulesFilter } from "./RulesFilter";
import { SectionCard } from "./SectionCard";
import { useRules } from "./useRules";
import { t } from "../i18n.ts";

// The Rules tab. Every action has two rules, shown one side at a time: Calls
// is what the Bot may do, Results is what happens to what comes back before
// the Bot reads it.
export function RulesPane({ botId }: { botId: string }) {
  const rules = useRules(botId);
  const { sections, loaded, err, setDecision, setSection } = rules;
  const [side, setSide] = useState<Side>("decision");
  const [open, setOpen] = useState<Record<string, boolean>>({ bot: true });
  const [filter, setFilter] = useState("");

  const query = filter.trim().toLowerCase();
  const shown = useMemo(() => sideSections(sections, side), [sections, side]);
  const visible = useMemo(() => filterSections(shown, query), [shown, query]);
  const total = useMemo(() => countRules(shown), [shown]);
  const output = side === "output";

  return (
    <div className="silo-page">
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-baseline sm:justify-between">
        <div>
          <h2 className="text-title text-ink">{t("Rules")}</h2>
          <p className="mt-0.5 text-ink-2">
            {output ? t("What happens to a tool's result before the Bot reads it") : t("Permissions for tools, secrets, and system actions")}
          </p>
        </div>
        {total > 5 && <RulesFilter value={filter} onChange={setFilter} />}
      </div>

      <div className="mb-4 flex">
        <TabPills>
          <TabPill on={!output} onClick={() => setSide("decision")}>
            {t("Calls")}
          </TabPill>
          <TabPill on={output} onClick={() => setSide("output")}>
            {t("Results")}
          </TabPill>
        </TabPills>
      </div>

      <DecisionLegend side={side} />

      {output ? (
        <>
          {loaded && !rules.filterEnabled && (
            <p className="mb-5 rounded-card bg-well p-3.5 text-[13px] text-ink-2">
              {t("The operator has the output filter turned off. These rules are kept, but every result passes until it is on again.")}
            </p>
          )}
          <PolicyPanel
            title={t("Output policy")}
            note={t("Read by the filter model for a result set to Check, next to the call that produced it and your last message in that conversation. It says what makes a result suspicious (flagged) or malicious (withheld). Empty means the built-in policy, which looks for attempts to direct the Bot.")}
            placeholder={t("Empty: the built-in policy. Write your own to change what is flagged or withheld.")}
            policy={rules.outputPolicy}
            saved={rules.savedOutputPolicy}
            state={rules.outputSaver.state}
            onChange={rules.setOutputPolicy}
            onSave={() => void rules.savePolicy("output")}
            extra={
              rules.outputPolicy.trim() === "" && rules.defaultOutputPolicy ? (
                <button type="button" className={btnClass("secondary")} onClick={() => rules.setOutputPolicy(rules.defaultOutputPolicy)}>
                  {t("Start from the built-in policy")}
                </button>
              ) : null
            }
          />
        </>
      ) : (
        <PolicyPanel
          title={t("Auto-approve policy")}
          note={t("Read by the auto-approval model when a rule is set to Auto, next to the action the Bot asks for and your last message in that conversation. Write the actions this Bot may run without a human. Empty means every Auto rule asks.")}
          placeholder={t("e.g. Auto-approve read-only file reads, web searches, and screenshots. Ask before anything that writes, deletes, sends a message, or touches a secret.")}
          policy={rules.policy}
          saved={rules.savedPolicy}
          state={rules.saver.state}
          onChange={rules.setPolicy}
          onSave={() => void rules.savePolicy()}
        />
      )}

      {err && <p className="mb-3 text-vermilion">{err}</p>}

      {!loaded && !err ? <SkeletonRows rows={4} height={52} /> : null}

      {loaded && visible.length === 0 && (
        <div className="rounded-card border border-dashed border-line bg-surface p-8 text-center text-ink-3">
          {t("No matching rules found for “{{filter}}”", { filter })}
        </div>
      )}

      {visible.map((s) => (
        <SectionCard
          key={s.id}
          section={s}
          side={side}
          open={query ? true : (open[s.id] ?? false)}
          locked={!!query}
          onToggle={(next) => setOpen((o) => (o[s.id] === next ? o : { ...o, [s.id]: next }))}
          onSection={(d) => void setSection(s, d, side)}
          onRule={(r, d) => void setDecision(r, d, side)}
        />
      ))}
    </div>
  );
}
