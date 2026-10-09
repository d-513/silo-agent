// The categories of Admin → Settings, in the order the list shows them. Each
// is a page at /admin/settings/<id>. No React here: the router imports it too.
export const SECTIONS = [
  { id: "models", label: "Models" },
  { id: "providers", label: "Providers" },
  { id: "search", label: "Search & extract" },
  { id: "memory", label: "Memory & knowledge" },
  { id: "runs", label: "Context & runs" },
  { id: "connectors", label: "Connector options" },
  { id: "tunnels", label: "Tunnels" },
  { id: "mail", label: "Mail" },
  { id: "signin", label: "Sign-in" },
  { id: "server", label: "Server" },
  // The two below are not the form: the file itself, and the decision log.
  { id: "yaml", label: "silo.yaml" },
  { id: "audit", label: "Audit" },
] as const;

export type SectionId = (typeof SECTIONS)[number]["id"];

/** Where Settings opens, and where an unknown section lands. */
export const FIRST_SECTION: SectionId = "models";

export function isSection(s: string | undefined): s is SectionId {
  return SECTIONS.some((x) => x.id === s);
}

/** The sections the form's Save bar covers: every one that edits fields. */
export function isFormSection(s: SectionId): boolean {
  return s !== "yaml" && s !== "audit";
}
