import { FolderOpen, Pencil, RefreshCw, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { ui } from "../api";
import { Btn } from "../Btn";
import { ArmedButton, CopyButton } from "../Feedback";
import { Lamp } from "../Lamp";
import { driveLook } from "../statusLook";
import { Tip } from "../Tip";
import type { Drive, DriveTemplate } from "../gen/silo/v1/ui_pb";
import { DriveMark } from "./DriveMark";

export function DriveRow({ botId, d, t, onReconnect, onRemoved }: { botId: string; d: Drive; t?: DriveTemplate; onReconnect: () => void; onRemoved: () => void }) {
  const navigate = useNavigate();
  const look = driveLook(d.state);
  const attention = look.lamp === "needs_you";
  return (
    <li
      className={`relative flex flex-col gap-3 overflow-hidden rounded-card bg-surface p-4 shadow-card transition-shadow duration-[160ms] ease-quiet hover:shadow-float sm:flex-row sm:items-center ${
        attention ? "before:absolute before:inset-y-0 before:left-0 before:w-[2px] before:bg-vermilion" : ""
      }`}
    >
      <div className="flex min-w-0 flex-1 items-start gap-3.5">
        <DriveMark svg={t?.iconSvg} size={40} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-card-title text-ink">{d.name}</span>
            <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{t?.title ?? d.template}</span>
            {d.readOnly ? <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">Read-only</span> : null}
          </div>
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5">
            <code className="truncate font-mono text-[12px] text-ink-2">{d.path}</code>
            <CopyButton text={d.path} title="Copy path" size={12} />
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-1.5 text-[12.5px]">
            <Lamp status={look.lamp} />
            <span className={`shrink-0 font-medium ${look.tone}`}>{look.word}</span>
            {d.account && d.state === "mounted" ? <span className="truncate text-ink-3">· {d.account}</span> : null}
            {d.stateDetail && d.state !== "mounted" ? (
              <Tip content={<p className="max-w-[320px] break-words font-mono text-[12px]">{d.stateDetail}</p>}>
                <span tabIndex={0} className="min-w-0 truncate text-ink-3 outline-none">
                  · {d.stateDetail}
                </span>
              </Tip>
            ) : null}
          </div>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1 self-end sm:self-center">
        {d.state === "needs_auth" ? (
          <Btn kind="primary" size="sm" type="button" onClick={onReconnect}>
            <RefreshCw size={13} className="breathe" />
            Reconnect
          </Btn>
        ) : d.state === "mounted" ? (
          <Btn kind="ghost" size="sm" type="button" onClick={() => navigate(`/bots/${botId}/files?open=${encodeURIComponent(`drives/${d.name}`)}`)}>
            <FolderOpen size={14} />
            Open in Files
          </Btn>
        ) : null}
        <Btn kind="ghost" size="sm" iconOnly type="button" aria-label={`Edit ${d.name}`} title="Edit" onClick={() => navigate(`/bots/${botId}/drives/${d.id}`)}>
          <Pencil size={14} />
        </Btn>
        <ArmedButton
          kind="ghost"
          size="sm"
          iconOnly
          title="Remove drive"
          armedLabel="Click again to remove"
          icon={<Trash2 size={14} />}
          onConfirm={async () => {
            await ui.deleteDrive({ id: d.id });
            onRemoved();
          }}
        />
      </div>
    </li>
  );
}
