import { useQuery } from "@connectrpc/connect-query";
import { Link, Navigate, useNavigate, useParams } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { ui } from "../api";
import { Btn, btnClass } from "../Btn";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { ErrorWell, inputClass, SettingGroup, SettingRow } from "../Field";
import { UI, type OtherBot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { PaneFallback } from "../PaneFallback";
import { Select } from "../Select";
import { reload } from "../query";
import { markOf } from "./pick.ts";
import { useBots } from "../bots";
import { Crest } from "../Crest";
import { Tip } from "../Tip";
import { Initials } from "./People";
import { canAdmin, canBuild, forgetSpace, useCurrentSpace } from "./spaces";
import { SpaceMark } from "./SpaceTile";

// SpaceRedirect is /, /new and /skills: the same page in the Workspace the
// console is in (the one last opened here, else the first).
// Home is the Workspace's first Bot you are on; with none, New Bot (or, for a
// guest, the Workspace page).
export function SpaceRedirect({ to }: { to: "home" | "new" | "skills" }) {
  const { space } = useCurrentSpace();
  const { bots } = useBots();
  if (!space || (to === "home" && !bots)) return <PaneFallback />;
  const params = { spaceId: space.id };
  if (to === "skills") return <Navigate to="/w/$spaceId/skills" params={params} replace />;
  const first = to === "home" ? bots?.find((b) => b.spaceId === space.id) : undefined;
  if (first) return <Navigate to="/bots/$botId/run" params={{ botId: first.id }} replace />;
  if (canBuild(space.role)) return <Navigate to="/w/$spaceId/new" params={params} replace />;
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
      void nav({ to: "/w/$spaceId/new", params: { spaceId: s.id } });
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
      <Membership id={id} role={role} />
      {canAdmin(role) ? <OtherBots spaceId={id} /> : null}
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

// Membership is leaving the Workspace (anyone but its owner) and handing it
// to an admin (its owner).
function Membership({ id, role }: { id: string; role: string }) {
  const peopleQ = useQuery(UI.method.listSpacePeople, { spaceId: id });
  const people = peopleQ.data?.people ?? [];
  const me = people.find((p) => p.you);
  const admins = people.filter((p) => p.role === "admin");
  const [heir, setHeir] = useState("");
  const [err, setErr] = useState("");
  const pick = heir || admins[0]?.userId || "";
  async function transfer() {
    setErr("");
    try {
      await ui.transferSpace({ spaceId: id, userId: pick });
      await Promise.all([reload(UI.method.listSpaces), reload(UI.method.listSpacePeople, { spaceId: id })]);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  if (role !== "owner") {
    if (!me) return null;
    return (
      <SettingGroup title={t("Membership")}>
        <SettingRow label={t("Leave workspace")} hint={t("You lose access to its Bots. Bots only you manage need a new manager first.")}>
          <Link to="/w/$spaceId/people/$userId" params={{ spaceId: id, userId: me.userId }} className={btnClass("secondary")}>
            {t("Leave…")}
          </Link>
        </SettingRow>
      </SettingGroup>
    );
  }
  if (people.length <= 1) return null;
  return (
    <SettingGroup title={t("Ownership")}>
      <SettingRow
        label={t("Transfer ownership")}
        hint={admins.length ? t("The admin you pick becomes the owner; you become an admin.") : t("Make someone an admin first: only an admin can become the owner.")}
        error={err}
      >
        {admins.length ? (
          <div className="flex flex-wrap items-center gap-2">
            <Select value={pick} onChange={setHeir} options={admins.map((p) => ({ value: p.userId, label: p.email }))} />
            <ArmedButton onConfirm={() => void transfer()}>{t("Transfer")}</ArmedButton>
          </div>
        ) : null}
      </SettingRow>
    </SettingGroup>
  );
}

// OtherBots is what a Workspace admin sees of the Bots they are not on: that
// they exist, who is on them and how much they are given, and Delete. They
// cannot be opened.
function OtherBots({ spaceId }: { spaceId: string }) {
  const q = useQuery(UI.method.listBots, { spaceId }, { refetchInterval: 15_000 });
  const others = q.data?.others ?? [];
  const [err, setErr] = useState("");
  if (others.length === 0) return null;
  async function remove(b: OtherBot) {
    setErr("");
    try {
      await ui.deleteBot({ id: b.id });
      await reload(UI.method.listBots, { spaceId });
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  return (
    <section className="mt-6">
      <h2 className="text-[13.5px] font-medium text-ink">{t("Other Bots")}</h2>
      <p className="mb-3 mt-0.5 text-[12.5px] text-ink-2">{t("Bots in this workspace you are not on. As an admin you can delete one, but not open it.")}</p>
      {err ? <p role="alert" className="mb-3 text-[13px] text-vermilion">{err}</p> : null}
      <div className="grid grid-cols-1 gap-3 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
        {others.map((b) => (
          <div key={b.id} className="flex items-center gap-3.5 rounded-card bg-surface/70 p-4 shadow-card">
            <Tip content={<p className="text-[12.5px]">{t("Only people on {{name}} can open it.", { name: b.name })}</p>}>
              <span tabIndex={0} className="opacity-60 outline-none">
                <Crest index={b.crest} size={40} />
              </span>
            </Tip>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[14px] font-medium text-ink-2">{b.name}</div>
              <div className="mt-1 flex items-center gap-1">
                {b.people.slice(0, 4).map((p) => (
                  <Initials key={p} email={p} size={20} />
                ))}
                {b.people.length > 4 ? <span className="text-[11px] text-ink-3">{`+${b.people.length - 4}`}</span> : null}
                <span className="ml-1.5 text-[11.5px] text-ink-3">{t("{{count}} grants", { count: b.grants })}</span>
              </div>
            </div>
            <ArmedButton kind="ghost" size="sm" iconOnly title={t("Delete {{name}}", { name: b.name })} armedLabel={t("Click again to delete")} icon={<Trash2 size={14} />} onConfirm={() => void remove(b)} />
          </div>
        ))}
      </div>
    </section>
  );
}
