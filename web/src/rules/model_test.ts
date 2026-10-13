import { eq } from "../testing.ts";
import { countRules, filterSections, sectionDecision, sideSections, withDecision, withSectionDecision } from "./model.ts";

const rule = (connector: string, action: string, title: string, decision: string) => ({ connector, action, title, decision }) as any;
const section = (id: string, title: string, rules: any[]) => ({ id, title, summary: "", rules }) as any;

const sections = [
  section("bot", "Bot", [rule("bot", "remember", "Remember", "allow"), rule("bot", "recall", "Recall", "allow")]),
  section("gh", "GitHub", [rule("github", "create_issue", "Create issue", "ask"), rule("github", "list_repos", "List repos", "allow")]),
  section("empty", "Nothing", []),
];

eq(sectionDecision(sections[0]), "allow", "all rules agree");
eq(sectionDecision(sections[1]), "", "mixed rules have no shared decision");
eq(sectionDecision(sections[2]), "", "a section with no rules has none");

eq(filterSections(sections, "   ").length, 3, "a blank search keeps everything");
eq(filterSections(sections, "github").map((s) => [s.id, s.rules.length]), [["gh", 2]], "a title match keeps all its rules");
eq(filterSections(sections, "ISSUE").map((s) => [s.id, s.rules.map((r) => r.action)]), [["gh", ["create_issue"]]], "rule match is case-insensitive and keeps only matching rules");
eq(filterSections(sections, "recall").map((s) => s.id), ["bot"], "matches the action");
eq(filterSections(sections, "zzz"), [], "no match");

eq(countRules(sections), 4, "rules across sections");

const changed = withDecision(sections, "github", "list_repos", "deny");
eq(changed[1].rules.map((r) => r.decision), ["ask", "deny"], "only the named rule changes");
eq(sections[1].rules[1].decision, "allow", "the original is untouched");
eq(changed[0].rules.map((r) => r.decision), ["allow", "allow"], "other sections are untouched");

const all = withSectionDecision(sections, "gh", "auto");
eq(all[1].rules.map((r) => r.decision), ["auto", "auto"], "every rule in the section changes");
eq(all[0].rules.map((r) => r.decision), ["allow", "allow"], "other sections are untouched");

// The output side: its own value per rule, and only for actions that have one.
const out = (connector: string, action: string, decision: string, output: string) => ({ connector, action, title: action, decision, output }) as any;
const both = [
  section("bot", "Bot", [out("web", "search", "allow", "check"), out("python", "run", "allow", "pass")]),
  section("secrets", "Secrets", [out("secrets", "token", "ask", "")]),
  section("gh", "GitHub", [out("github", "list", "allow", "check"), out("github", "get", "ask", "check")]),
];
eq(sideSections(both, "decision").length, 3, "every section has call rules");
eq(sideSections(both, "output").map((s) => s.id), ["bot", "gh"], "a section with no results to filter is not on the output side");
eq(sectionDecision(both[2], "output"), "check", "a section's shared output rule");
eq(sectionDecision(both[2]), "", "while its call rules differ");
eq(sectionDecision(both[0], "output"), "", "mixed output rules");

const flagged = withDecision(both, "web", "search", "flag", "output");
eq(flagged[0].rules.map((r) => [r.decision, r.output]), [["allow", "flag"], ["allow", "pass"]], "the output side changes alone");
const held = withSectionDecision(both, "gh", "withhold", "output");
eq(held[2].rules.map((r) => [r.decision, r.output]), [["allow", "withhold"], ["ask", "withhold"]], "a section's output rules, call rules untouched");
eq(withSectionDecision(both, "secrets", "check", "output")[1].rules[0].output, "", "a rule with no output side gets none");
