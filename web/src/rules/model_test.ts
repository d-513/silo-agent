import { eq } from "../testing.ts";
import { countRules, filterSections, sectionDecision, withDecision, withSectionDecision } from "./model.ts";

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
