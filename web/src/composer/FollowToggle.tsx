import { FoldVertical, UnfoldVertical } from "lucide-react";
import { setAutoExpand, useAutoExpand } from "../autoExpand";
import { Tip, TipAction, TipTitle } from "../Tip";

// Whether thinking and tool rows open while they run (and fold shut after).
export function FollowToggle() {
  const autoExpand = useAutoExpand();
  return (
    <Tip
      closeOnClick={false}
      content={
        <>
          <TipTitle aside={autoExpand ? "On" : "Off"}>Follow live steps</TipTitle>
          <p className="mt-1">
            {autoExpand
              ? "Thinking and tool rows open while they run and fold shut when they finish."
              : "Thinking and tool rows stay folded. Click a row to open it."}
          </p>
          <TipAction>Click to turn {autoExpand ? "off" : "on"}</TipAction>
        </>
      }
    >
      <button
        type="button"
        aria-label="Open thinking and tool rows while they run"
        aria-pressed={autoExpand}
        className={`flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-control transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well active:scale-[.94] active:duration-[70ms] ${
          autoExpand ? "text-ink" : "text-ink-3 hover:text-ink"
        }`}
        onClick={() => setAutoExpand(!autoExpand)}
      >
        {autoExpand ? <UnfoldVertical size={16} /> : <FoldVertical size={16} />}
      </button>
    </Tip>
  );
}
