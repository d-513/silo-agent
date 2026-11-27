import assert from "node:assert/strict";
import test from "node:test";
import { isFoldKey, storedFold } from "./navStore.ts";

test("only a stored fold folds the sidebar", () => {
  assert.equal(storedFold("folded"), true);
  assert.equal(storedFold("open"), false);
  assert.equal(storedFold(null), false);
  assert.equal(storedFold("1"), false);
});

test("Ctrl or ⌘ with B folds, never while typing", () => {
  const key = { key: "b", ctrlKey: true, metaKey: false, altKey: false, shiftKey: false };
  assert.equal(isFoldKey(key, false), true);
  assert.equal(isFoldKey({ ...key, ctrlKey: false, metaKey: true, key: "B" }, false), true);
  assert.equal(isFoldKey(key, true), false);
  assert.equal(isFoldKey({ ...key, shiftKey: true }, false), false);
  assert.equal(isFoldKey({ ...key, ctrlKey: false }, false), false);
});
