import { fmtClock, insertDictation, pickMime } from "./voice.ts";
import { eq } from "./testing.ts";

eq(pickMime(() => true), "audio/webm;codecs=opus", "first supported wins");
eq(pickMime((m) => m === "audio/mp4"), "audio/mp4", "Safari falls through to mp4");
eq(pickMime(() => false), "", "none supported");
eq(
  pickMime(() => {
    throw new Error("unknown");
  }),
  "",
  "a throwing probe is not supported",
);

eq(insertDictation("", 0, 0, " hello "), { text: "hello", caret: 5 }, "empty draft");
eq(insertDictation("say", 3, 3, "hello"), { text: "say hello", caret: 9 }, "space before");
eq(insertDictation("world", 0, 0, "hello"), { text: "hello world", caret: 5 }, "space after");
eq(insertDictation("a  b", 2, 2, "x"), { text: "a x b", caret: 3 }, "no double spaces");
eq(insertDictation("keep REPLACE end", 5, 12, "new"), { text: "keep new end", caret: 8 }, "replaces a selection");
eq(insertDictation("abc", 99, 99, "d"), { text: "abc d", caret: 5 }, "clamps past the end");
eq(insertDictation("abc", 1, 1, "   "), { text: "abc", caret: 1 }, "silence changes nothing");

eq(fmtClock(0), "0:00", "zero");
eq(fmtClock(7400), "0:07", "seconds");
eq(fmtClock(600000), "10:00", "minutes");

console.log("ok");
