import { eq } from "../../testing.ts";
import { draftFrom, emptyDraft, patchAt, setAt, specOf, withImage, withName, withoutImage } from "./draft.ts";

const conn = (extra: object = {}) =>
  ({ id: "c1", name: "GitHub", description: "d", category: "", prompt: "", autoAttach: false, httpUrl: "https://x/mcp", auth: "", defaultMode: "", transport: "",
    headerKeys: [], oauthClientId: "", hasOauthClientSecret: false, stdioCommand: "", stdioArgs: [], stdioImage: "", envKeys: [], hasImage: false,
    builtin: "", fields: [], config: {}, secretsSet: [], ...extra }) as any;

// A server row with blanks becomes a draft with the form's own defaults.
const d = draftFrom(conn());
eq([d.category, d.auth, d.defaultMode, d.transport], ["General", "none", "ask", "http"], "blank fields take defaults");
eq(d.headers, [{ name: "", value: "" }], "no headers still shows one empty row");
eq(d.stdioArgs, [""], "no args still shows one empty field");
eq(d.imageId, "c1", "the draft remembers which connector's image to show");

// Saved header and env names come back with their values blank (never shown again).
const saved = draftFrom(conn({ headerKeys: [{ name: "X-Org" }], envKeys: [{ name: "TOKEN", secretName: "gh" }, { name: "PLAIN" }], stdioArgs: ["-y", "pkg"] }));
eq(saved.headers, [{ name: "X-Org", value: "" }], "header names without values");
eq(saved.env, [{ name: "TOKEN", value: "", secret: "gh" }, { name: "PLAIN", value: "", secret: "" }], "env names with their secret name");
eq(saved.stdioArgs, ["-y", "pkg"], "args are kept");

// A built-in's starting config is the stored (or default) value of each plain field; secrets are never sent down.
const field = (key: string, secret: boolean) => ({ key, secret }) as any;
const b = draftFrom(conn({ builtin: "email", fields: [field("host", false), field("password", true)], config: { host: "imap.x", password: "leak" } }));
eq(b.config, { host: "imap.x" }, "plain fields only");
eq(draftFrom(conn({ fields: [field("host", false)], config: { host: "x" } })).config, {}, "a non-built-in has no config");

// What the server is sent: an HTTP connector, a STDIO one, a built-in.
const http = { ...emptyDraft(), name: "n", httpUrl: "u", auth: "oauth", headers: [{ name: " ", value: "v" }, { name: "A", value: "b" }] };
eq(specOf(http).headers, [{ name: "A", value: "b" }], "blank header names are dropped");
eq((specOf(http) as any).auth, "oauth", "http keeps its auth");
const stdio = { ...emptyDraft(), transport: "stdio", auth: "oauth", stdioArgs: [" -y ", "", "pkg"], env: [{ name: "", value: "x", secret: "" }, { name: "K", value: "v", secret: "s" }] };
eq((specOf(stdio) as any).auth, "none", "stdio never uses auth");
eq((specOf(stdio) as any).stdioArgs, ["-y", "pkg"], "args are trimmed and blanks dropped");
eq((specOf(stdio) as any).env, [{ name: "K", value: "v", secret: "s" }], "blank env names are dropped");
const bi = specOf({ ...emptyDraft(), builtin: "email", config: { host: "h" } }) as any;
eq([bi.config, "httpUrl" in bi, "transport" in bi], [{ host: "h" }, false, false], "a built-in sends config, not transport settings");

// A new preset's identifier follows the name until someone types their own.
const named = withName(withName(emptyDraft(), "fal", true), "fal.ai", true);
eq([named.name, named.identifier], ["fal.ai", "fal_ai"], "the identifier follows the name");
const own = withName({ ...named, identifier: "images" }, "fal.ai images", true);
eq(own.identifier, "images", "a typed identifier is left alone");
eq(withName({ ...named, identifier: "fal_ai" }, "Renamed", false).identifier, "fal_ai", "a saved preset keeps its identifier through a rename");
eq((specOf({ ...emptyDraft(), identifier: "fal_" }) as any).identifier, "fal", "what is sent is settled");
eq(draftFrom(conn({ identifier: "fal_ai", autoenabled: true })).identifier, "fal_ai", "a preset's identifier fills the form");

// List edits.
eq(patchAt([{ a: 1, b: 2 }, { a: 3, b: 4 }], 1, { b: 9 }), [{ a: 1, b: 2 }, { a: 3, b: 9 }], "patchAt merges into one item");
eq(setAt(["x", "y"], 0, "z"), ["z", "y"], "setAt replaces one item");

// Picking and removing an image.
const withPic = withImage({ ...emptyDraft(), clearImage: true }, new Uint8Array([1]), "image/png", "blob:1");
eq([withPic.hasImage, withPic.clearImage, withPic.imageType, withPic.previewUrl], [true, false, "image/png", "blob:1"], "a picked image replaces a pending removal");
const without = withoutImage(withPic);
eq([without.hasImage, without.clearImage, without.image, without.previewUrl], [false, true, undefined, undefined], "removing marks it for deletion");
