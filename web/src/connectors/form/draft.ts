// The connector form's working copy and what it turns into: a draft is what the
// fields edit, a spec is what the server is sent. Plain functions, so the
// round trip can be checked without a screen.
import type { ChannelField, Connector } from "../../gen/silo/v1/ui_pb";
import { identOf } from "./ident.ts";

export type HeaderDraft = { name: string; value: string };
export type EnvDraft = { name: string; value: string; secret: string };

export type ConnectorDraft = {
  name: string;
  // Library presets only: the name autoenable_connectors knows it by.
  identifier: string;
  description: string;
  category: string;
  prompt: string;
  autoAttach: boolean;
  // Read-only: autoenable_connectors names this preset.
  autoenabled: boolean;
  httpUrl: string;
  auth: string;
  defaultMode: string;
  transport: string;
  headers: HeaderDraft[];
  oauthClientId: string;
  oauthClientSecret: string;
  hasOauthClientSecret: boolean;
  stdioCommand: string;
  stdioArgs: string[];
  stdioImage: string;
  env: EnvDraft[];
  image?: Uint8Array;
  imageType: string;
  hasImage: boolean;
  clearImage: boolean;
  imageId?: string;
  previewUrl?: string;
  // Built-in connectors: Go-declared fields instead of transport settings.
  builtin: string;
  fields: ChannelField[];
  config: Record<string, string>;
  secretsSet: string[];
};

export function emptyDraft(): ConnectorDraft {
  return {
    name: "",
    identifier: "",
    description: "",
    category: "Custom",
    prompt: "",
    autoAttach: false,
    autoenabled: false,
    httpUrl: "",
    auth: "none",
    defaultMode: "ask",
    transport: "http",
    headers: [{ name: "", value: "" }],
    oauthClientId: "",
    oauthClientSecret: "",
    hasOauthClientSecret: false,
    stdioCommand: "",
    stdioArgs: [""],
    stdioImage: "",
    env: [{ name: "", value: "", secret: "" }],
    imageType: "",
    hasImage: false,
    clearImage: false,
    builtin: "",
    fields: [],
    config: {},
    secretsSet: [],
  };
}

export function draftFrom(c: Connector): ConnectorDraft {
  return {
    name: c.name,
    identifier: c.identifier || "",
    description: c.description,
    category: c.category || "General",
    prompt: c.prompt || "",
    autoAttach: c.autoAttach,
    autoenabled: !!c.autoenabled,
    httpUrl: c.httpUrl,
    auth: c.auth || "none",
    defaultMode: c.defaultMode || "ask",
    transport: c.transport || "http",
    headers: c.headerKeys.length ? c.headerKeys.map((h) => ({ name: h.name, value: "" })) : [{ name: "", value: "" }],
    oauthClientId: c.oauthClientId || "",
    oauthClientSecret: "",
    hasOauthClientSecret: c.hasOauthClientSecret,
    stdioCommand: c.stdioCommand || "",
    stdioArgs: c.stdioArgs.length ? c.stdioArgs : [""],
    stdioImage: c.stdioImage || "",
    env: c.envKeys.length
      ? c.envKeys.map((e) => ({ name: e.name, value: "", secret: e.secretName || "" }))
      : [{ name: "", value: "", secret: "" }],
    imageType: "",
    hasImage: c.hasImage,
    clearImage: false,
    imageId: c.id,
    builtin: c.builtin || "",
    fields: c.fields,
    config: builtinDefaults(c),
    secretsSet: c.secretsSet,
  };
}

// builtinDefaults is the form's starting config: the server sends stored
// values on a Bot copy and the field defaults on a library preset.
function builtinDefaults(c: Connector): Record<string, string> {
  if (!c.builtin) return {};
  const out: Record<string, string> = {};
  for (const f of c.fields) {
    if (!f.secret) out[f.key] = c.config[f.key] ?? "";
  }
  return out;
}

export function specOf(d: ConnectorDraft) {
  if (d.builtin) {
    return {
      name: d.name,
      identifier: identOf(d.identifier),
      description: d.description,
      category: d.category,
      prompt: d.prompt,
      autoAttach: d.autoAttach,
      defaultMode: d.defaultMode,
      config: d.config,
      image: d.image,
      imageType: d.imageType,
      clearImage: d.clearImage,
    };
  }
  return {
    name: d.name,
    identifier: identOf(d.identifier),
    description: d.description,
    category: d.category,
    prompt: d.prompt,
    autoAttach: d.autoAttach,
    httpUrl: d.httpUrl,
    auth: d.transport === "stdio" ? "none" : d.auth,
    defaultMode: d.defaultMode,
    transport: d.transport,
    headers: d.headers.filter((h) => h.name.trim()),
    oauthClientId: d.oauthClientId,
    oauthClientSecret: d.oauthClientSecret,
    stdioCommand: d.stdioCommand,
    stdioArgs: d.stdioArgs.map((a) => a.trim()).filter(Boolean),
    stdioImage: d.stdioImage,
    env: d.env.filter((e) => e.name.trim()).map((e) => ({ name: e.name, value: e.value, secret: e.secret })),
    image: d.image,
    imageType: d.imageType,
    clearImage: d.clearImage,
  };
}

// withName is d renamed. A new preset's identifier follows its name for as
// long as nobody has typed a different one.
export function withName(d: ConnectorDraft, name: string, follow: boolean): ConnectorDraft {
  const identifier = follow && d.identifier === identOf(d.name) ? identOf(name) : d.identifier;
  return { ...d, name, identifier };
}

// patchAt is xs with the item at i merged with patch; the list is not changed.
export function patchAt<T extends object>(xs: T[], i: number, patch: Partial<T>): T[] {
  return xs.map((x, j) => (j === i ? { ...x, ...patch } : x));
}

// setAt is xs with the item at i replaced.
export function setAt<T>(xs: T[], i: number, v: T): T[] {
  return xs.map((x, j) => (j === i ? v : x));
}

// withImage is d with a picked image as its pending upload.
export function withImage(d: ConnectorDraft, image: Uint8Array, imageType: string, previewUrl: string): ConnectorDraft {
  return { ...d, image, imageType, clearImage: false, hasImage: true, previewUrl };
}

// withoutImage is d with its image marked for removal.
export function withoutImage(d: ConnectorDraft): ConnectorDraft {
  return { ...d, clearImage: true, image: undefined, hasImage: false, previewUrl: undefined };
}
