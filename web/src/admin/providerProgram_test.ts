import assert from "node:assert/strict";
import { installPercent, programLine } from "./providerProgram.ts";

const t = (key: string, vars: Record<string, unknown> = {}) => key.replace(/\{\{(\w+)\}\}/g, (_, k) => String(vars[k]));
const base = { name: "Claude Code", installed: false, version: "", path: "", managed: false, canInstall: true, installing: false, doneBytes: 0n, totalBytes: 0n };

assert.equal(programLine(base, t), "Claude Code is not installed on this server");
assert.equal(programLine({ ...base, installed: true, version: "2.1.3", managed: true, path: "/d/program/claude" }, t), "Claude Code 2.1.3 · installed by Silo");
assert.equal(programLine({ ...base, installed: true, version: "2.1.3", canInstall: false, path: "/opt/claude" }, t), "Claude Code 2.1.3 · your own, at /opt/claude");
assert.equal(programLine({ ...base, installed: true, path: "/usr/bin/claude" }, t), "Claude Code · found on this server at /usr/bin/claude");

assert.equal(installPercent(base), null);
assert.equal(installPercent({ installing: true, doneBytes: 5n, totalBytes: 0n }), null);
assert.equal(installPercent({ installing: true, doneBytes: 93n, totalBytes: 186n }), 50);
assert.equal(installPercent({ installing: true, doneBytes: 400n, totalBytes: 186n }), 100);

console.log("providerProgram ok");
