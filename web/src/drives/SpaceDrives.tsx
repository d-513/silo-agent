import { useQuery } from "@connectrpc/connect-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { HardDrive, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ui } from "../api";
import { useAuth } from "../auth";
import { Btn, btnClass } from "../Btn";
import { BotsStep, ChooseBots, UsedBy, useManagedBots } from "../connectors/BotPick";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { UI, type Drive } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { PageHead } from "../PageHead";
import { reload } from "../query";
import { driveLook } from "../statusLook";
import { Trans } from "../Trans.tsx";
import { DriveForm } from "./DriveForm";
import { DriveMark } from "./DriveMark";
import { Gallery } from "./Gallery";

// The Workspace's drives and the provider templates, shared by the pages of
// the hub's Drives tab.
function useHubDrives(spaceId: string) {
  const navigate = useNavigate();
  const templatesQ = useQuery(UI.method.listDriveTemplates, {});
  const drivesQ = useQuery(
    UI.method.listSpaceDrives,
    { spaceId, draftId: "" },
    { refetchInterval: (q) => (q.state.data?.drives.some((d) => d.state === "mounting") ? 1500 : 6000) },
  );
  const templates = templatesQ.data?.templates ?? null;
  const drives = drivesQ.data?.drives ?? null;
  const failed = templatesQ.error ?? drivesQ.error;
  const refresh = useCallback(() => Promise.all([reload(UI.method.listSpaceDrives, { spaceId }), reload(UI.method.listDrives)]), [spaceId]);
  const back = useCallback(() => {
    void refresh();
    void navigate({ to: "/w/$spaceId/customize/drives", params: { spaceId } });
  }, [spaceId, navigate, refresh]);
  const byKey = useMemo(() => new Map((templates ?? []).map((x) => [x.key, x])), [templates]);
  const taken = useMemo(() => new Set((drives ?? []).map((d) => d.name)), [drives]);
  return { templates, drives, loadErr: failed ? fail(failed) : "", refresh, back, byKey, taken };
}

