import assert from "node:assert/strict";
import { defaultFormValues, isDisplay, normalizeOptions, parseBlocks } from "./blocks.ts";

const blocks = parseBlocks(
  JSON.stringify([
    { type: "metrics", items: [{ label: "Modified", value: "4", delta: "+2", tone: "warn" }] },
    { type: "switch", name: "clean_tmp", label: "Clean /tmp", default: true },
    { type: "select", name: "target", label: "Target", options: ["main", "staging"], default: "staging" },
    { type: "checkboxes", name: "checks", options: ["lint", "test"], default: ["lint"] },
  ]),
);

assert.equal(blocks.length, 4);
const defaults = defaultFormValues(blocks);
assert.deepEqual(defaults, {
  clean_tmp: true,
  target: "staging",
  checks: ["lint"],
});

assert.deepEqual(parseBlocks("not json"), []);
assert.deepEqual(parseBlocks('[{"type":"markdown","text":"hi"},3,{"x":1}]'), [{ type: "markdown", text: "hi" }]);
assert.deepEqual(normalizeOptions(["a", { value: "b", label: "Bee" }]), [
  { value: "a", label: "a" },
  { value: "b", label: "Bee", hint: undefined },
]);

// An answered card keeps only what it showed, not its controls.
assert.deepEqual(
  blocks.filter(isDisplay).map((b) => b.type),
  ["metrics"],
);

console.log("ask blocks ok");
