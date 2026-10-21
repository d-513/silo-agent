import { msg } from "../i18n.ts";

// What a Bot's setup offer may fill in besides the provider's own fields: the
// add form's options, per kind (the server's setup.options holds the same
// list). Every value arrives as a string; a switch is "true" or "false".
export const formOptions = {
  connector: ["name", "default_mode", "expose_completion"],
  channel: ["name", "inbound", "enabled", "prompt"],
  drive: ["name", "read_only"],
} as const;

export type OfferKind = keyof typeof formOptions;

// What the offer card calls each of the form's options.
export const optionWord: Record<string, string> = {
  name: msg("Name"),
  default_mode: msg("Default rule"),
  expose_completion: msg("Also chat tools"),
  inbound: msg("Deliver messages to the Bot"),
  enabled: msg("Enabled"),
  prompt: msg("Prompt"),
  read_only: msg("Read-only"),
};

// splitPrefill splits an offer's values into the form's own options (`own`)
// and the provider's fields (`fields`).
export function splitPrefill<K extends OfferKind>(kind: K, prefill: Record<string, string> | undefined) {
  const keys: readonly string[] = formOptions[kind];
  const own: Partial<Record<(typeof formOptions)[K][number], string>> = {};
  const fields: Record<string, string> = {};
  for (const [k, v] of Object.entries(prefill ?? {})) {
    if (keys.includes(k)) own[k as (typeof formOptions)[K][number]] = v;
    else fields[k] = v;
  }
  return { own, fields };
}
