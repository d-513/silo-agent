// What the Rules page does to its sections, as plain functions so the filter and
// the optimistic updates can be checked without a screen.
import type { Rule, RuleSection } from "../gen/silo/v1/ui_pb";

// The one decision every rule in a section shares, or "" when they differ or
// there are none.
export function sectionDecision(s: RuleSection): string {
  if (s.rules.length === 0) return "";
  const d = s.rules[0].decision;
  return s.rules.every((r) => r.decision === d) ? d : "";
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

// The sections with one rule's decision changed (shown before the server agrees).
export function withDecision(sections: RuleSection[], connector: string, action: string, decision: string): RuleSection[] {
  return sections.map((s) => ({
    ...s,
    rules: s.rules.map((r) => (r.connector === connector && r.action === action ? ({ ...r, decision } as Rule) : r)),
  })) as RuleSection[];
}

// The sections with every rule of one section set to a decision.
export function withSectionDecision(sections: RuleSection[], sectionId: string, decision: string): RuleSection[] {
  return sections.map((s) =>
    s.id === sectionId ? ({ ...s, rules: s.rules.map((r) => ({ ...r, decision }) as Rule) } as RuleSection) : s,
  );
}
