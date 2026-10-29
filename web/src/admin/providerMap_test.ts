import { test } from "node:test";
import assert from "node:assert/strict";
import { capOf, effective, isReady, markOf, modelOf, providerOf, providersInUse, resolveJobs } from "./providerMap.ts";

test("a job follows its setting's fallbacks", () => {
  const jobs = resolveJobs({ model: "openrouter/openai/gpt-5.6-luna", model_title: "", model_approval: "", transcribe_model: "openrouter/openai/whisper-1", "voice.transcribe_model": "", "voice.speech_model": "elevenlabs/eleven_flash_v2_5", model_voice: "groq/openai/gpt-oss-120b", embedding_model: "openrouter/openai/text-embedding-3-small" }, ["typesafe/jev-latest"]);
  const by = Object.fromEntries(jobs.map((j) => [j.key, j]));
  assert.equal(by["voice.transcribe_model"].model, "openrouter/openai/whisper-1");
  assert.equal(by["voice.transcribe_model"].inherited, "transcribe_model");
  assert.equal(by.model_approval.model, "openrouter/openai/gpt-5.6-luna");
  assert.equal(by.model_decision.model, "typesafe/jev-latest");
  assert.deepEqual(providersInUse(jobs), ["openrouter", "groq", "elevenlabs", "typesafe"]);
});

test("off is nobody", () => {
  const jobs = resolveJobs({ transcribe_model: "off" }, []);
  const hears = jobs.find((j) => j.key === "voice.transcribe_model")!;
  assert.equal(hears.model, "");
  assert.equal(jobs.find((j) => j.key === "model_voice")!.model, "");
});

test("an empty setting follows the nearest one set", () => {
  const v = { model: "a/chat", model_title: "b/title", model_approval: "" };
  assert.deepEqual(effective(v, "model_filter"), { model: "b/title", from: "model_title" });
  assert.deepEqual(effective(v, "model_approval"), { model: "b/title", from: "model_title" });
  assert.deepEqual(effective({ ...v, model_filter: "c/judge" }, "model_filter"), { model: "c/judge", from: "" });
  assert.deepEqual(effective({}, "model_subagent"), { model: "", from: "" }, "the lead's model is not a setting");
  assert.deepEqual(effective({}, "model_decision", ["t/jev"]), { model: "t/jev", from: "decision_models" });
  assert.deepEqual(effective({ transcribe_model: "off" }, "voice.transcribe_model"), { model: "", from: "transcribe_model" });
});

test("ready means a key, or a local server's address", () => {
  const fields = [
    { key: "providers.deepgram.api_key", value: "", source: 0 },
    { key: "providers.groq.api_key", value: "", source: 2 },
    { key: "providers.local.base_url", value: "http://x", source: 1 },
  ];
  assert.equal(isReady("deepgram", fields, {}, 2), false);
  assert.equal(isReady("deepgram", fields, { "providers.deepgram.api_key": "k" }, 2), true);
  assert.equal(isReady("groq", fields, {}, 2), true, "a key from the environment");
  assert.equal(isReady("local", fields, {}, 2), true);
});

test("capOf counts allowed models and reads a local server's realtime setting", () => {
  const p = { id: "local", kinds: ["text", "transcription"], liveKinds: ["transcription"] };
  assert.deepEqual(capOf(p, "transcription", ["local/whisper", "openrouter/x"], {}), { served: true, live: false, allowed: 1 });
  assert.equal(capOf(p, "transcription", [], { "providers.local.realtime": "openai" }).live, true);
  assert.equal(capOf(p, "speech", [], {}).served, false);
});

test("ids and marks", () => {
  assert.equal(providerOf("openrouter/openai/x"), "openrouter");
  assert.equal(modelOf("openrouter/openai/x"), "openai/x");
  assert.equal(markOf("OpenRouter"), "OR");
  assert.equal(markOf("Deepgram"), "DE");
  assert.equal(markOf("ElevenLabs"), "EL");
});
