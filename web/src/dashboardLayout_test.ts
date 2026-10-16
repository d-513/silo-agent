import assert from "node:assert/strict";
import type { DashboardCard } from "./gen/silo/v1/ui_pb.ts";
import { defaultFormValues, estimateCardFootprint, packDashboardCards, parseBlocks } from "./dashboardLayout.ts";

function makeCard(partial: Partial<DashboardCard>): DashboardCard {
  return {
    $typeName: "silo.v1.DashboardCard",
    id: "",
    botId: "b1",
    key: "",
    title: "",
    summary: "",
    priority: 2,
    blocksJson: "[]",
    submitLabel: "",
    draftState: "ready",
    draftContext: "",
    status: "open",
    sourceKind: "automation",
    sourceName: "Dashboard",
    chatId: "",
    createdAt: "2026-10-16T09:00:00Z",
    updatedAt: "2026-10-16T09:00:00Z",
    read: false,
    ...partial,
  };
}

const blocks = parseBlocks(
  JSON.stringify([
    { type: "metrics", items: [{ label: "Modified", value: "4", delta: "+2", tone: "warn" }] },
    { type: "switch", name: "clean_tmp", label: "Clean /tmp", default: true },
    { type: "select", name: "target", label: "Target", options: ["main", "staging"], default: "staging" },
    { type: "checkboxes", name: "checks", options: ["lint", "test"], default: ["lint"] },
  ]),
);

assert.equal(blocks.length, 4);
const defaults = defaultFormValues(blocks);
assert.deepEqual(defaults, {
  clean_tmp: true,
  target: "staging",
  checks: ["lint"],
});

const c1 = makeCard({
  id: "c1",
  key: "normal_small",
  title: "Small normal card",
  priority: 2,
  updatedAt: "2026-10-16T10:00:00Z",
  blocksJson: JSON.stringify([{ type: "switch", name: "ok", label: "OK" }]),
});

const c2 = makeCard({
  id: "c2",
  key: "urgent_wide",
  title: "Urgent wide card",
  priority: 1,
  updatedAt: "2026-10-16T09:00:00Z",
  blocksJson: JSON.stringify([
    { type: "diff", path: "README.md", patch: "+ hello\n- world" },
    { type: "textarea", name: "notes", label: "Notes" },
  ]),
});

const c3 = makeCard({
  id: "c3",
  key: "partner_small",
  title: "Partner small card",
  priority: 2,
  updatedAt: "2026-10-16T11:00:00Z",
  blocksJson: JSON.stringify([{ type: "input", name: "tag", label: "Tag", default: "v1" }]),
});

assert.equal(estimateCardFootprint(c2, parseBlocks(c2.blocksJson)).preferredCols, 2);

// In a 3-column Bento grid:
// - c2 (priority 1, 2 cols) comes first and pairs with c3 (priority 2, newer than c1, 1 col) to fill row 1 (2 + 1 = 3 cols).
// - c1 is alone on row 2 and expands to fill all 3 columns so there is no orphan gap.
const packed3 = packDashboardCards([c1, c2, c3], 3);
assert.deepEqual(
  packed3.map((p) => ({ id: p.card.id, colSpan: p.colSpan })),
  [
    { id: "c2", colSpan: 2 },
    { id: "c3", colSpan: 1 },
    { id: "c1", colSpan: 3 },
  ],
);

console.log("dashboardLayout ok");
