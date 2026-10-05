import { CircleAlert, History, Pencil, Settings, Trash2 } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { ui } from "../api";
import { Btn } from "../Btn";
import { ArmedButton } from "../Feedback";
import type { Channel, ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { channelLook } from "../statusLook";
import { AdapterLogo } from "./AdapterLogo";

// One channel in the list: who it is, what it is bound to, its state, and the
// ways into it (setup, log, configure, delete).
export function ChannelCard({ botId, c, a, onLog, onDeleted }: { botId: string; c: Channel; a?: ChannelAdapter; onLog: () => void; onDeleted: () => void }) {
  const navigate = useNavigate();
  const needsSetup = !!a?.requiresTarget && !c.externalId;
  const look = channelLook(c.status);
  // A channel that needs you wears the 2px vermilion ribbon, like a Bot or a drive.
  const attention = needsSetup || look.lamp === "needs_you";
  return (
    <div
      className={`relative flex min-h-[140px] flex-col justify-between overflow-hidden rounded-card bg-surface p-5 shadow-card transition-shadow duration-[160ms] ease-quiet hover:shadow-float ${
        attention ? "before:absolute before:inset-y-0 before:left-0 before:w-[2px] before:bg-vermilion" : ""
      }`}
    >
      <div>
        <div className="flex items-start gap-4">
          <AdapterLogo adapter={a} size={48} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold text-[15px] text-ink tracking-tight">{c.name}</span>
              <span className="rounded-sm bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">
                {c.adapterName || c.adapter}
              </span>
              {!c.enabled && (
                <span className="rounded-sm bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">
                  Disabled
                </span>
              )}
              {!c.inbound && (
                <span className="rounded-sm bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">
                  Send-only
                </span>
              )}
            </div>

            {/* Target Info */}
            {a?.requiresTarget && (
              <div className="mt-1.5 text-xs">
                {c.externalId ? (
                  <span className="text-ink-3">
                    Chat: <span className="font-medium text-ink">{c.targetTitle || c.externalId}</span>
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1 font-semibold text-vermilion">
                    <CircleAlert size={13} /> Needs setup
                  </span>
                )}
              </div>
            )}

            {/* Status */}
            <div className="mt-2.5 flex items-center gap-1.5 text-[11px] font-medium text-ink-3">
              <Lamp status={look.lamp} />
              <span className={look.tone}>{look.word}</span>
              {c.statusDetail && (
                <span className="truncate text-ink-3">· {c.statusDetail}</span>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Bottom Actions Toolbar */}
      <div className="mt-4 flex items-center justify-between border-t border-line/80 pt-3.5">
        <div>
          {a?.actions?.length ? (
            needsSetup ? (
              <Btn
                kind="primary"
                type="button"
                onClick={() => void navigate({ to: "/bots/$botId/channels/$channelId/setup", params: { botId, channelId: c.id } })}
              >
                <Settings size={14} className="breathe" />
                <span>Set up</span>
              </Btn>
            ) : (
              <Btn
                kind="ghost"
                type="button"
                onClick={() => void navigate({ to: "/bots/$botId/channels/$channelId/setup", params: { botId, channelId: c.id } })}
              >
                <Settings size={14} />
                <span>Set up</span>
              </Btn>
            )
          ) : null}
        </div>

        <div className="flex items-center gap-1.5">
          <Btn
            kind="ghost"
            type="button"
            onClick={onLog}
            title="View conversation log"
          >
            <History size={14} />
            <span>Log</span>
          </Btn>

          <Btn
            kind="ghost"
            type="button"
            onClick={() => void navigate({ to: "/bots/$botId/channels/$channelId", params: { botId, channelId: c.id } })}
            title="Configure channel"
          >
            <Pencil size={14} />
            <span>Configure</span>
          </Btn>

          <ArmedButton
            kind="ghost"
            title="Delete channel"
            icon={<Trash2 size={14} />}
            onConfirm={async () => {
              await ui.deleteChannel({ botId, id: c.id });
              onDeleted();
            }}
          >
            Delete
          </ArmedButton>
        </div>
      </div>
    </div>
  );
}
