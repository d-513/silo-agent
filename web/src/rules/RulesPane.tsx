import { useMemo, useState } from "react";
import { SkeletonRows } from "../Field";
import { DecisionLegend } from "./DecisionLegend";
import { countRules, filterSections } from "./model";
import { PolicyPanel } from "./PolicyPanel";
import { RulesFilter } from "./RulesFilter";
import { SectionCard } from "./SectionCard";
import { useRules } from "./useRules";

// The Rules tab: what each Bot, secret and connector action is allowed to do.
export function RulesPane({ botId }: { botId: string }) {
  const { sections, loaded, err, policy, setPolicy, savedPolicy, saver, savePolicy, setDecision, setSection } = useRules(botId);
  const [open, setOpen] = useState<Record<string, boolean>>({ bot: true });
  const [filter, setFilter] = useState("");

  const query = filter.trim().toLowerCase();
  const visible = useMemo(() => filterSections(sections, query), [sections, query]);
  const total = useMemo(() => countRules(sections), [sections]);

  return (
    <div className="silo-page">
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-baseline sm:justify-between">
        <div>
          <h2 className="text-title text-ink">Rules</h2>
          <p className="mt-0.5 text-ink-2">Permissions for tools, secrets, and system actions</p>
        </div>
        {total > 5 && <RulesFilter value={filter} onChange={setFilter} />}
      </div>

      <DecisionLegend />

      <PolicyPanel policy={policy} saved={savedPolicy} state={saver.state} onChange={setPolicy} onSave={() => void savePolicy()} />

      {err && <p className="mb-3 text-vermilion">{err}</p>}

      {!loaded && !err ? <SkeletonRows rows={4} height={52} /> : null}

      {loaded && visible.length === 0 && (
        <div className="rounded-card border border-dashed border-line bg-surface p-8 text-center text-ink-3">
          No matching rules found for &ldquo;{filter}&rdquo;
        </div>
      )}

      {visible.map((s) => (
        <SectionCard
          key={s.id}
          section={s}
          open={query ? true : (open[s.id] ?? false)}
          locked={!!query}
          onToggle={(next) => setOpen((o) => (o[s.id] === next ? o : { ...o, [s.id]: next }))}
          onSection={(d) => void setSection(s, d)}
          onRule={(r, d) => void setDecision(r, d)}
        />
      ))}
    </div>
  );
}
