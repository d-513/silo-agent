import { useEffect, useState } from "react";
import { type Cols, columnsFor } from "./widgets";

// useColumns follows the page's own width, not the window's: a docked pane
// narrows the board. It watches the element it is given, once per element
// (the skeleton, the empty board and the board are different ones).
export function useColumns(el: HTMLElement | null): Cols {
  const [cols, setCols] = useState<Cols>(4);
  useEffect(() => {
    if (!el || typeof ResizeObserver === "undefined") return;
    const update = () => setCols(columnsFor(el.clientWidth));
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [el]);
  return cols;
}
