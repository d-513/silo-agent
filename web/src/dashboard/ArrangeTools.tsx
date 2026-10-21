import { X } from "lucide-react";
import { useArmed } from "../Feedback";
import type { DashboardWidget } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import type { Cols } from "./widgets";

// ArrangeTools replace a widget's corner while arranging: one chip for its
// width (a quarter, half, the row; a press moves to the next), one for one or
// two rows, and remove, which arms first.
export function ArrangeTools({ w, cols, onSize, onRemove }: { w: DashboardWidget; cols: Cols; onSize: (size: string, rows: number) => void; onRemove: () => void }) {
  const { armed, fire } = useArmed();
  const onHatch = w.kind === "app";
  const chip = `h-6 rounded-[6px] px-1.5 font-mono text-[11px] font-medium transition-colors duration-[160ms] ${
    onHatch ? "bg-on-hatch/10 text-on-hatch/80 hover:bg-on-hatch/20 hover:text-on-hatch" : "bg-well text-ink-2 hover:bg-pressed hover:text-ink"
  }`;
  const next = w.size === "s" ? "m" : w.size === "m" ? "l" : "s";
  const width = w.size === "s" ? "¼" : w.size === "m" ? "½" : "1/1";
  return (
    <div className="flex shrink-0 items-center gap-1">
      <button type="button" title={t("Width")} aria-label={t("Width")} onClick={() => onSize(next, w.rows)} className={chip}>
        {width}
      </button>
      {cols > 1 ? (
        <button type="button" title={t("Height")} aria-label={t("Height")} onClick={() => onSize(w.size, w.rows === 2 ? 1 : 2)} className={chip}>
          {w.rows === 2 ? "×2" : "×1"}
        </button>
      ) : null}
      <button
        type="button"
        title={armed ? t("Click again") : t("Remove")}
        aria-label={armed ? t("Click again") : t("Remove")}
        onClick={() => fire(onRemove)}
        className={`grid h-6 min-w-6 place-items-center rounded-[6px] px-1 text-[11px] font-medium transition-colors duration-[160ms] ${
          armed ? "bg-vermilion text-on-accent" : onHatch ? "text-on-hatch/80 hover:bg-on-hatch/10" : "text-ink-3 hover:bg-vermilion-pale hover:text-vermilion"
        }`}
      >
        {armed ? t("Remove") : <X size={13} />}
      </button>
    </div>
  );
}
