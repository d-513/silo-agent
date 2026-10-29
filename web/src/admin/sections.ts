import { msg } from "../i18n.ts";

// The categories of Admin → Settings, in the order the list shows them. Each
// is a page at /admin/settings/<id>, except Models, which is the model map at
// /admin/settings/models with its own pages under it (MODEL_PAGES). No React
// here: the router imports it too.
export const SECTIONS = [
  { id: "models", label: msg("Models") },
  { id: "search", label: msg("Search") },
  { id: "memory", label: msg("Memory & knowledge") },
  { id: "runs", label: msg("Context & runs") },
  { id: "rules", label: msg("Rules") },
  { id: "connectors", label: msg("Connector options") },
  { id: "tunnels", label: msg("Tunnels & apps") },
  { id: "mail", label: msg("Mail") },
  { id: "network", label: msg("Network") },
  { id: "signin", label: msg("Sign-in") },
  { id: "server", label: msg("Server") },
  // The file itself is not the form.
  { id: "yaml", label: "silo.yaml" },
] as const;

export type SectionId = (typeof SECTIONS)[number]["id"];

// The pages under Models, each at /admin/settings/models/<id>: who serves
// models, then one page per kind of job with its pickers and allowlists.
export const MODEL_PAGES = [
  { id: "providers", label: msg("Providers") },
  { id: "text", label: msg("Text") },
  { id: "decision", label: msg("Decisions") },
  { id: "embedding", label: msg("Embeddings") },
  { id: "voice", label: msg("Voice") },
] as const;

export type ModelPage = (typeof MODEL_PAGES)[number]["id"];

export function isModelPage(s: string | undefined): s is ModelPage {
  return MODEL_PAGES.some((x) => x.id === s);
}

/** Where Settings opens, and where an unknown section lands. */
export const FIRST_SECTION: SectionId = "models";

export function isSection(s: string | undefined): s is SectionId {
  return SECTIONS.some((x) => x.id === s);
}

/** The sections the form's Save bar covers: every one that edits fields. */
export function isFormSection(s: SectionId): boolean {
  return s !== "yaml";
}
