import { msg } from "../i18n.ts";

// The categories of Admin → Settings, in the order the list shows them. Each
// is a page at /admin/settings/<id>, except Models, which opens on its jobs at
// /admin/settings/models and has its other pages under it (MODEL_PAGES). No
// React here: the router imports it too.
export const SECTIONS = [
  { id: "models", label: msg("Models"), note: msg("Which model does each job, who serves them, and what a chat may pick.") },
  { id: "search", label: msg("Search"), note: msg("How a Bot searches the web and reads the pages it finds.") },
  { id: "memory", label: msg("Memory & knowledge"), note: msg("What a Bot keeps across conversations, and the folders it can search.") },
  { id: "runs", label: msg("Context & runs"), note: msg("How long a conversation and a reply may grow, and what is kept of a Bot's work.") },
  { id: "voice", label: msg("Voice & calls"), note: msg("Dictation, Read aloud and calls, and the network a call's audio travels on.") },
  { id: "rules", label: msg("Rules"), note: msg("The operator's layer of the rules: what every Bot obeys, whatever its owner set.") },
  { id: "connectors", label: msg("Connector options"), note: msg("What connectors may reach, the variables they can use, and the ones every new Bot starts with.") },
  { id: "tunnels", label: msg("Tunnels & apps"), note: msg("The domain a Bot's tunnels, live apps and shared files are served on.") },
  { id: "mail", label: msg("Mail"), note: msg("The address every Bot receives mail at.") },
  { id: "network", label: msg("Network"), note: msg("The filter between every Bot's machine and the internet.") },
  { id: "https", label: msg("HTTPS"), note: msg("Certificates for Silo's own address and the content domain, through Caddy.") },
  { id: "signin", label: msg("Sign-in"), note: msg("How people prove who they are: passwords, a single sign-on provider, and the first admin.") },
  { id: "server", label: msg("Server"), note: msg("Where Silo listens and keeps its data, and the containers it starts.") },
  // The file itself is not the form.
  { id: "yaml", label: "silo.yaml", note: msg("Everything the other pages edit, as one file.") },
] as const;

export type SectionId = (typeof SECTIONS)[number]["id"];

// The pages of Models: its jobs (the map) at /admin/settings/models itself,
// then these at /admin/settings/models/<id>.
export const MODEL_PAGES = [
  { id: "providers", label: msg("Providers") },
  { id: "allowed", label: msg("Allowed models") },
] as const;

export type ModelPage = (typeof MODEL_PAGES)[number]["id"];

export function isModelPage(s: string | undefined): s is ModelPage {
  return MODEL_PAGES.some((x) => x.id === s);
}

// A page per kind of model stood here before the jobs moved onto the map:
// each address now opens that kind's allowlist. Voice became a category.
export const OLD_KIND_PAGES: Record<string, string> = { text: "text", decision: "decision", embedding: "embedding", image: "image" };

/** Where Settings opens, and where an unknown section lands. */
export const FIRST_SECTION: SectionId = "models";

export function isSection(s: string | undefined): s is SectionId {
  return SECTIONS.some((x) => x.id === s);
}

/** The sections the form's Save bar covers: every one that edits fields. */
export function isFormSection(s: SectionId): boolean {
  return s !== "yaml";
}
