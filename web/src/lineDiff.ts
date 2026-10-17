// lineDiff is what changed between two versions of a text file, as the lines
// of a unified diff ("+" added, "-" removed, " " kept), with long runs of kept
// lines folded to a few lines of context. No React, so node tests it.

export type DiffStat = { added: number; removed: number };

const CONTEXT = 3;
// Past this many line pairs the table is too big to fill: show it all replaced.
const MAX_CELLS = 4_000_000;

export function lineDiff(before: string, after: string): { text: string; stat: DiffStat } {
  const a = split(before);
  const b = split(after);
  let ops: string[];
  if (a.length * b.length > MAX_CELLS) {
    ops = [...a.map((l) => `-${l}`), ...b.map((l) => `+${l}`)];
  } else {
    ops = lcsOps(a, b);
  }
  const stat = { added: ops.filter((o) => o[0] === "+").length, removed: ops.filter((o) => o[0] === "-").length };
  return { text: fold(ops).join("\n"), stat };
}

function split(s: string): string[] {
  if (!s) return [];
  const lines = s.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

// lcsOps walks the longest-common-subsequence table back into edit lines.
function lcsOps(a: string[], b: string[]): string[] {
  // Common head and tail first: most edits touch a small middle.
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const x = a.slice(head, a.length - tail);
  const y = b.slice(head, b.length - tail);
  const n = x.length;
  const m = y.length;
  const t = new Uint32Array((n + 1) * (m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      t[i * (m + 1) + j] = x[i] === y[j] ? t[(i + 1) * (m + 1) + j + 1] + 1 : Math.max(t[(i + 1) * (m + 1) + j], t[i * (m + 1) + j + 1]);
    }
  }
  const mid: string[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      mid.push(` ${x[i]}`);
      i++;
      j++;
    } else if (t[(i + 1) * (m + 1) + j] >= t[i * (m + 1) + j + 1]) {
      mid.push(`-${x[i++]}`);
    } else {
      mid.push(`+${y[j++]}`);
    }
  }
  while (i < n) mid.push(`-${x[i++]}`);
  while (j < m) mid.push(`+${y[j++]}`);
  return [...a.slice(0, head).map((l) => ` ${l}`), ...mid, ...a.slice(a.length - tail).map((l) => ` ${l}`)];
}

// fold keeps CONTEXT kept lines around each change and marks what it skips.
function fold(ops: string[]): string[] {
  const near = new Array(ops.length).fill(false);
  ops.forEach((o, i) => {
    if (o[0] === " ") return;
    for (let k = Math.max(0, i - CONTEXT); k <= Math.min(ops.length - 1, i + CONTEXT); k++) near[k] = true;
  });
  const out: string[] = [];
  let skipped = 0;
  ops.forEach((o, i) => {
    if (near[i]) {
      if (skipped) out.push(`@@ ${skipped} @@`);
      skipped = 0;
      out.push(o);
    } else skipped++;
  });
  if (skipped && out.length) out.push(`@@ ${skipped} @@`);
  return out;
}
