import { test } from "node:test";
import assert from "node:assert/strict";
import { callClock, encodeWAV, joinFrames, loudness, playoutMs } from "./audio.ts";

test("encodeWAV writes a 16-bit mono header and clips the samples", () => {
  const wav = new DataView(encodeWAV(new Float32Array([0, 1, -1, 2]), 16000));
  const text = (at: number) => String.fromCharCode(...[0, 1, 2, 3].map((i) => wav.getUint8(at + i)));
  assert.equal(text(0), "RIFF");
  assert.equal(text(8), "WAVE");
  assert.equal(wav.getUint32(24, true), 16000);
  assert.equal(wav.getUint16(34, true), 16);
  assert.equal(wav.getUint32(40, true), 8);
  assert.equal(wav.getInt16(46, true), 0x7fff);
  assert.equal(wav.getInt16(48, true), -0x8000);
  assert.equal(wav.getInt16(50, true), 0x7fff, "out of range is clipped");
});

test("joinFrames keeps the order", () => {
  assert.deepEqual([...joinFrames([new Float32Array([1, 2]), new Float32Array([3])])], [1, 2, 3]);
});

test("loudness is 0 for silence and fills for speech", () => {
  assert.equal(loudness(new Float32Array(512)), 0);
  assert.ok(loudness(new Float32Array(512).fill(0.3)) > 0.9);
});

test("playoutMs reads the jitter buffer and adds the device's share", () => {
  assert.equal(playoutMs({ jitterBufferDelay: 6, jitterBufferEmittedCount: 100 }), 100);
  assert.equal(playoutMs({}), 0);
});

test("callClock is m:ss", () => {
  assert.equal(callClock(65_400), "1:05");
});
