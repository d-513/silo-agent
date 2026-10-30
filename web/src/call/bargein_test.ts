import { test } from "node:test";
import assert from "node:assert/strict";
import { BargeIn, type BargeMsg } from "./bargein.ts";

function setup() {
  const sent: BargeMsg[] = [];
  const mutes: boolean[] = [];
  const b = new BargeIn(
    (m) => sent.push(m),
    (on) => mutes.push(on),
  );
  return { b, sent, mutes };
}

const talk = (b: BargeIn, frames: number) => {
  for (let i = 0; i < frames; i++) b.frame(0.9, true);
};

test("speech over the voice ducks it, and being sure stops it", () => {
  const { b, sent, mutes } = setup();
  b.speaking = true;
  talk(b, 3);
  b.sure();
  talk(b, 5);
  assert.deepEqual(sent, ["duck", "interrupt"]);
  assert.deepEqual(mutes, [true, true]);
});

test("a false start lifts the duck", () => {
  const { b, sent, mutes } = setup();
  b.speaking = true;
  talk(b, 3);
  b.ended();
  assert.deepEqual(sent, ["duck", "unduck"]);
  assert.deepEqual(mutes, [true, false]);
});

test("the detector being sure stops the voice even if the frames never ducked it", () => {
  const { b, sent } = setup();
  b.speaking = true;
  b.frame(0.6, true);
  b.sure();
  assert.deepEqual(sent, ["interrupt"]);
});

test("a voice that starts while you are talking is stopped, not left paused", () => {
  const { b, sent } = setup();
  b.sure();
  assert.deepEqual(sent, []);
  b.speaking = true;
  talk(b, 3);
  assert.deepEqual(sent, ["interrupt"]);
  b.ended();
  assert.deepEqual(sent, ["interrupt"]);
});

test("after a cut the next sentence is heard, and can be cut in its turn", () => {
  const { b, sent, mutes } = setup();
  b.speaking = true;
  b.sure();
  b.said();
  assert.equal(mutes.at(-1), false);
  talk(b, 1);
  assert.deepEqual(sent, ["interrupt", "interrupt"]);
});

test("a sentence arriving during a duck does not forget the voice is paused", () => {
  const { b, sent } = setup();
  b.speaking = true;
  talk(b, 3);
  b.said();
  b.ended();
  assert.deepEqual(sent, ["duck", "unduck"]);
});

test("hold-to-talk ducks nothing: the button decides", () => {
  const { b, sent } = setup();
  b.speaking = true;
  for (let i = 0; i < 5; i++) b.frame(0.9, false);
  assert.deepEqual(sent, []);
  b.sure();
  assert.deepEqual(sent, ["interrupt"]);
});

test("quiet does not count, and nothing happens without a voice", () => {
  const { b, sent } = setup();
  talk(b, 5);
  b.sure();
  b.ended();
  assert.deepEqual(sent, []);
  b.speaking = true;
  b.frame(0.9, true);
  b.frame(0.9, true);
  b.frame(0.1, true);
  b.frame(0.9, true);
  assert.deepEqual(sent, []);
});
