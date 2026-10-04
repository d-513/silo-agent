import { eq } from "../testing.ts";
import { NAME_RE, slug, uniqueName, visible } from "./driveModel.ts";

// A drive's name becomes a folder under /workspace/drives, so it is a slug.
eq(slug("Google Drive"), "google-drive", "spaces become dashes");
eq(slug("  My_Files!! 2026 "), "my-files-2026", "punctuation collapses, ends trimmed");
eq(slug("x".repeat(60)).length, 40, "capped at 40");
eq(slug("***"), "", "nothing left");

eq(uniqueName("Google Drive", new Set()), "google-drive", "free name");
eq(uniqueName("Google Drive", new Set(["google-drive"])), "google-drive-2", "taken");
eq(uniqueName("Google Drive", new Set(["google-drive", "google-drive-2"])), "google-drive-3", "numbers skip used");
eq(uniqueName("***", new Set()), "drive", "falls back to drive");

eq(NAME_RE.test("my-drive"), true, "valid");
eq(NAME_RE.test("My-Drive"), false, "no capitals");
eq(NAME_RE.test("-drive"), false, "no leading dash");
eq(NAME_RE.test(""), false, "not empty");
eq(NAME_RE.test("a".repeat(41)), false, "at most 40");

// A field can depend on another field's value (or its default).
const t = { vars: [{ kind: "user", key: "mode", defaultValue: "basic" }] } as any;
const v = { visibleIf: { mode: "advanced" } } as any;
eq(visible({ visibleIf: {} } as any, {}, t), true, "no condition");
eq(visible(v, { mode: "advanced" }, t), true, "condition met");
eq(visible(v, { mode: "basic" }, t), false, "condition not met");
eq(visible(v, {}, t), false, "unset falls back to the default, which does not match");
eq(visible({ visibleIf: { mode: "basic" } } as any, {}, t), true, "default can satisfy it");
