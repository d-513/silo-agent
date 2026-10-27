import { Link, Navigate, useNavigate, useParams } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { ui } from "../api";
import { Btn, btnClass } from "../Btn";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { ErrorWell, inputClass, SettingGroup, SettingRow } from "../Field";
import { UI } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { PaneFallback } from "../PaneFallback";
import { reload } from "../query";
import { markOf } from "./pick.ts";
import { forgetSpace, useCurrentSpace } from "./spaces";
import { SpaceMark } from "./SpaceTile";

// SpaceRedirect is /, /new and /skills: the same page in the Workspace the
// console is in (the one last opened here, else the first).
export function SpaceRedirect({ to }: { to: "home" | "new" | "skills" }) {
  const { space } = useCurrentSpace();
  if (!space) return <PaneFallback />;
  const params = { spaceId: space.id };
  if (to === "new") return <Navigate to="/w/$spaceId/new" params={params} replace />;
  if (to === "skills") return <Navigate to="/w/$spaceId/customize/skills" params={params} replace />;
  return <Navigate to="/w/$spaceId" params={params} replace />;
}

// The name and letters of a Workspace, with its tile as it will look.
function NameFields({ name, mark, onName, onMark }: { name: string; mark: string; onName: (v: string) => void; onMark: (v: string) => void }) {
  return (
    <div className="flex items-start gap-4">
      <div className="pt-6">
        <SpaceMark space={{ name, mark: markOf(name, mark) }} size={44} />
      </div>
      <div className="min-w-0 flex-1">
        <label htmlFor="space-name" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          {t("Name")}
        </label>
        <input id="space-name" className={`${inputClass} mb-4`} placeholder={"Acme labs"} value={name} onChange={(e) => onName(e.target.value)} autoFocus />
        <label htmlFor="space-mark" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          {t("Letters on its tile")}
        </label>
        <input
          id="space-mark"
          className={`${inputClass} w-24 uppercase`}
          maxLength={2}
          placeholder={markOf(name)}
          value={mark}
          onChange={(e) => onMark(e.target.value)}
        />
      </div>
    </div>
  );
}

export function NewSpacePage() {
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [mark, setMark] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  async function create(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setErr("");
    try {
      const s = await ui.createSpace({ name: name.trim(), mark: mark.trim() });
      await reload(UI.method.listSpaces);
      void nav({ to: "/w/$spaceId", params: { spaceId: s.id } });
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  return (
    <div className="silo-page silo-page-sm rise">
      <h1 className="mb-1.5 text-title">{t("New workspace")}</h1>
      <p className="mb-6 text-[13px] text-ink-2">{t("A workspace holds Bots and what they share. Each Bot still has its own machine.")}</p>
      <form onSubmit={create}>
        <NameFields name={name} mark={mark} onName={setName} onMark={setMark} />
        {err ? <ErrorWell className="mt-4">{err}</ErrorWell> : null}
        <div className="mt-6 flex gap-2">
          <Btn kind="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? t("Creating…") : t("Create workspace")}
          </Btn>
          <Link to="/" className={btnClass("secondary")}>
            {t("Cancel")}
          </Link>
        </div>
      </form>
    </div>
  );
}

export function SpaceSettingsPage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/settings" });
  const { spaces } = useCurrentSpace();
  const space = spaces?.find((s) => s.id === spaceId);
  if (!spaces) return <PaneFallback />;
  if (!space) return <Navigate to="/" replace />;
  // Keyed by the Workspace so its form starts from that Workspace's name.
  return <SpaceSettingsForm key={space.id} id={space.id} name={space.name} mark={space.mark} role={space.role} />;
}

function SpaceSettingsForm({ id, name: savedName, mark: savedMark, role }: { id: string; name: string; mark: string; role: string }) {
  const nav = useNavigate();
  const [name, setName] = useState(savedName);
  const [mark, setMark] = useState(savedMark === markOf(savedName) ? "" : savedMark);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [dangerErr, setDangerErr] = useState("");
  const changed = name.trim() !== savedName || markOf(name, mark) !== savedMark;

  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await ui.updateSpace({ id, name: name.trim(), mark: mark.trim() });
      await reload(UI.method.listSpaces);
    } catch (ex) {
      setErr(fail(ex));
    }
    setBusy(false);
  }

  async function remove() {
    setDangerErr("");
    try {
      await ui.deleteSpace({ id });
      forgetSpace(id);
      await reload(UI.method.listSpaces);
      void nav({ to: "/" });
    } catch (ex) {
      setDangerErr(fail(ex));
    }
  }

  return (
    <div className="silo-page silo-page-wide rise">
      <h1 className="mb-6 text-title">{t("Workspace settings")}</h1>
      <SettingGroup title={t("General")}>
        <form onSubmit={save} className="px-5 py-5">
          <NameFields name={name} mark={mark} onName={setName} onMark={setMark} />
          {err ? <ErrorWell className="mt-4">{err}</ErrorWell> : null}
          <div className="mt-5 flex gap-2">
            <Btn kind="primary" type="submit" disabled={busy || !changed || !name.trim()}>
              {busy ? t("Saving…") : t("Save")}
            </Btn>
          </div>
        </form>
      </SettingGroup>
      {role === "owner" ? (
        <SettingGroup title={t("Dangerous")} note={t("The first click arms, the second acts. These cannot be undone from here.")} tone="danger">
          <SettingRow
            label={t("Delete workspace")}
            hint={t("Only an empty workspace can go, and never your last one. Move or delete its Bots first.")}
            error={dangerErr}
          >
            <ArmedButton icon={<Trash2 size={14} />} onConfirm={() => void remove()}>
              {t("Delete")}
            </ArmedButton>
          </SettingRow>
        </SettingGroup>
      ) : null}
    </div>
  );
}
