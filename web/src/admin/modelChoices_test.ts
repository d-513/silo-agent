import { test } from "node:test";
import assert from "node:assert/strict";
import { customChoice, matches } from "./modelChoices.ts";

const providers = [
  { id: "openrouter", kinds: ["text", "decision", "speech"] },
  { id: "typesafe", kinds: ["decision"] },
  { id: "deepgram", kinds: ["transcription", "speech"] },
];

test("a query matches by every word, in the id or the name", () => {
  const m = { id: "openrouter/anthropic/claude-haiku-4.5", name: "Claude Haiku 4.5" };
  assert.equal(matches(m, ""), true);
  assert.equal(matches(m, "haiku open"), true);
  assert.equal(matches(m, "Claude 4.5"), true);
  assert.equal(matches(m, "haiku gpt"), false);
});

test("a typed id is offered only for a provider that serves the job", () => {
  assert.equal(customChoice("openrouter/x/new-model", ["text"], providers, [])?.kind, "text");
  assert.equal(customChoice("deepgram/nova-9", ["transcription"], providers, [])?.kind, "transcription");
  assert.equal(customChoice("deepgram/nova-9", ["text"], providers, []), null, "Deepgram writes nothing");
  assert.equal(customChoice("nobody/x", ["text"], providers, []), null);
  assert.equal(customChoice("haiku", ["text"], providers, []), null, "not an id");
  assert.equal(customChoice("openrouter/", ["text"], providers, []), null);
  assert.equal(customChoice("openrouter/a b", ["text"], providers, []), null);
  assert.equal(customChoice("openrouter/x", ["text"], providers, [{ id: "openrouter/x" }]), null, "a list has it");
});

test("a judge's typed id is read as the server reads it", () => {
  // An id on no list is text where its provider writes, else what the provider serves.
  assert.equal(customChoice("openrouter/x", ["decision", "text"], providers, [])?.kind, "text");
  assert.equal(customChoice("typesafe/jev-9", ["decision", "text"], providers, [])?.kind, "decision");
});
