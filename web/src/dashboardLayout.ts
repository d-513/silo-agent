import type { DashboardCard } from "./gen/silo/v1/ui_pb";

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
    };

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

export function hasFormControls(blocks: DashboardBlock[]): boolean {
  return blocks.some((b) =>
    b.type === "switch" ||
    b.type === "select" ||
    b.type === "segmented" ||
    b.type === "checkboxes" ||
    b.type === "input" ||
    b.type === "textarea",
  );
}

export interface CardFootprint {
  preferredCols: 1 | 2 | 3;
  estimatedUnits: number;
}

// Deterministic footprint estimation for a card based on its blocks, summary,
// and priority so the 2D Bento packer can balance row widths and column heights
// without layout thrashing.
export function estimateCardFootprint(card: DashboardCard, blocks: DashboardBlock[]): CardFootprint {
  let units = 3; // header + footer baseline
  let wantsWide = false;
  let wantsFull = false;

  if (card.summary) {
    units += Math.min(4, Math.ceil(card.summary.length / 90));
  }

  for (const b of blocks) {
    switch (b.type) {
      case "markdown": {
        const text = b.text ?? "";
        const lines = text.split("\n").length;
        units += Math.min(8, Math.max(1, Math.ceil(text.length / 110), Math.ceil(lines * 0.8)));
        if (text.length > 260 || lines > 6 || text.includes("```") || text.includes("| ---")) {
          wantsWide = true;
        }
        break;
      }
      case "kv": {
        const n = b.items?.length ?? 0;
        units += Math.max(1, Math.ceil(n * 0.9));
        break;
      }
      case "metrics": {
        const n = b.items?.length ?? 0;
        units += 2;
        if (n >= 3) wantsWide = true;
        if (n >= 5) wantsFull = true;
        break;
      }
      case "diff": {
        const patch = b.patch ?? b.after ?? "";
        const lines = patch.split("\n").length;
        units += Math.min(8, Math.max(3, Math.ceil(lines * 0.6)));
        wantsWide = true;
        break;
      }
      case "switch":
      case "select":
      case "segmented":
      case "input":
        units += 2;
        break;
      case "checkboxes":
        units += Math.max(2, Math.ceil((b.options?.length ?? 2) * 0.8));
        break;
      case "textarea":
        units += Math.max(3, b.rows ?? 3);
        wantsWide = true;
        break;
    }
  }

  let preferredCols: 1 | 2 | 3 = 1;
  if (wantsFull) {
    preferredCols = 3;
  } else if (wantsWide || (card.priority === 1 && blocks.length >= 2)) {
    preferredCols = 2;
  }

  return { preferredCols, estimatedUnits: units };
}

export interface PackedDashboardCard {
  card: DashboardCard;
  blocks: DashboardBlock[];
  colSpan: 1 | 2 | 3;
  estimatedUnits: number;
}

// 2D Bento packing algorithm:
// 1. Orders cards by urgency (priority 1 is urgent -> 2 -> 3) then freshness.
// 2. Packs them into rows of `columns` (1 on mobile, 2 on medium, 3 on wide),
//    expanding or pairing cards so no orphan gaps remain on a row when possible.
export function packDashboardCards(cards: DashboardCard[], columns: 1 | 2 | 3): PackedDashboardCard[] {
  const prepared = cards
    .map((card) => {
      const blocks = parseBlocks(card.blocksJson);
      const fp = estimateCardFootprint(card, blocks);
      return {
        card,
        blocks,
        colSpan: Math.min(columns, fp.preferredCols) as 1 | 2 | 3,
        estimatedUnits: fp.estimatedUnits,
      };
    })
    .sort((a, b) => {
      const pa = a.card.priority || 2;
      const pb = b.card.priority || 2;
      if (pa !== pb) return pa - pb;
      return (b.card.updatedAt || "").localeCompare(a.card.updatedAt || "");
    });

  if (columns === 1) {
    return prepared.map((item) => ({ ...item, colSpan: 1 }));
  }

  const out: PackedDashboardCard[] = [];
  const pool = [...prepared];

  while (pool.length > 0) {
    let remaining = columns;
    const rowIndices: number[] = [];

    for (let i = 0; i < pool.length && remaining > 0; i++) {
      const item = pool[i];
      if (item.colSpan <= remaining) {
        rowIndices.push(i);
        remaining -= item.colSpan;
      }
    }

    // Take selected items in reverse index order from pool so indices stay valid.
    const rowItems = rowIndices.map((idx) => pool[idx]);
    for (let j = rowIndices.length - 1; j >= 0; j--) {
      pool.splice(rowIndices[j], 1);
    }

    // If the row has leftover columns (e.g. 1 or 2 unused columns on the last row,
    // or a 2-col card with no 1-col partner left), expand the widest/highest-priority
    // card in the row to fill the row cleanly.
    if (remaining > 0 && rowItems.length > 0) {
      const lead = rowItems[0];
      lead.colSpan = Math.min(columns, lead.colSpan + remaining) as 1 | 2 | 3;
    }

    out.push(...rowItems);
  }

  return out;
}
