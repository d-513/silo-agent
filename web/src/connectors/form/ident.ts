// A library connector's identifier, the name an operator writes in
// autoenable_connectors: lowercase letters and digits, with every run of
// anything else as one underscore ("fal.ai" is fal_ai). The server normalises
// the same way (catalog.Identifier) and has the last word.

/** The identifier as it is typed: a trailing separator stays, so the next word can follow it. */
export function identTyped(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_/, "");
}

/** The identifier a name or a typed value settles to. */
export function identOf(s: string): string {
  return identTyped(s).replace(/_$/, "");
}
