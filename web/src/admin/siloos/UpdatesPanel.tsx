import { useQuery } from "@connectrpc/connect-query";
import { Download, RefreshCw, RotateCcw, Undo2 } from "lucide-react";
import { useState } from "react";
import { Btn } from "../../Btn";
import { ChipRadio } from "../../ChipRadio";
import { ArmedButton } from "../../Feedback";
import { ErrorWell, SettingRow } from "../../Field";
import { Group } from "../kit";
import { Status } from "../../Lamp";
import { Select } from "../../Select";
import { ui } from "../../api";
import { fail } from "../../errors";
import type { BootImage, Updates } from "../../gen/silo/v1/siloos_pb";
import { UI } from "../../gen/silo/v1/ui_pb";
import { msg, t } from "../../i18n.ts";
import { patch } from "../../query";
import { updateJobLook } from "../../statusLook";
import { channelChoices } from "./siloosView";

const modes = [
  { value: "off", label: msg("Off") },
  { value: "download", label: msg("Download") },
  { value: "reboot", label: msg("Download and restart") },
] as const;

const jobWords: Record<string, string> = {
  switch: msg("Moving to the new channel"),
  check: msg("Checking for an update"),
  download: msg("Downloading the update"),
  rollback: msg("Going back to the previous version"),
};

function imageLine(i: BootImage | undefined) {
  if (!i) return "";
  const when = i.timestamp ? new Date(i.timestamp).toLocaleDateString() : "";
  return [i.version || i.digest.slice(7, 19), when].filter(Boolean).join(" · ");
}

