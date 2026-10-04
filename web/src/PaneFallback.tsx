import { SkeletonRows } from "./Field";

// What a tab shows for the moment its code is on the way.
export function PaneFallback() {
  return (
    <div className="silo-page" aria-busy="true">
      <SkeletonRows rows={3} height={56} />
    </div>
  );
}
