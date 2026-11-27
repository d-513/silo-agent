// The model providers Silo speaks to, read at build time from the control
// plane's own registry (internal/llm/llm.go, `builtinProviders`), so the
// landing page never names a provider the code does not have or misses one
// it does. The build fails if the registry cannot be read: a section that
// quietly showed nothing would be worse.
import source from "../../../internal/llm/llm.go?raw";
import { PROVIDER_ICONS, type ProviderIcon } from "../../../web/src/admin/providerIcons.ts";

export const KINDS = [
  { id: "text", word: "Text", says: "Chat and tool calls" },
  { id: "decision", word: "Decisions", says: "Typed answers with probabilities" },
  { id: "transcription", word: "Speech to text", says: "Dictation, voice memos, calls" },
  { id: "speech", word: "Speech", says: "Read aloud and calls" },
  { id: "embedding", word: "Embeddings", says: "Memory and Knowledge search" },
  { id: "image", word: "Images", says: "Generate and edit pictures" },
] as const;

export type KindID = (typeof KINDS)[number]["id"];

export type Provider = {
  id: string;
  name: string;
  /** The registry's description, written for the operator. */
  description: string;
  kinds: KindID[];
  live: KindID[];
  fast: boolean;
  signIn: boolean;
  icon?: ProviderIcon;
};

// `KindText` → "text": the Go constants are the kind's id, capitalised.
const kindOf = (name: string) => name.replace(/^Kind/, "").toLowerCase() as KindID;
const list = (block: string, key: string) =>
  (block.match(new RegExp(`\\b${key}:\\s*\\[\\]string\\{([^}]*)\\}`))?.[1] ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map(kindOf);

function read(): Provider[] {
  const ids = new Map([...source.matchAll(/^\s*(\w+)\s*=\s*"([a-z0-9]+)"\s*$/gm)].map((m) => [m[1], m[2]]));
  const known = new Set<string>(KINDS.map((k) => k.id));
  const out = source
    .split("desc: Descriptor{")
    .slice(1)
    .map((block) => {
      const ref = block.match(/\bID:\s*(\w+)/)?.[1] ?? "";
      const id = ids.get(ref) ?? "";
      const name = block.match(/\bName:\s*"([^"]+)"/)?.[1] ?? "";
      const description = block.match(/\bDescription:\s*"((?:[^"\\]|\\.)*)"/)?.[1] ?? "";
      // An empty Kinds means text only (Descriptor.Kinds).
      const kinds = list(block, "Kinds");
      const p: Provider = {
        id,
        name,
        description,
        kinds: kinds.length ? kinds : ["text"],
        live: list(block, "Live"),
        fast: /\bFast:\s*true/.test(block),
        signIn: /\bSignIn:\s*true/.test(block),
        icon: PROVIDER_ICONS[id],
      };
      if (!id || !name || [...p.kinds, ...p.live].some((k) => !known.has(k))) {
        throw new Error(`site: could not read a provider from internal/llm/llm.go (ID ${ref || "?"}): the registry's shape changed`);
      }
      return p;
    });
  if (out.length < 10) throw new Error(`site: read only ${out.length} providers from internal/llm/llm.go`);
  return out;
}

export const providers = read();
export const byID = new Map(providers.map((p) => [p.id, p]));
