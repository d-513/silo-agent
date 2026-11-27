import { eq } from "../testing.ts";
import { bareModel, filterModels, fmtContext, kindOf, kindsIn, withFast, withModel } from "./providerModels.ts";
import { FIRST_SECTION, isFormSection, isSection, SECTIONS } from "./sections.ts";

eq(bareModel("openrouter/openai/gpt-5.6-luna"), "openai/gpt-5.6-luna", "the model part keeps its own slashes");
eq(bareModel("anthropic/claude-opus-5-5"), "claude-opus-5-5", "one slash");
eq(bareModel("bare"), "bare", "no provider");

eq(fmtContext(400000), "400k", "thousands");
eq(fmtContext(1000000), "1M", "a million");
eq(fmtContext(1048576), "1M", "a binary million rounds");
eq(fmtContext(2500000), "2.5M", "a fraction of a million");
eq(fmtContext(8192), "8k", "small windows round");
eq(fmtContext(512), "512", "under a thousand");
eq(fmtContext(0), "", "unknown is blank");

const list = [
  { id: "openrouter/openai/gpt-5.6-luna", name: "OpenAI: GPT-5.6 Luna" },
  { id: "openrouter/anthropic/claude-opus-5.5", name: "Anthropic: Claude Opus 5.5" },
  { id: "openrouter/deepseek/deepseek-v4.1-flash", name: "" },
];
eq(filterModels(list, ""), list, "no query keeps the list");
eq(filterModels(list, "  ").length, 3, "blank query keeps the list");
eq(filterModels(list, "OPUS").map((m) => m.id), ["openrouter/anthropic/claude-opus-5.5"], "case does not matter");
eq(filterModels(list, "claude anthropic").length, 1, "every word must match, in any order");
eq(filterModels(list, "luna flash").length, 0, "words are not alternatives");
eq(filterModels(list, "Luna").length, 1, "the name is searched too");

eq(withModel(["a/x"], "b/y", true), ["a/x", "b/y"], "add goes last");
const same = ["a/x"];
eq(withModel(same, "a/x", true) === same, true, "adding a model twice changes nothing");
eq(withModel(["a/x", "b/y"], "a/x", false), ["b/y"], "remove");
eq(withModel(same, "c/z", false) === same, true, "removing a stranger changes nothing");

eq(kindOf({ kind: "decision" }), "decision", "a decision model");
eq(kindOf({ kind: "" }), "text", "an unmarked model is text");
eq(kindOf({}), "text", "a model from before kinds is text");
eq(kindsIn([{ kind: "decision" }, { kind: "text" }]), ["text", "decision"], "text comes first");
eq(kindsIn([{ kind: "decision" }]), ["decision"], "a decision-only provider");
eq(kindsIn([]), [], "an empty list has no kinds");
eq(isSection(FIRST_SECTION), true, "the first section is a section");
eq(isSection("nope"), false, "unknown section");
eq(isSection(undefined), false, "no section");
eq(SECTIONS.filter((s) => !isFormSection(s.id)).map((s) => s.id), ["yaml"], "the file is not the form");

const fastList = withFast([
  { id: "claude/opus", fast: true },
  { id: "claude/haiku", fast: false },
]);
eq(fastList.map((m) => m.id), ["claude/opus", "claude/opus:fast", "claude/haiku"], "a fast row follows each model that has fast mode");
eq(fastList[1].fastOf, "claude/opus", "the fast row knows its model");
eq(fastList[1].fast, false, "a fast row is not offered a fast row of its own");
eq(withFast([{ id: "claude/opus:fast", fast: true }]).length, 1, "an id already fast gets no second suffix");
