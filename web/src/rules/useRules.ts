import { useEffect, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import type { Rule, RuleSection } from "../gen/silo/v1/ui_pb";
import { withDecision, withSectionDecision } from "./model";

// useRules loads a Bot's rule sections and auto-approve policy and saves each
// change: a decision shows at once and rolls back if the server refuses it.
export function useRules(botId: string) {
  const [sections, setSections] = useState<RuleSection[]>([]);
  const [loaded, setLoaded] = useState(false);
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

  // change applies an optimistic edit, runs the saves, then reloads; on failure
  // the sections go back to what they were.
  async function change(optimistic: (curr: RuleSection[]) => RuleSection[], save: () => Promise<unknown>) {
    setErr("");
    const prev = sections;
    setSections(optimistic);
    try {
      await save();
      await load();
    } catch (e) {
      setSections(prev);
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

  return { sections, loaded, err, policy, setPolicy, savedPolicy, saver, savePolicy, setDecision, setSection };
}
