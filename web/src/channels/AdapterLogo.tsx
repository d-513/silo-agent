import { Radio } from "lucide-react";
import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";

export function AdapterLogo({ adapter, size = 44 }: { adapter?: ChannelAdapter; size?: number }) {
  if (adapter?.logo) {
    return (
      <div
        className="flex shrink-0 items-center justify-center overflow-hidden rounded-sm bg-well"
        style={{ width: size, height: size }}
      >
        <img src={adapter.logo} alt="" className="h-full w-full object-cover" />
      </div>
    );
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
