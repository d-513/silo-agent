import { ChevronRight, CircleAlert, HardDrive, Plus } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ui } from "../api";
import { Btn, btnClass } from "../Btn";
import { ErrorWell, SkeletonRows } from "../Field";
import { PageHead } from "../PageHead";
import { UI, type Drive, type DriveTemplate } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { fail } from "../errors";
import { DriveForm } from "./DriveForm";
import { DriveMark } from "./DriveMark";
import { DriveRow } from "./DriveRow";
import { Gallery } from "./Gallery";

const POPULAR = ["gdrive", "onedrive", "dropbox", "box", "pcloud", "nextcloud", "s3", "r2", "b2", "sftp"];

// A Bot's drives and the provider templates, shared by the pages of the
// Drives tab (each a route in router.tsx).
function useDrives(botId: string) {
  const navigate = useNavigate();
  const templatesQ = useQuery(UI.method.listDriveTemplates, {});
  // Poll quickly while something is connecting, slowly otherwise.
  const drivesQ = useQuery(
    UI.method.listDrives,
    { botId, draftId: "" },
    { refetchInterval: (q) => (q.state.data?.drives.some((d) => d.state === "mounting") ? 1500 : 6000) },
  );
  const templates = templatesQ.data?.templates ?? null;
  const drives = drivesQ.data?.drives ?? null;
  const failed = templatesQ.error ?? drivesQ.error;
  const refresh = useCallback(() => reload(UI.method.listDrives, { botId }), [botId]);
  // Leaving a form rereads the list: it may have added or changed a drive.
  const back = useCallback(() => {
    void refresh();
    void navigate({ to: "/bots/$botId/drives", params: { botId } });
  }, [botId, navigate, refresh]);
  const byKey = useMemo(() => new Map((templates ?? []).map((t) => [t.key, t])), [templates]);
  const taken = useMemo(() => new Set((drives ?? []).map((d) => d.name)), [drives]);
  return {
    templates,
    drives,
    bindOk: drivesQ.data?.bindOk ?? true,
    loadErr: failed ? fail(failed) : "",
    refresh,
    back,
    gallery: () => void navigate({ to: "/bots/$botId/drives/new", params: { botId } }),
    byKey,
    taken,
  };
}

// /bots/$botId/drives/new: the provider gallery.
export function DriveNew({ botId, admin }: { botId: string; admin: boolean }) {
  const { templates, back } = useDrives(botId);
  return templates ? <Gallery botId={botId} templates={templates} admin={admin} onBack={back} /> : <Loading onBack={back} />;
}

// /bots/$botId/drives/new/$template: the form for a new drive.
export function DriveAdd({ botId, template }: { botId: string; template: string }) {
  const { templates, drives, byKey, taken, back } = useDrives(botId);
  const t = byKey.get(template);
  if (!templates || !drives) return <Loading onBack={back} />;
  if (!t) return <Missing onBack={back} />;
  return <DriveForm key={t.key} botId={botId} t={t} taken={taken} onDone={back} />;
}

// /bots/$botId/drives/$driveId: edit a drive.
export function DriveEdit({ botId, driveId }: { botId: string; driveId: string }) {
  const { templates, drives, byKey, taken, back } = useDrives(botId);
  if (!templates || !drives) return <Loading onBack={back} />;
  const d = drives.find((x) => x.id === driveId);
  const t = d && byKey.get(d.template);
  if (!d || !t) return <Missing onBack={back} />;
  return <DriveForm key={d.id} botId={botId} t={t} drive={d} taken={taken} onDone={back} />;
}

