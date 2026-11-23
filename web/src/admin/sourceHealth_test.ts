import { eq } from "../testing.ts";
import { sourceLook, testLine, waitWord } from "./sourceHealth.ts";

const now = new Date("2026-11-21T12:00:00Z");
const src = (over: object) => ({ on: true, offReason: "", state: "", reason: "", restingUntil: "", lastOk: "", ...over });

// Off says what would turn it on, whichever of the server's reasons it is.
eq(sourceLook(src({ on: false, offReason: "needs its API key" }), now).detail, "Add its API key above to use it", "needs a key");
eq(sourceLook(src({ on: false, offReason: "off unless named in the sources list: it answers plain requests with a script challenge" }), now).word, "Off", "opt-in");
eq(
  sourceLook(src({ on: false, offReason: "off unless named in the sources list: it answers plain requests with a script challenge" }), now).detail,
  "Answers most addresses with a script challenge; name it under Sources to try it",
  "opt-in says how to try it",
);
eq(sourceLook(src({ on: false, offReason: "not in the sources list" }), now).detail, "Not named under Sources", "left out");
eq(sourceLook(src({ on: false, offReason: "something new" }), now).detail, "something new", "an unknown reason is shown as it is");

// On: answering, resting with why and for how long, failed, or never asked.
eq(sourceLook(src({ state: "ok" }), now).lamp, "online", "answering is green");
const resting = sourceLook(src({ state: "resting", reason: "rate-limited (HTTP 429)", restingUntil: "2026-11-21T12:02:00Z" }), now);
eq(resting.word, "Resting", "resting");
eq(resting.detail, "rate-limited (HTTP 429); left alone for 2 min", "why and how long");
eq(sourceLook(src({ state: "failing", reason: "timed out" }), now).detail, "timed out; the next search tries it again", "past its rest");
eq(sourceLook(src({}), now).word, "Not asked yet", "nothing seen since the start");

eq(waitWord("2026-11-21T12:00:30Z", now), "30 s", "seconds");
eq(waitWord("2026-11-21T12:29:40Z", now), "30 min", "minutes");
eq(waitWord("2026-11-21T11:00:00Z", now), "1 s", "a time already past");
eq(waitWord("", now), "a moment", "no time");

eq(testLine({ tookMs: 1240, hits: [{}, {}, {}] as never, error: "" }), { ok: true, text: "3 results in 1.2 s" }, "results");
eq(testLine({ tookMs: 300, hits: [], error: "" }), { ok: false, text: "No results in 0.3 s" }, "none");
eq(testLine({ tookMs: 0, hits: [], error: "SearXNG answered HTTP 502" }), { ok: false, text: "SearXNG answered HTTP 502" }, "an error is the line");
