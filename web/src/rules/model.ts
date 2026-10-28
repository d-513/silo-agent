// What the Rules page does to its sections, as plain functions so the filter and
// the optimistic updates can be checked without a screen.
import type { Rule, RuleSection } from "../gen/silo/v1/ui_pb";

// A rule has two sides: the call rule (may the action run) and the output
// rule (what happens to its result before the Bot reads it). The page shows
// one side at a time.
export type Side = "decision" | "output";

// The one decision every rule in a section shares on a side, or "" when they
// differ or there are none.
export function sectionDecision(s: RuleSection, side: Side = "decision"): string {
  if (s.rules.length === 0) return "";
  const d = s.rules[0][side];
  return s.rules.every((r) => r[side] === d) ? d : "";
}

// The sections as one side shows them. Every action has a call rule; only an
// action whose result the Bot reads has an output rule (a secret and a channel
// send have none), and a section left with nothing to set is dropped.
export function sideSections(sections: RuleSection[], side: Side): RuleSection[] {
  if (side === "decision") return sections;
  return sections
    .map((s) => ({ ...s, rules: s.rules.filter((r) => r.output !== "") }) as RuleSection)
    .filter((s) => s.rules.length > 0);
}

// The sections that match a search: a section whose title matches keeps all
// its rules, otherwise only the rules whose title, action or connector match.
export function filterSections(sections: RuleSection[], query: string): RuleSection[] {
  const q = query.trim().toLowerCase();
  if (!q) return sections;
  return sections
    .map((s) => {
      const titleMatch = s.title.toLowerCase().includes(q);
      const matching = s.rules.filter(
        (r) => r.title.toLowerCase().includes(q) || r.action.toLowerCase().includes(q) || r.connector.toLowerCase().includes(q),
      );
      if (titleMatch || matching.length > 0) {
        return { ...s, rules: titleMatch ? s.rules : matching } as RuleSection;
      }
      return null;
    })
    .filter((s): s is RuleSection => s !== null);
}

export function countRules(sections: RuleSection[]): number {
  return sections.reduce((sum, s) => sum + s.rules.length, 0);
}

// How much a mode holds a call back (allow, auto, ask, deny) and how much an
// output mode hides (pass, flag, check, withhold). The operator's locked value
// is a floor: a looser mode is not offered. An unknown mode ranks as ask or
// check, so a bad value can never loosen a lock.
const callRank: Record<string, number> = { allow: 0, auto: 1, ask: 2, deny: 3 };
const outputRank: Record<string, number> = { pass: 0, flag: 1, check: 2, withhold: 3 };

export function strictness(mode: string, side: Side = "decision"): number {
  return (side === "output" ? outputRank : callRank)[mode] ?? 2;
}

type Floored = Pick<Rule, "locked" | "operatorDecision" | "operatorOutput" | "spaceDecision" | "spaceOutput">;

// ruleFloor is the value a rule's side may not go below, and who holds it:
// the operator (a locked row) or the Workspace (always a floor), the
// stricter of the two; "" when neither holds it.
export function ruleFloor(r: Floored, side: Side): { mode: string; by: "operator" | "workspace" | "" } {
  const op = r.locked ? (side === "output" ? r.operatorOutput : r.operatorDecision) : "";
  const ws = side === "output" ? r.spaceOutput : r.spaceDecision;
  if (ws && (!op || strictness(ws, side) > strictness(op, side))) return { mode: ws, by: "workspace" };
  if (op) return { mode: op, by: "operator" };
  return { mode: "", by: "" };
}

// floorOf is the locked value for a side of a section: the strictest one
// among its rules, or "" when nobody holds any of them.
export function floorOf(s: RuleSection, side: Side): string {
  let floor = "";
  for (const r of s.rules) {
    const v = ruleFloor(r, side).mode;
    if (v && (floor === "" || strictness(v, side) > strictness(floor, side))) floor = v;
  }
  return floor;
}

// The sections with one side of one rule changed (shown before the server agrees).
export function withDecision(sections: RuleSection[], connector: string, action: string, decision: string, side: Side = "decision"): RuleSection[] {
  return sections.map((s) => ({
    ...s,
    rules: s.rules.map((r) => (r.connector === connector && r.action === action ? ({ ...r, [side]: decision } as Rule) : r)),
  })) as RuleSection[];
}

// The sections with one side of every rule of one section set. On the output
// side a rule with no output rule keeps none.
export function withSectionDecision(sections: RuleSection[], sectionId: string, decision: string, side: Side = "decision"): RuleSection[] {
  return sections.map((s) =>
    s.id === sectionId
      ? ({ ...s, rules: s.rules.map((r) => (side === "output" && r.output === "" ? r : ({ ...r, [side]: decision } as Rule))) } as RuleSection)
      : s,
  );
}
