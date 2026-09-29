import { fitThinking, thinkingLabel } from "./thinking.ts";

function eq(got: unknown, want: unknown, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

// Same cases as TestNearestThinking in internal/llm.
eq(fitThinking("high", ["low", "medium", "high"]), "high", "listed");
eq(fitThinking("xhigh", ["low", "medium", "high"]), "high", "down to nearest");
eq(fitThinking("minimal", ["low", "medium", "high"]), "low", "up to nearest");
eq(fitThinking("off", ["low", "medium", "high"]), "low", "off unavailable");
eq(fitThinking("medium", ["low", "high"]), "low", "tie goes cheaper");
eq(fitThinking("high", []), "", "no levels");
eq(fitThinking("", ["low"]), "", "default stays default");
eq(fitThinking("bogus", ["low"]), "", "unknown level");

eq(thinkingLabel(""), "Default", "default label");
eq(thinkingLabel("xhigh"), "Extra high", "xhigh label");

console.log("thinking_test ok");
