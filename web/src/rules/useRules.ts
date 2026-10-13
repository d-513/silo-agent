import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { UI, type Rule, type RuleSection } from "../gen/silo/v1/ui_pb";
import { patch, reload, setBot } from "../query";
import { type Side, withDecision, withSectionDecision } from "./model";

const none: RuleSection[] = [];

// useRules loads a Bot's rule sections and its two policies (auto-approve for
// calls, output for results) and saves each change: a rule shows at once and
// rolls back if the server refuses it.
export function useRules(botId: string) {
  const sectionsQ = useQuery(UI.method.listRules, { botId });
  const sections = sectionsQ.data?.sections ?? none;
  const botQ = useQuery(UI.method.getBot, { id: botId });
  const savedPolicy = botQ.data?.autoApprove ?? "";
  const savedOutputPolicy = botQ.data?.outputPolicy ?? "";
  // null until the human types: the box then shows the saved policy.
  const [draft, setPolicy] = useState<string | null>(null);
  const [outputDraft, setOutputPolicy] = useState<string | null>(null);
  const [actErr, setErr] = useState("");
  const saver = useSave();
  const outputSaver = useSave();
  const failed = sectionsQ.error ?? botQ.error;

  useEffect(() => {
    setPolicy(null);
    setOutputPolicy(null);
  }, [botId]);

  // Both policies ride on UpdateBot, which takes the Bot's other fields
  // whole: they are read fresh and sent back as they are. The output policy
  // is optional there, so saving one never touches the other.
  async function savePolicy(side: Side = "decision") {
    setErr("");
    const output = side === "output";
    try {
      const next = await (output ? outputSaver : saver).run(async () => {
        const b = await ui.getBot({ id: botId });
        return ui.updateBot({
          id: botId,
          name: b.name,
          description: b.description,
          soul: b.soul,
          memory: b.memory,
          autoApprove: output ? b.autoApprove : (draft ?? savedPolicy),
          model: b.model,
          outputPolicy: output ? (outputDraft ?? savedOutputPolicy) : undefined,
        });
      });
      setBot(next);
      if (output) setOutputPolicy(null);
      else setPolicy(null);
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

  // One side of a rule is saved alone: a request with an output and no
  // decision leaves the call rule as it is, and the other way round.
  const send = (r: Rule, value: string, side: Side) =>
    ui.setRule({ botId, connector: r.connector, action: r.action, ...(side === "output" ? { output: value } : { decision: value }) });

  function setDecision(r: Rule, decision: string, side: Side = "decision") {
    return change(
      (curr) => withDecision(curr, r.connector, r.action, decision, side),
      () => send(r, decision, side),
    );
  }

  function setSection(s: RuleSection, decision: string, side: Side = "decision") {
    if (s.rules.every((r) => r[side] === decision)) return Promise.resolve();
    return change(
      (curr) => withSectionDecision(curr, s.id, decision, side),
      () => Promise.all(s.rules.map((r) => send(r, decision, side))),
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
    outputPolicy: outputDraft ?? savedOutputPolicy,
    setOutputPolicy,
    savedOutputPolicy,
    outputSaver,
    defaultOutputPolicy: sectionsQ.data?.defaultOutputPolicy ?? "",
    filterEnabled: sectionsQ.data?.filterEnabled ?? true,
    setDecision,
    setSection,
  };
}
