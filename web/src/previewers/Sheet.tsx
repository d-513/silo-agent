import { useMemo, useState } from "react";
import * as XLSX from "xlsx";
import { t } from "../i18n.ts";

const MAX_ROWS = 1000;
const MAX_COLS = 60;

// A workbook: one tab per sheet, each a table of the cells as the sheet shows
// them (formatted values, not formulas).
export default function Sheet({ data }: { data: Uint8Array }) {
  const book = useMemo(() => {
    try {
      return XLSX.read(data, { type: "array", cellDates: true, sheetRows: MAX_ROWS + 1, dense: true });
    } catch {
      return null;
    }
  }, [data]);
  const [tab, setTab] = useState(0);
  if (!book || book.SheetNames.length === 0) return <p className="text-ink-2">{t("This workbook could not be read. Download it instead.")}</p>;
  const name = book.SheetNames[Math.min(tab, book.SheetNames.length - 1)];
  const rows = XLSX.utils.sheet_to_json<string[]>(book.Sheets[name], { header: 1, raw: false, defval: "", blankrows: false });
  const width = Math.min(MAX_COLS, rows.reduce((n, r) => Math.max(n, r.length), 0));
  return (
    <div className="space-y-2">
      {book.SheetNames.length > 1 ? (
        <div className="flex flex-wrap gap-1">
          {book.SheetNames.map((s, i) => (
            <button
              key={s}
              type="button"
              onClick={() => setTab(i)}
              className={`h-7 rounded-control px-2.5 text-[12.5px] font-medium transition-colors duration-[160ms] ${i === tab ? "bg-ink text-on-ink" : "bg-well text-ink-2 hover:text-ink"}`}
            >
              {s}
            </button>
          ))}
        </div>
      ) : null}
      {rows.length === 0 ? (
        <p className="text-ink-2">{t("This sheet is empty.")}</p>
      ) : (
        <div className="silo-md-table">
          <table className="text-[12.5px]">
            <thead>
              <tr>
                <th className="text-ink-3">#</th>
                {Array.from({ length: width }, (_, i) => (
                  <th key={i} className="font-mono text-ink-3">
                    {XLSX.utils.encode_col(i)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.slice(0, MAX_ROWS).map((r, i) => (
                <tr key={i}>
                  <td className="font-mono text-ink-3">{i + 1}</td>
                  {Array.from({ length: width }, (_, j) => (
                    <td key={j}>{r[j] ?? ""}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > MAX_ROWS ? <p className="text-[12.5px] text-ink-3">{t("Showing the first {{n}} rows.", { n: MAX_ROWS })}</p> : null}
    </div>
  );
}