// /bots/$botId/drives: the Bot's drives.
export function DrivesList({ botId }: { botId: string }) {
  const navigate = useNavigate();
  const { templates, drives, bindOk, loadErr, refresh, gallery, byKey } = useDrives(botId);
  const [actErr, setErr] = useState("");
  const err = actErr || loadErr;

  async function reconnect(d: Drive) {
    const w = window.open("about:blank", "silo-drive-auth", "width=520,height=720");
    try {
      const r = await ui.beginDriveAuth({ id: d.id });
      if (w) w.location.href = r.url;
    } catch (e) {
      w?.close();
      setErr(fail(e));
    }
  }
  useEffect(() => {
    const onMsg = (e: MessageEvent) => {
      if ((e.data as { silo?: string })?.silo === "drive-auth") void refresh();
    };
    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, [refresh]);

  return (
    <div className="silo-page">
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 className="text-title text-ink">Drives</h2>
          <p className="mt-1 max-w-[520px] text-[13.5px] leading-[21px] text-ink-2">
            Cloud storage and file servers, mounted as folders in <code className="font-mono text-[12.5px] text-ink">/workspace/drives</code>. The Bot works on them like any file. Sign-ins and passwords stay on the Silo server.
          </p>
        </div>
        {drives && drives.length > 0 ? (
          <button type="button" className={btnClass("primary")} onClick={gallery}>
            <Plus size={15} />
            Add drive
          </button>
        ) : null}
      </div>

      {err ? (
        <div className="mb-4">
          <ErrorWell>{err}</ErrorWell>
        </div>
      ) : null}

      {!bindOk ? (
        <div className="mb-4 flex flex-wrap items-center gap-3 rounded-card bg-well px-4 py-3">
          <CircleAlert size={16} className="shrink-0 text-vermilion" />
          <p className="min-w-0 flex-1 text-[13px] leading-5 text-ink">This Bot’s container was made before it had drives. Reset it once so the Bot can see them. Its files are kept.</p>
          <Btn kind="secondary" size="sm" type="button" onClick={() => void navigate({ to: "/bots/$botId/container", params: { botId } })}>
            Go to Containers
          </Btn>
        </div>
      ) : null}

      {drives === null || templates === null ? (
        <SkeletonRows rows={2} height={92} />
      ) : drives.length === 0 ? (
        <div className="rounded-card bg-surface p-6 shadow-card sm:p-8">
          <div className="mb-5 flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-sm bg-well text-ink-2">
              <HardDrive size={22} />
            </div>
            <div>
              <h3 className="text-card-title text-ink">No drives yet</h3>
              <p className="text-[13px] text-ink-2">Give the Bot a folder from somewhere else.</p>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
            {POPULAR.map((k) => byKey.get(k))
              .filter((t): t is DriveTemplate => !!t)
              .map((t) => (
                <button
                  key={t.key}
                  type="button"
                  className="flex flex-col items-center gap-2 rounded-control bg-well px-2 pb-2.5 pt-3.5 text-center transition-[background-color,transform] duration-[160ms] ease-quiet hover:bg-pressed active:scale-[.97]"
                  onClick={() => (t.available ? void navigate({ to: "/bots/$botId/drives/new/$template", params: { botId, template: t.key } }) : gallery())}
                >
                  <DriveMark svg={t.iconSvg} size={36} className="bg-surface" muted={!t.available} />
                  <span className={`w-full truncate text-[12.5px] font-medium ${t.available ? "text-ink" : "text-ink-3"}`}>{t.title}</span>
                </button>
              ))}
          </div>
          <button type="button" className="mt-4 flex items-center gap-1 text-[13px] font-medium text-cobalt hover:text-cobalt-deep" onClick={gallery}>
            All {templates.length} providers <ChevronRight size={14} />
          </button>
        </div>
      ) : (
        <ul className="space-y-2.5">
          {drives.map((d) => (
            <DriveRow key={d.id} botId={botId} d={d} t={byKey.get(d.template)} onReconnect={() => void reconnect(d)} onRemoved={() => void refresh()} />
          ))}
        </ul>
      )}
    </div>
  );
}

function Loading({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <PageHead title="Drives" onBack={onBack} />
      <SkeletonRows rows={3} height={64} />
    </div>
  );
}

function Missing({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <PageHead title="Drives" onBack={onBack} />
      <p className="text-[13.5px] text-ink-2">That drive is gone. It may have been removed.</p>
    </div>
  );
}
