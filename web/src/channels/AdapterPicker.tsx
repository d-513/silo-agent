import { useNavigate } from "react-router-dom";
import { SkeletonRows } from "../Field";
import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { PageHead, widePage } from "../PageHead";
import { AdapterLogo } from "./AdapterLogo";

// /bots/:id/channels/new: the built-in adapters to start a channel from.
export function AdapterPicker({ botId, adapters, loaded, onBack }: { botId: string; adapters: ChannelAdapter[]; loaded: boolean; onBack: () => void }) {
  const navigate = useNavigate();
  return (
    <div className={widePage}>
      <PageHead title="Add a channel" subtitle="Choose a built-in adapter to connect with this Bot." onBack={onBack} />
      {!loaded ? (
        <SkeletonRows rows={2} height={72} />
      ) : adapters.length === 0 ? (
        <div className="rounded-card border border-dashed border-line bg-surface p-12 text-center text-ink-3">
          No channel adapters available on this system.
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
          {adapters.map((a) => (
            <button
              key={a.slug}
              type="button"
              className="group flex items-start gap-4 rounded-card shadow-card bg-surface p-5 text-left transition-[background-color,color,box-shadow] hover:shadow-float hover:shadow-xs"
              onClick={() => navigate(`/bots/${botId}/channels/new/${a.slug}`)}
            >
              <AdapterLogo adapter={a} size={44} />
              <div className="min-w-0 flex-1">
                <div className="font-semibold text-[15px] text-ink group-hover:text-ink transition-colors">
                  {a.name}
                </div>
                {a.description ? (
                  <div className="mt-1 text-xs text-ink-3 leading-relaxed line-clamp-2">{a.description}</div>
                ) : null}
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
