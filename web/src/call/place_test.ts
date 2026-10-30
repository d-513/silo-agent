import { test } from "node:test";
import assert from "node:assert/strict";
import { callIsHere, callPlace } from "./place.ts";

const base = { call: true, here: false, expanded: true, wide: true, docked: false };

test("no call is drawn nowhere", () => {
  assert.equal(callPlace({ ...base, call: false }), "");
});

test("the call's own chat draws it, whatever else is open", () => {
  assert.equal(callPlace({ ...base, here: true }), "chat");
  assert.equal(callPlace({ ...base, here: true, docked: true, wide: false }), "chat");
});

test("the call screen follows you to another page", () => {
  assert.equal(callPlace(base), "aside");
});

test("with no room for a third column the call is the bar, never gone", () => {
  assert.equal(callPlace({ ...base, wide: false }), "bar");
  assert.equal(callPlace({ ...base, docked: true }), "bar");
});

test("a call folded into the bar stays one", () => {
  assert.equal(callPlace({ ...base, expanded: false }), "bar");
});

test("a call is here in its chat, or on the empty chat it is making one from", () => {
  assert.equal(callIsHere("c1", "c1", false), true);
  assert.equal(callIsHere("c1", "c2", false), false);
  assert.equal(callIsHere("c1", undefined, false), false);
  assert.equal(callIsHere("new", undefined, true), true);
  assert.equal(callIsHere("new", undefined, false), false);
  assert.equal(callIsHere("new", "c1", true), false);
});
