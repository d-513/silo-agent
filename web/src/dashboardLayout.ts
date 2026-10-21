export type DashboardOption = string | { value: string; label: string; hint?: string };

export type DashboardBlock =
  | {
      type: "markdown";
      text?: string;
    }
  | {
      type: "kv";
      items?: { label: string; value: string }[];
    }
  | {
      type: "metrics";
      items?: { label: string; value: string; delta?: string; tone?: "good" | "warn" | "bad" | "neutral" }[];
    }
  | {
      type: "diff";
      path?: string;
      patch?: string;
      before?: string;
      after?: string;
    }
  | {
      type: "switch";
      name: string;
      label: string;
      hint?: string;
      default?: boolean;
    }
  | {
      type: "select";
      name: string;
      label: string;
      hint?: string;
      options?: DashboardOption[];
      default?: string;
    }
  | {
      type: "segmented";
      name: string;
      label?: string;
      hint?: string;
      options?: DashboardOption[];
      default?: string;
    }
  | {
      type: "checkboxes";
      name: string;
      label?: string;
      hint?: string;
      options?: DashboardOption[];
      default?: string[];
    }
  | {
      type: "input";
      name: string;
      label?: string;
      hint?: string;
      placeholder?: string;
      default?: string;
    }
  | {
      type: "textarea";
      name: string;
      label?: string;
      hint?: string;
      placeholder?: string;
      rows?: number;
      default?: string;
    }
  | SetupBlock;

// A setup offer: something from Silo's catalog the Bot suggests adding, with
// a few plain fields filled in. The owner adds it with the normal form.
export type SetupBlock = {
  type: "setup";
  kind: "connector" | "channel" | "drive";
  target: string;
  label: string;
  why?: string;
  prefill?: Record<string, string>;
};

// formControls are the blocks that take an answer (a setup offer is answered
// on its own).
export function hasFormControls(blocks: DashboardBlock[]): boolean {
  return blocks.some((b) => ["switch", "select", "segmented", "checkboxes", "input", "textarea"].includes(b.type));
}

// setupState is what the owner did with each setup offer, by block index.
export function setupState(json: string): Record<number, "done" | "skipped"> {
  try {
    const raw = JSON.parse(json || "{}") as Record<string, string>;
    const out: Record<number, "done" | "skipped"> = {};
    for (const [k, v] of Object.entries(raw)) if (v === "done" || v === "skipped") out[Number(k)] = v;
    return out;
  } catch {
    return {};
  }
}

export function parseBlocks(blocksJson: string): DashboardBlock[] {
  if (!blocksJson || !blocksJson.trim()) return [];
  try {
    const raw = JSON.parse(blocksJson);
    if (!Array.isArray(raw)) return [];
    return raw.filter((b): b is DashboardBlock => !!b && typeof b === "object" && typeof b.type === "string");
  } catch {
    return [];
  }
}

export function normalizeOptions(opts: DashboardOption[] | undefined): { value: string; label: string; hint?: string }[] {
  if (!Array.isArray(opts)) return [];
  return opts
    .map((o) => {
      if (typeof o === "string") return { value: o, label: o };
      if (o && typeof o === "object" && typeof o.value === "string") {
        return { value: o.value, label: typeof o.label === "string" && o.label ? o.label : o.value, hint: o.hint };
      }
      return null;
    })
    .filter((x): x is { value: string; label: string; hint?: string } => x !== null);
}

export function defaultFormValues(blocks: DashboardBlock[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const b of blocks) {
    switch (b.type) {
      case "switch":
        if (b.name) out[b.name] = Boolean(b.default);
        break;
      case "select":
      case "segmented": {
        if (!b.name) break;
        const opts = normalizeOptions(b.options);
        out[b.name] = typeof b.default === "string" ? b.default : (opts[0]?.value ?? "");
        break;
      }
      case "checkboxes":
        if (b.name) out[b.name] = Array.isArray(b.default) ? b.default.map(String) : [];
        break;
      case "input":
      case "textarea":
        if (b.name) out[b.name] = typeof b.default === "string" ? b.default : "";
        break;
    }
  }
  return out;
}
