import { useQuery } from "@connectrpc/connect-query";
import { useParams } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { UI, type Rule, type RuleSection } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { patch, reload } from "../query";
import { countRules, filterSections, sideSections, withDecision, withSectionDecision, type Side } from "../rules/model";
import { PolicyPanel } from "../rules/PolicyPanel";
import { RulesFilter } from "../rules/RulesFilter";
import { SectionCard } from "../rules/SectionCard";
import { TabPill, TabPills } from "../TabPills";

const none: RuleSection[] = [];

// /w/$spaceId/rules: the Workspace's guardrails. The same sections and
// switches as a Bot's Rules page, but what is set here is the least strict a
// Bot of the Workspace may go: Ask here means no Bot may Allow it. Allow (and
// Pass) is no limit, so every Bot decides for itself.
export function SpaceRules() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/rules" });
  const q = useQuery(UI.method.listSpaceRules, { spaceId });
  const sections = q.data?.sections ?? none;
  const [side, setSide] = useState<Side>("decision");
  const [open, setOpen] = useState<Record<string, boolean>>({ bot: true });
  const [filter, setFilter] = useState("");
  const [err, setErr] = useState("");
  const query = filter.trim().toLowerCase();
  const shown = useMemo(() => sideSections(sections, side), [sections, side]);
  const visible = useMemo(() => filterSections(shown, query), [shown, query]);
  const output = side === "output";
  const key = { spaceId };

  // One action's floors, both sides as they now stand; shown at once, put
  // back when the server refuses.
  async function write(rows: Rule[], decision: string) {
    setErr("");
    const before = q.data;
    patch(UI.method.listSpaceRules, key, (r) => ({
      ...r,
      sections: rows.reduce((acc, row) => withDecision(acc, row.connector, row.action, decision, side), r.sections),
    }));
    try {
      await Promise.all(
        rows.map((r) => ui.setSpaceRule({ spaceId, connector: r.connector, action: r.action, decision: output ? r.decision : decision, output: output ? decision : r.output })),
      );
    } catch (e) {
      if (before) patch(UI.method.listSpaceRules, key, () => before);
      setErr(fail(e));
    }
    void reload(UI.method.listSpaceRules, key);
  }

  return (
    <div className="silo-page silo-page-wide rise">
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-baseline sm:justify-between">
        <div>
          <h2 className="text-title text-ink">{t("Rules")}</h2>
          <p className="mt-1 max-w-[620px] text-[13.5px] leading-[21px] text-ink-2">
            {output
              ? t("The least each Bot in this workspace must do with a result before reading it. Pass leaves it to each Bot.")
              : t("The least strict each Bot in this workspace may be. Set Ask on an action and no Bot can Allow it; Allow leaves it to each Bot.")}
          </p>
        </div>
        {countRules(shown) > 5 && <RulesFilter value={filter} onChange={setFilter} />}
      </div>

      <div className="mb-5 flex">
        <TabPills>
          <TabPill on={!output} onClick={() => setSide("decision")}>
            {t("Calls")}
          </TabPill>
          <TabPill on={output} onClick={() => setSide("output")}>
            {t("Results")}
          </TabPill>
        </TabPills>
      </div>

      {q.data ? <Policy key={`${spaceId}:${side}`} spaceId={spaceId} side={side} approval={q.data.approvalPolicy} output={q.data.outputPolicy} /> : null}

      {err || q.error ? <ErrorWell className="mb-4">{err || fail(q.error)}</ErrorWell> : null}
      {!q.data && !q.error ? <SkeletonRows rows={4} height={52} /> : null}

      {visible.map((s) => (
        <SectionCard
          key={s.id}
          section={s}
          side={side}
          open={query ? true : (open[s.id] ?? false)}
          locked={!!query}
          onToggle={(next) => setOpen((o) => (o[s.id] === next ? o : { ...o, [s.id]: next }))}
          onSection={(d) => {
            patch(UI.method.listSpaceRules, key, (r) => ({ ...r, sections: withSectionDecision(r.sections, s.id, d, side) }));
            void write(s.rules, d);
          }}
          onRule={(r, d) => void write([r], d)}
        />
      ))}
    </div>
  );
}

// The Workspace's policy text for the side shown: read by auto-approval (Calls)
// or the output filter (Results) after the operator's and before each Bot's.
function Policy({ spaceId, side, approval, output }: { spaceId: string; side: Side; approval: string; output: string }) {
  const results = side === "output";
  const saved = results ? output : approval;
  const [draft, setDraft] = useState<string | null>(null);
  const saver = useSave();
  const [err, setErr] = useState("");
  async function save() {
    setErr("");
    try {
      await saver.run(() =>
        ui.updateSpacePolicy({ spaceId, approvalPolicy: results ? approval : (draft ?? saved), outputPolicy: results ? (draft ?? saved) : output }),
      );
      await reload(UI.method.listSpaceRules, { spaceId });
      setDraft(null);
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <>
      <PolicyPanel
        title={results ? t("Output policy for every Bot") : t("Auto-approve policy for every Bot")}
        note={
          results
            ? t("Read by the output filter for every Bot in this workspace, before the Bot's own policy. Say what no Bot here should be fed.")
            : t("Read by auto-approval for every Bot in this workspace, before the Bot's own policy. Say what no Bot here may do without a person.")
        }
        placeholder={results ? t("e.g. Withhold anything that asks the Bot to send company data elsewhere.") : t("e.g. Never auto-approve sending mail outside the company.")}
        policy={draft ?? saved}
        saved={saved}
        state={saver.state}
        onChange={setDraft}
        onSave={() => void save()}
      />
      {err ? <ErrorWell className="-mt-3 mb-5">{err}</ErrorWell> : null}
    </>
  );
}
