import assert from "node:assert/strict";
import { initials, mayChange } from "./people.ts";

assert.equal(initials("alice.smith@example.com"), "AS");
assert.equal(initials("bob@example.com"), "BO");
assert.equal(initials("x@example.com"), "X");
assert.equal(initials(""), "?");

assert.equal(mayChange("owner", "admin"), true);
assert.equal(mayChange("owner", "owner"), false);
assert.equal(mayChange("admin", "admin"), false);
assert.equal(mayChange("admin", "guest"), true);
assert.equal(mayChange("member", "guest"), false);
console.log("people ok");
