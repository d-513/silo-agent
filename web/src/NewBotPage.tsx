import { useQuery } from "@connectrpc/connect-query";
import { Check } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { COLOR_COUNT, Crest, CrestPicker, packCrest, SHAPE_COUNT } from "./Crest";
import { fail } from "./errors";
import { UI } from "./gen/silo/v1/ui_pb";
import { reload } from "./query";
import { inputClass, textareaClass } from "./Field";
import { useAuth } from "./auth";
import { Initials } from "./space/People";
import { canBuild, useSpaces } from "./space/spaces";
import { ToggleRow } from "./Switch";
import { deviceZone } from "./zones";
import { t } from "./i18n.ts";

function randomCrest() {
  return packCrest(Math.floor(Math.random() * SHAPE_COUNT), Math.floor(Math.random() * COLOR_COUNT));
}

export function NewBotPage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/new" });
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [crest, setCrest] = useState(randomCrest);
  const [interview, setInterview] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const { email: me } = useAuth();
  const { spaces } = useSpaces();
  const build = canBuild(spaces?.find((x) => x.id === spaceId)?.role);
  // What the Workspace has for it, all ticked (but nobody else's personal
  // sign-in), and who else uses it.
  const conns = useQuery(UI.method.listSpaceConnectors, { spaceId }, { enabled: build }).data?.connectors ?? [];
  const drives = useQuery(UI.method.listSpaceDrives, { spaceId, draftId: "" }, { enabled: build }).data?.drives ?? [];
  const skills = useQuery(UI.method.listSkills, { scope: "space", spaceId }, { enabled: build }).data?.skills ?? [];
  const people = (useQuery(UI.method.listSpacePeople, { spaceId }).data?.people ?? []).filter((p) => !p.you);
  const [off, setOff] = useState<Set<string>>(new Set());
  const [users, setUsers] = useState<string[]>([]);
  const toggle = (key: string) => setOff((cur) => {
    const next = new Set(cur);
    if (!next.delete(key)) next.add(key);
    return next;
  });
  // A personal sign-in may go only where its lender alone works: someone
  // else's never, mine only while nobody else is picked.
  const blocked = (lentBy: string, shared: boolean) =>
    !lentBy || shared ? "" : lentBy !== me ? t("Personal to {{who}}", { who: lentBy }) : users.length ? t("Personal: others will use this Bot") : "";
  const takes = (key: string, why: string) => !why && !off.has(key);
  async function create(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setErr("");
    try {
      // The Bot starts in this device's time zone and follows it from here.
      const b = await ui.createBot({
        spaceId,
        name: name.trim(),
        crest,
        description: description.trim(),
        timeZone: deviceZone(),
        interview,
        connectorIds: conns.filter((c) => takes(`c:${c.id}`, blocked(c.lentBy, c.shared))).map((c) => c.id),
        driveIds: drives.filter((d) => takes(`d:${d.id}`, blocked(d.lentBy, d.shared))).map((d) => d.id),
        skillsOff: skills.filter((k) => off.has(`s:${k.name}`)).map((k) => k.name),
        userIds: users,
      });
      void reload(UI.method.listBots);
      void nav({ to: "/bots/$botId/run", params: { botId: b.id } });
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  return (
    <div className="silo-page silo-page-sm rise">
      <h1 className="mb-6 text-title">{t("New Bot")}</h1>
      <form onSubmit={create}>
        <div className="mb-5 flex flex-col items-center gap-4">
          <Crest index={crest} size={88} />
          <span className="text-[12px] text-ink-3">{t("Pick a crest for this machine")}</span>
        </div>
        <div className="mb-6">
          <CrestPicker value={crest} onChange={setCrest} />
        </div>
        <label htmlFor="bot-name" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          {t("Name")}
        </label>
        <input
          id="bot-name"
          className={`${inputClass} mb-4`}
          placeholder={"Scout"}
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />
        <label htmlFor="bot-desc" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          {t("Description")}
        </label>
        <textarea
          id="bot-desc"
          className={`${textareaClass} mb-3 min-h-[72px]`}
          placeholder={t("What this machine is for")}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <p className="mb-4 text-[13px] text-ink-2">{t("A Bot is its own machine. It does not share files with the others.")}</p>
        <ToggleRow
          className="mb-6 rounded-control bg-well px-3.5 py-3"
          label={t("Start with a short interview")}
          hint={t("The Bot asks what you want help with, offers what to connect, and how proactive to be. You can skip any step, or run /onboard later.")}
          on={interview}
          onChange={setInterview}
        />
        {conns.length + drives.length + skills.length > 0 ? (
          <section className="mb-6">
            <h2 className="mb-1 text-[13.5px] font-medium text-ink">{t("What it can use")}</h2>
            <p className="mb-3 text-[12.5px] text-ink-2">{t("The workspace's connectors, drives and skills. Untick what this Bot should not have; you can change it later in Customize.")}</p>
            <div className="flex flex-wrap gap-2">
              {conns.map((c) => (
                <Pick key={c.id} on={takes(`c:${c.id}`, blocked(c.lentBy, c.shared))} why={blocked(c.lentBy, c.shared)} onClick={() => toggle(`c:${c.id}`)}>
                  {c.name}
                </Pick>
              ))}
              {drives.map((d) => (
                <Pick key={d.id} on={takes(`d:${d.id}`, blocked(d.lentBy, d.shared))} why={blocked(d.lentBy, d.shared)} onClick={() => toggle(`d:${d.id}`)}>
                  {d.name}
                </Pick>
              ))}
              {skills.map((k) => (
                <Pick key={k.name} on={!off.has(`s:${k.name}`)} why="" onClick={() => toggle(`s:${k.name}`)}>
                  {k.name}
                </Pick>
              ))}
            </div>
          </section>
        ) : null}
        {people.length > 0 ? (
          <section className="mb-6">
            <h2 className="mb-1 text-[13.5px] font-medium text-ink">{t("Who can use it")}</h2>
            <p className="mb-3 text-[12.5px] text-ink-2">{t("Just you, unless you pick people. Everyone on a Bot shares what it holds: its memory, files and connections.")}</p>
            <div className="flex flex-wrap gap-2">
              {people.map((p) => {
                const on = users.includes(p.userId);
                return (
                  <Pick key={p.userId} on={on} why="" onClick={() => setUsers(on ? users.filter((x) => x !== p.userId) : [...users, p.userId])}>
                    <Initials email={p.email} size={18} />
                    {p.email}
                  </Pick>
                );
              })}
            </div>
          </section>
        ) : null}
        {err && (
          <p role="alert" className="mb-3 text-[13px] text-vermilion">
            {err}
          </p>
        )}
        <div className="flex gap-2">
          <Btn kind="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? t("Creating…") : t("Create Bot")}
          </Btn>
          <Link to="/w/$spaceId" params={{ spaceId }} className={btnClass("secondary")}>
            {t("Cancel")}
          </Link>
        </div>
      </form>
    </div>
  );
}

// Pick is one thing a new Bot may get: on, off, or unavailable with why.
function Pick({ on, why, onClick, children }: { on: boolean; why: string; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      aria-disabled={!!why || undefined}
      title={why || undefined}
      onClick={() => !why && onClick()}
      className={`flex h-8 items-center gap-1.5 rounded-control px-2.5 text-[12.5px] font-medium transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${
        why ? "cursor-default bg-well text-ink-3 line-through" : on ? "bg-surface text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "bg-well text-ink-2 hover:text-ink"
      }`}
    >
      {children}
      {on && !why ? <Check size={12} className="text-cobalt" /> : null}
    </button>
  );
}
