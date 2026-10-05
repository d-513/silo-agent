import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { UI, type Rule, type RuleSection } from "../gen/silo/v1/ui_pb";
import { patch, reload, setBot } from "../query";
import { withDecision, withSectionDecision } from "./model";

const none: RuleSection[] = [];

// useRules loads a Bot's rule sections and auto-approve policy and saves each
// change: a decision shows at once and rolls back if the server refuses it.
export function useRules(botId: string) {
  const sectionsQ = useQuery(UI.method.listRules, { botId });
  const sections = sectionsQ.data?.sections ?? none;
  const botQ = useQuery(UI.method.getBot, { id: botId });
  const savedPolicy = botQ.data?.autoApprove ?? "";
  // null until the human types: the box then shows the saved policy.
  const [draft, setPolicy] = useState<string | null>(null);
  const [actErr, setErr] = useState("");
  const saver = useSave();
  const failed = sectionsQ.error ?? botQ.error;

  useEffect(() => setPolicy(null), [botId]);

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
          autoApprove: draft ?? savedPolicy,
          model: b.model,
        });
      });
      setBot(next);
      setPolicy(null);
    } catch (e) {
      setErr(fail(e));
    }
  }

  // change applies an optimistic edit, runs the saves, then reloads; on failure
  // the sections go back to what they were.
  async function change(optimistic: (curr: RuleSection[]) => RuleSection[], save: () => Promise<unknown>) {
    setErr("");
    const prev = sections;
    const put = (next: RuleSection[]) => patch(UI.method.listRules, { botId }, (r) => ({ ...r, sections: next }));
    put(optimistic(prev));
    try {
      await save();
      await reload(UI.method.listRules, { botId });
    } catch (e) {
      put(prev);
      setErr(fail(e));
    }
  }

  function setDecision(r: Rule, decision: string) {
    return change(
      (curr) => withDecision(curr, r.connector, r.action, decision),
      () => ui.setRule({ botId, connector: r.connector, action: r.action, decision }),
    );
  }

  function setSection(s: RuleSection, decision: string) {
    if (s.rules.every((r) => r.decision === decision)) return Promise.resolve();
    return change(
      (curr) => withSectionDecision(curr, s.id, decision),
      () => Promise.all(s.rules.map((r) => ui.setRule({ botId, connector: r.connector, action: r.action, decision }))),
    );
  }

  return {
    sections,
    loaded: !!sectionsQ.data,
    err: actErr || (failed ? fail(failed) : ""),
    policy: draft ?? savedPolicy,
    setPolicy,
    savedPolicy,
    saver,
    savePolicy,
    setDecision,
    setSection,
  };
}
