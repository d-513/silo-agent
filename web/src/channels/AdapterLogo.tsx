import { Radio } from "lucide-react";
import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";

// An adapter's logo is its own round badge (every adapter's SVG is a filled
// circle), so it stands on the page with no tile behind it: a square fill
// would show as corners around the circle.
export function AdapterLogo({ adapter, size = 44 }: { adapter?: ChannelAdapter; size?: number }) {
  if (adapter?.logo) {
    return <img src={adapter.logo} alt="" className="shrink-0 rounded-full object-cover" style={{ width: size, height: size }} />;
  }
  return (
    <div
      className="flex shrink-0 items-center justify-center rounded-sm bg-well text-ink-2"
      style={{ width: size, height: size }}
    >
      <Radio size={Math.round(size * 0.5)} />
    </div>
  );
}