// UpdatesPanel is the machine's image (bootc): the one it runs, a newer one
// downloaded for the next start, the one before it to go back to, and the
// policy siloosd runs each day.
export function UpdatesPanel() {
  const q = useQuery(UI.method.getSiloOSUpdates, {}, { refetchInterval: (query) => (query.state.data?.job?.running ? 2_000 : 60_000) });
  const u = q.data;
  const [err, setErr] = useState("");
  const [rebooting, setRebooting] = useState(false);

  async function act(call: () => Promise<Updates>) {
    setErr("");
    try {
      const n = await call();
      patch(UI.method.getSiloOSUpdates, {}, () => n);
    } catch (e) {
      setErr(fail(e));
    }
  }
  async function restart() {
    setErr("");
    try {
      await ui.siloOSPower({ action: "reboot" });
      setRebooting(true);
    } catch (e) {
      setErr(fail(e));
    }
  }

  const running = !!u?.job?.running;
  const local = u?.channel === "local";
  return (
    <Group title={t("Updates")} note={t("Silo and the system update together: each version of SiloOS is one image.")}>
      {q.error ? (
        <div className="px-5 py-4">
          <ErrorWell>{fail(q.error)}</ErrorWell>
        </div>
      ) : null}
      {u ? (
        <>
          <SettingRow label={t("Running")} hint={u.booted?.image}>
            <span className="font-mono text-small text-ink">{u.version && u.version !== "dev" ? u.version : imageLine(u.booted) || "—"}</span>
          </SettingRow>
          <ChannelRow u={u} running={running} onMove={(channel) => act(() => ui.setSiloOSUpdateChannel({ channel }))} />
          {u.staged ? (
            <SettingRow label={t("Downloaded, used at the next start")} hint={rebooting ? t("Restarting into it…") : undefined}>
              <div className="flex items-center gap-2">
                <span className="font-mono text-small text-ink">{imageLine(u.staged)}</span>
                <ArmedButton size="sm" kind="primary" armedLabel={t("Click again to restart")} icon={<RotateCcw size={12} />} onConfirm={() => void restart()} disabled={rebooting}>
                  {t("Restart now")}
                </ArmedButton>
              </div>
            </SettingRow>
          ) : u.available ? (
            <SettingRow label={t("Available")}>
              <div className="flex items-center gap-2">
                <span className="font-mono text-small text-ink">{imageLine(u.available)}</span>
                <Btn size="sm" kind="primary" disabled={running} icon={<Download size={12} />} onClick={() => void act(() => ui.runSiloOSUpdate({ action: "download" }))}>
                  {t("Download")}
                </Btn>
              </div>
            </SettingRow>
          ) : null}
          {u.rollback ? (
            <SettingRow
              label={t("Previous version")}
              hint={u.rollbackQueued ? t("The machine goes back to it at its next start.") : t("Going back takes effect at the next start.")}
            >
              <div className="flex items-center gap-2">
                <span className="font-mono text-small text-ink">{imageLine(u.rollback)}</span>
                {u.rollbackQueued ? null : (
                  <ArmedButton size="sm" armedLabel={t("Click again to go back")} icon={<Undo2 size={12} />} disabled={running} onConfirm={() => void act(() => ui.runSiloOSUpdate({ action: "rollback" }))}>
                    {t("Go back")}
                  </ArmedButton>
                )}
              </div>
            </SettingRow>
          ) : null}
          {local ? null : (
            <SettingRow
              label={t("Automatic updates")}
              hint={
                u.policy?.mode === "reboot"
                  ? t("Each day at the hour below, a new version is downloaded and the machine restarts into it.")
                  : u.policy?.mode === "download"
                    ? t("Each day at the hour below, a new version is downloaded. It is used when the machine next restarts.")
                    : t("Nothing happens by itself.")
              }
            >
              <div className="flex flex-wrap items-center justify-end gap-2">
                <ChipRadio
                  size="sm"
                  label={t("Automatic updates")}
                  value={u.policy?.mode ?? "download"}
                  onChange={(mode) => void act(() => ui.setSiloOSUpdatePolicy({ policy: { mode, hour: u.policy?.hour ?? 4 } }))}
                  options={modes.map((m) => ({ value: m.value, label: t(m.label) }))}
                />
                {u.policy?.mode !== "off" ? (
                  <div className="w-[96px]">
                    <Select
                      ariaLabel={t("Hour")}
                      value={String(u.policy?.hour ?? 4)}
                      onChange={(h) => void act(() => ui.setSiloOSUpdatePolicy({ policy: { mode: u.policy?.mode ?? "download", hour: Number(h) } }))}
                      options={Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, "0")}:00` }))}
                    />
                  </div>
                ) : null}
              </div>
            </SettingRow>
          )}
          <SettingRow
            label={u.job ? t(jobWords[u.job.action] ?? u.job.action) : t("Check for an update")}
            hint={u.job?.error || (u.job && !u.job.running && u.job.endedAt ? t("Finished {{when}}", { when: new Date(u.job.endedAt).toLocaleString() }) : undefined)}
            meta={u.job ? <Status look={updateJobLook(u.job)} /> : undefined}
            error={err}
          >
            {local ? null : (
              <Btn size="sm" disabled={running} icon={<RefreshCw size={12} />} onClick={() => void act(() => ui.runSiloOSUpdate({ action: "check" }))}>
                {t("Check now")}
              </Btn>
            )}
          </SettingRow>
          {u.job?.output ? (
            <details className="px-5 py-3">
              <summary className="cursor-pointer text-label text-ink-3">{t("What bootc said")}</summary>
              <pre className="mt-2 max-h-[240px] overflow-auto whitespace-pre-wrap break-words rounded-sm bg-well p-3 font-mono text-label leading-5 text-ink-2">{u.job.output}</pre>
            </details>
          ) : null}
        </>
      ) : null}
    </Group>
  );
}

// ChannelRow says what the machine follows and moves it to another channel
// of its repository: latest, master or a release version the registry has,
// never an image older than the one running (siloosd refuses it; the list
// shows why). The new image is downloaded now and used at the next start,
// like any update.
function ChannelRow({ u, running, onMove }: { u: Updates; running: boolean; onMove: (channel: string) => Promise<void> }) {
  const listed = useQuery(UI.method.listSiloOSUpdateChannels, {}, { enabled: !!u.repository, staleTime: 5 * 60_000, refetchInterval: false });
  const { choices, current } = channelChoices(u.channel, u.tag, u.version, listed.data?.channels);
  const [pick, setPick] = useState(current);
  const words: Record<string, string> = {
    local: t("A local build: it follows no channel, so nothing updates it. Move it to one to get updates."),
    latest: t("Stable releases."),
    master: t("A build of every change to Silo: the newest, and the least tried."),
    pinned: t("This one version: nothing newer arrives until you move it."),
    custom: t("An image from another repository: {{image}}", { image: u.booted?.image ?? "" }),
  };
  if (!u.repository) return null;
  const picked = choices.find((c) => c.value === pick);
  const hint = [words[u.channel] ?? u.channel, t("Moving to an image older than the one running is not allowed: go back with Previous version instead.")];
  return (
    <SettingRow
      label={t("Channel")}
      hint={hint.join(" ")}
      error={listed.error ? t("The registry did not list its versions: {{error}}", { error: fail(listed.error) }) : undefined}
    >
      <div className="flex flex-wrap items-center justify-end gap-2">
        <div className="w-[240px]">
          <Select
            ariaLabel={t("Channel")}
            value={pick}
            onChange={setPick}
            placeholder={listed.isLoading ? t("Asking the registry…") : t("Pick a channel")}
            options={choices.map((c) => ({ value: c.value, label: c.label, hint: c.hint, disabled: c.disabled }))}
          />
        </div>
        {pick && pick !== current && !picked?.disabled ? (
          <ArmedButton size="sm" kind="primary" armedLabel={t("Click again to move")} disabled={running} onConfirm={() => void onMove(pick)}>
            {t("Move to {{channel}}", { channel: pick })}
          </ArmedButton>
        ) : null}
      </div>
    </SettingRow>
  );
}
