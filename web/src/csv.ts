// Reading a CSV or TSV for the table preview: quoted fields, doubled quotes
// and line breaks inside quotes, then sorting and filtering its rows. No React,
// so node tests it.

export function guessDelim(text: string): string {
  const head = text.slice(0, 2000).split(/\r?\n/)[0] ?? "";
  const counts = [",", "\t", ";", "|"].map((d) => [d, head.split(d).length - 1] as const);
  counts.sort((a, b) => b[1] - a[1]);
  return counts[0][1] > 0 ? counts[0][0] : ",";
}

export function parseCsv(text: string, delim = guessDelim(text), maxRows = 5000): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          cell += '"';
          i++;
        } else quoted = false;
      } else cell += c;
      continue;
    }
    if (c === '"' && cell === "") quoted = true;
    else if (c === delim) {
      row.push(cell);
      cell = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && text[i + 1] === "\n") i++;
      row.push(cell);
      cell = "";
      if (row.length > 1 || row[0] !== "") rows.push(row);
      row = [];
      if (rows.length >= maxRows) return rows;
    } else cell += c;
  }
  if (cell !== "" || row.length) {
    row.push(cell);
    rows.push(row);
  }
  return rows;
}

const numeric = (s: string) => /^-?[\d.,\s]+%?$/.test(s.trim()) && !Number.isNaN(Number(s.replace(/[,\s%]/g, "")));

// sortRows orders rows by a column: numbers as numbers when the whole column
// reads as numbers, else text in the reader's locale.
export function sortRows(rows: string[][], col: number, dir: 1 | -1): string[][] {
  const vals = rows.map((r) => r[col] ?? "").filter((v) => v.trim());
  const nums = vals.length > 0 && vals.every(numeric);
  const key = (r: string[]) => r[col] ?? "";
  return [...rows].sort((a, b) => {
    const x = key(a);
    const y = key(b);
    const c = nums ? Number(x.replace(/[,\s%]/g, "")) - Number(y.replace(/[,\s%]/g, "")) : x.localeCompare(y, undefined, { numeric: true, sensitivity: "base" });
    return c * dir;
  });
}

export function filterRows(rows: string[][], q: string): string[][] {
  const needle = q.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((r) => r.some((c) => c.toLowerCase().includes(needle)));
}