// /w/$spaceId/customize/drives: every drive of the Workspace and the Bots
// that mount it.
export function SpaceDrives() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/customize/drives/" });
  const navigate = useNavigate();
  const { templates, drives, loadErr, refresh, byKey } = useHubDrives(spaceId);
  const managed = useManagedBots(spaceId) ?? [];
  const [actErr, setErr] = useState("");
  const err = actErr || loadErr;
  const gallery = () => void navigate({ to: "/w/$spaceId/customize/drives/new", params: { spaceId } });

  async function act(fn: () => Promise<unknown>) {
    setErr("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setErr(fail(e));
    }
  }

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
          <h2 className="text-title text-ink">{t("Drives")}</h2>
          <p className="mt-1 max-w-[520px] text-[13.5px] leading-[21px] text-ink-2">
            <Trans
              i18nKey="Connect cloud storage or a file server once for the whole workspace, then choose which Bots mount it in <code>/workspace/drives</code>. Sign-ins and passwords stay on the Silo server."
              components={{ code: <code className="font-mono text-[12.5px] text-ink" /> }}
            />
          </p>
        </div>
        {drives && drives.length > 0 ? (
          <button type="button" className={btnClass("primary")} onClick={gallery}>
            <Plus size={15} />
            {t("Add drive")}
          </button>
        ) : null}
      </div>

      {err ? (
        <div className="mb-4">
          <ErrorWell>{err}</ErrorWell>
        </div>
      ) : null}

      {drives === null || templates === null ? (
        <SkeletonRows rows={2} height={92} />
      ) : drives.length === 0 ? (
        <div className="rounded-card border border-dashed border-line bg-surface p-12 text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-well text-ink-3">
            <HardDrive size={28} />
          </div>
          <h3 className="text-base font-semibold text-ink">{t("No drives in this workspace yet")}</h3>
          <p className="mx-auto mt-1 max-w-md text-[13px] text-ink-2">{t("Add one, then choose which Bots mount it.")}</p>
          <button type="button" className={`${btnClass("primary")} mt-6`} onClick={gallery}>
            <Plus size={15} />
            {t("Add drive")}
          </button>
        </div>
      ) : (
        <ul className="space-y-2.5">
          {drives.map((d) => {
            const tpl = byKey.get(d.template);
            const look = driveLook(d.state);
            return (
              <li key={d.id} className="flex flex-col gap-3 rounded-card bg-surface p-4 shadow-card sm:flex-row sm:items-center">
                <div className="flex min-w-0 flex-1 items-start gap-3.5">
                  <DriveMark svg={tpl?.iconSvg} size={40} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-card-title text-ink">{d.name}</span>
                      <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{tpl?.title ?? d.template}</span>
                      {d.readOnly ? <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{t("Read-only")}</span> : null}
                    </div>
                    <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-1.5 text-[12.5px]">
                      <Lamp status={look.lamp} />
                      <span className={`shrink-0 font-medium ${look.tone}`}>{look.word}</span>
                      {d.account ? <span className="truncate text-ink-3">· {d.account}</span> : null}
                      {d.lentBy ? <span className="truncate text-ink-3">· {t("Signed in by {{who}}", { who: d.lentBy })}</span> : null}
                    </div>
                    <div className="mt-2">
                      <UsedBy ids={d.usedBy} hidden={d.usedByHidden} />
                    </div>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1 self-end sm:self-center">
                  {d.state === "needs_auth" && d.canEdit ? (
                    <Btn kind="primary" size="sm" type="button" onClick={() => void reconnect(d)}>
                      <RefreshCw size={13} className="breathe" />
                      {t("Reconnect")}
                    </Btn>
                  ) : null}
                  <ChooseBots
                    bots={managed}
                    granted={d.usedBy}
                    onToggle={(botId, on) => act(() => (on ? ui.grantDrive({ botId, driveId: d.id }) : ui.revokeDrive({ botId, driveId: d.id })))}
                  />
                  {d.canEdit ? (
                    <>
                      <Btn
                        kind="ghost"
                        size="sm"
                        iconOnly
                        type="button"
                        aria-label={t("Edit {{name}}", { name: d.name })}
                        title={t("Edit")}
                        onClick={() => void navigate({ to: "/w/$spaceId/customize/drives/$driveId", params: { spaceId, driveId: d.id } })}
                      >
                        <Pencil size={14} />
                      </Btn>
                      <ArmedButton
                        kind="ghost"
                        size="sm"
                        iconOnly
                        title={t("Delete drive")}
                        armedLabel={d.usedBy.length + d.usedByHidden > 0 ? t("Unmount from every Bot and delete") : t("Click again to delete")}
                        icon={<Trash2 size={14} />}
                        onConfirm={() => act(() => ui.deleteDrive({ id: d.id }))}
                      />
                    </>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

// /w/$spaceId/customize/drives/new: the provider gallery.
export function SpaceDriveNew() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/customize/drives/new" });
  const navigate = useNavigate();
  const { admin } = useAuth();
  const { templates, back } = useHubDrives(spaceId);
  return templates ? (
    <Gallery templates={templates} admin={admin} onPick={(key) => void navigate({ to: "/w/$spaceId/customize/drives/new/$template", params: { spaceId, template: key } })} onBack={back} />
  ) : (
    <Loading onBack={back} />
  );
}

// /w/$spaceId/customize/drives/new/$template: a new drive for the Workspace,
// mounted in the Bots picked at its foot.
export function SpaceDriveAdd() {
  const { spaceId, template } = useParams({ from: "/_authed/w/$spaceId/customize/drives/new/$template" });
  const { templates, drives, byKey, taken, back } = useHubDrives(spaceId);
  const managed = useManagedBots(spaceId);
  const [chosen, setChosen] = useState<string[] | null>(null);
  const picked = chosen ?? (managed ?? []).map((b) => b.id);
  const tpl = byKey.get(template);
  if (!templates || !drives || !managed) return <Loading onBack={back} />;
  if (!tpl) return <Missing onBack={back} />;
  return (
    <DriveForm
      key={tpl.key}
      botId=""
      spaceId={spaceId}
      botIds={picked}
      t={tpl}
      taken={taken}
      onDone={back}
      extra={<BotsStep bots={managed} value={picked} onChange={setChosen} />}
    />
  );
}

// /w/$spaceId/customize/drives/$driveId: edit a Workspace drive.
export function SpaceDriveEdit() {
  const { spaceId, driveId } = useParams({ from: "/_authed/w/$spaceId/customize/drives/$driveId" });
  const { templates, drives, byKey, back } = useHubDrives(spaceId);
  if (!templates || !drives) return <Loading onBack={back} />;
  const d = drives.find((x) => x.id === driveId);
  const tpl = d && byKey.get(d.template);
  if (!d || !tpl) return <Missing onBack={back} />;
  const taken = new Set(drives.filter((x) => x.id !== d.id).map((x) => x.name));
  return <DriveForm key={d.id} botId="" spaceId={spaceId} t={tpl} drive={d} taken={taken} onDone={back} />;
}

function Loading({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <PageHead title={t("Drives")} onBack={onBack} />
      <SkeletonRows rows={3} height={64} />
    </div>
  );
}

function Missing({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <PageHead title={t("Drives")} onBack={onBack} />
      <p className="text-[13.5px] text-ink-2">{t("That drive is gone. It may have been removed.")}</p>
    </div>
  );
}
