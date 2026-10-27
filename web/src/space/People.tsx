import { useQuery } from "@connectrpc/connect-query";
import { Link, Outlet, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { History, Link2, ListChecks, SlidersHorizontal, Trash2, UserPlus, Users, type LucideIcon } from "lucide-react";
import { useState } from "react";
import { ui } from "../api";
import { TabStrip } from "../bot/SectionTabs";
import { Btn, btnClass } from "../Btn";
import { ChipRadio } from "../ChipRadio";
import { Crest } from "../Crest";
import { fail } from "../errors";
import { ArmedButton, CopyButton } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { day } from "../format";
import { UI, type LastManaged, type SpacePerson } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";
import { PageHead } from "../PageHead";
import { reload } from "../query";
import { initials, mayChange } from "./people.ts";
import { forgetSpace, canAdmin, useSpaces } from "./spaces";
import { roleWord } from "./SpaceTile";

/** A person's letters on a `well` circle. */
export function Initials({ email, size = 28 }: { email: string; size?: number }) {
  return (
    <span
      aria-hidden
      title={email}
      style={{ width: size, height: size, fontSize: size >= 28 ? 11 : 9.5 }}
      className="flex shrink-0 items-center justify-center rounded-full bg-well font-semibold tracking-[0.02em] text-ink-2 shadow-[inset_0_0_0_1px_var(--color-line)]"
    >
      {initials(email)}
    </span>
  );
}

type SettingsTab = "general" | "people" | "rules" | "activity";
const tabs: readonly {
  tab: SettingsTab;
  to: "/w/$spaceId/settings" | "/w/$spaceId/settings/people" | "/w/$spaceId/settings/rules" | "/w/$spaceId/settings/activity";
  label: string;
  icon: LucideIcon;
  admin?: boolean;
}[] = [
  { tab: "general", to: "/w/$spaceId/settings", label: msg("General"), icon: SlidersHorizontal },
  { tab: "people", to: "/w/$spaceId/settings/people", label: msg("People"), icon: Users },
  { tab: "rules", to: "/w/$spaceId/settings/rules", label: msg("Rules"), icon: ListChecks, admin: true },
  { tab: "activity", to: "/w/$spaceId/settings/activity", label: msg("Activity"), icon: History, admin: true },
];

// The Workspace's settings: General and People, one tab each.
export function SpaceSettingsLayout() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/settings" });
  const { pathname } = useLocation();
  const admin = canAdmin(useSpaceRole(spaceId).role);
  const open: SettingsTab = tabs.find((x) => x.tab !== "general" && pathname.startsWith(`/w/${spaceId}/settings/${x.tab}`))?.tab ?? "general";
  return (
    <section className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-4 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:px-5">
        <h1 className="hidden shrink-0 text-card-title leading-5 wide:block">{t("Workspace settings")}</h1>
        <TabStrip tab={open}>
          {tabs.filter((x) => admin || !x.admin).map(({ tab, to, label, icon: Icon }) => {
            const on = tab === open;
            return (
              <Link
                key={tab}
                to={to}
                params={{ spaceId }}
                data-tab-on={on || undefined}
                className={`flex h-8 shrink-0 items-center gap-1.5 self-center whitespace-nowrap rounded-sm px-2.5 text-[13px] font-medium transition-[background-color,color] duration-[160ms] ease-quiet ${
                  on ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"
                }`}
              >
                <Icon size={15} />
                {t(label)}
              </Link>
            );
          })}
        </TabStrip>
      </header>
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </section>
  );
}

function useSpaceRole(spaceId: string) {
  const { spaces } = useSpaces();
  const space = spaces?.find((s) => s.id === spaceId);
  return { space, role: space?.role ?? "" };
}

// /w/$spaceId/settings/people: who is in the Workspace and with what role.
export function SpacePeople() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/settings/people/" });
  const { space, role } = useSpaceRole(spaceId);
  const admin = canAdmin(role);
  const peopleQ = useQuery(UI.method.listSpacePeople, { spaceId });
  const invitesQ = useQuery(UI.method.listSpaceInvites, { spaceId }, { enabled: admin });
  const [err, setErr] = useState("");
  const people = peopleQ.data?.people ?? null;
  const invites = invitesQ.data?.invites ?? [];
  const solo = people !== null && people.length === 1;

  async function setRole(p: SpacePerson, next: string) {
    setErr("");
    try {
      await ui.setSpaceRole({ spaceId, userId: p.userId, role: next });
      await reload(UI.method.listSpacePeople, { spaceId });
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  return (
    <div className="silo-page silo-page-wide rise">
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 className="text-title text-ink">{t("People")}</h2>
          <p className="mt-1 max-w-[560px] text-[13.5px] leading-[21px] text-ink-2">
            {t("Everyone here can be given Bots to use. Admins see every Bot in the workspace, but cannot open one they are not on.")}
          </p>
        </div>
        {admin ? (
          <Link to="/w/$spaceId/settings/people/invite" params={{ spaceId }} className={btnClass("primary")}>
            <UserPlus size={15} />
            {t("Invite")}
          </Link>
        ) : null}
      </div>
      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}
      {peopleQ.error ? <ErrorWell className="mb-4">{fail(peopleQ.error)}</ErrorWell> : null}
      {people === null ? (
        <SkeletonRows rows={2} height={56} />
      ) : solo ? (
        <div className="rounded-card bg-surface p-6 shadow-card">
          <p className="text-[13.5px] leading-[21px] text-ink-2">
            {t("Only you are in {{name}}. Invite people to share its Bots and what they use, without sharing your own.", { name: space?.name ?? "" })}
          </p>
        </div>
      ) : (
        <ul className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
          {people.map((p) => {
            const editable = admin && !p.you && mayChange(role, p.role);
            return (
              <li key={p.userId} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <Initials email={p.email} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-[13.5px] font-medium text-ink">{p.email}</span>
                    {p.you ? <span className="text-[12px] text-ink-3">{t("you")}</span> : null}
                    {p.disabled ? <span className="rounded-sm bg-vermilion-pale px-1.5 py-0.5 text-[11px] font-medium text-vermilion">{t("Disabled")}</span> : null}
                  </div>
                  <div className="text-[12px] text-ink-3">{t("On {{count}} Bots", { count: p.bots })}</div>
                </div>
                {p.role === "owner" || !editable ? (
                  <span className="rounded-sm bg-well px-2 py-1 text-[12px] font-medium text-ink-2">{p.role === "owner" ? t("Owner", { context: "role" }) : roleWord(p.role)}</span>
                ) : (
                  <ChipRadio
                    size="sm"
                    label={t("Role of {{who}}", { who: p.email })}
                    value={p.role}
                    onChange={(v) => void setRole(p, v)}
                    options={[
                      { value: "admin", label: roleWord("admin"), disabled: role !== "owner", title: role !== "owner" ? t("Only the owner makes admins.") : undefined },
                      { value: "member", label: roleWord("member") },
                      { value: "guest", label: roleWord("guest") },
                    ]}
                  />
                )}
                {editable || (p.you && p.role !== "owner") ? (
                  <Link
                    to="/w/$spaceId/settings/people/$userId"
                    params={{ spaceId, userId: p.userId }}
                    className="h-8 rounded-control px-2.5 text-[12.5px] font-medium leading-8 text-ink-2 transition-colors hover:bg-well hover:text-ink"
                  >
                    {p.you ? t("Leave") : t("Remove")}
                  </Link>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      {admin && invites.length > 0 ? (
        <section className="mt-8">
          <h3 className="mb-2 text-[13.5px] font-medium text-ink">{t("Open invite links")}</h3>
          <ul className="space-y-2">
            {invites.map((inv) => (
              <li key={inv.id} className="flex items-center gap-3 rounded-card bg-well px-4 py-2.5">
                <Link2 size={15} className="text-ink-3" />
                <span className="min-w-0 flex-1 truncate text-[13px] text-ink">
                  {t("{{role}} · made by {{who}} · until {{date}}", { role: roleWord(inv.role), who: inv.createdBy, date: day(inv.expiresAt) })}
                </span>
                <ArmedButton
                  kind="ghost"
                  size="sm"
                  iconOnly
                  title={t("Delete link")}
                  armedLabel={t("Click again to delete")}
                  icon={<Trash2 size={14} />}
                  onConfirm={async () => {
                    await ui.deleteSpaceInvite({ spaceId, id: inv.id });
                    await reload(UI.method.listSpaceInvites, { spaceId });
                  }}
                />
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}

// /w/$spaceId/settings/people/invite: make a link, shown once.
export function SpaceInvitePage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/settings/people/invite" });
  const nav = useNavigate();
  const { space, role } = useSpaceRole(spaceId);
  const [pick, setPick] = useState("member");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const back = () => void nav({ to: "/w/$spaceId/settings/people", params: { spaceId } });

  async function make() {
    setBusy(true);
    setErr("");
    try {
      const r = await ui.createSpaceInvite({ spaceId, role: pick });
      setUrl(r.url);
      void reload(UI.method.listSpaceInvites, { spaceId });
    } catch (ex) {
      setErr(fail(ex));
    }
    setBusy(false);
  }

  const hint: Record<string, string> = {
    admin: t("Sees every Bot in the workspace, invites people and removes them. Cannot open a Bot they are not on."),
    member: t("Makes Bots, adds connectors, drives and skills, and uses the Bots they are given."),
    guest: t("Only uses the Bots they are given. Does not see the rest of the workspace."),
  };
  return (
    <div className="silo-page silo-page-sm rise">
      <PageHead title={t("Invite to {{name}}", { name: space?.name ?? "" })} onBack={back} />
      {url ? (
        <div className="rounded-card bg-surface p-5 shadow-card">
          <p className="mb-3 text-[13.5px] leading-[21px] text-ink-2">
            {t("Send this link to the person you are inviting. It works once, for 7 days, and is shown only now.")}
          </p>
          <div className="flex items-center gap-2 rounded-control bg-well px-3 py-2">
            <code className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-ink">{url}</code>
            <CopyButton text={url} label />
          </div>
          <div className="mt-5 flex gap-2">
            <Btn kind="secondary" onClick={() => setUrl("")}>
              {t("Make another")}
            </Btn>
            <Btn kind="ghost" onClick={back}>
              {t("Done")}
            </Btn>
          </div>
        </div>
      ) : (
        <div className="rounded-card bg-surface p-5 shadow-card">
          <p className="mb-2 text-[12px] font-medium text-ink-3">{t("They join as")}</p>
          <ChipRadio
            label={t("Role")}
            value={pick}
            onChange={setPick}
            options={[
              { value: "admin", label: roleWord("admin"), disabled: role !== "owner", title: role !== "owner" ? t("Only the owner makes admins.") : undefined },
              { value: "member", label: roleWord("member") },
              { value: "guest", label: roleWord("guest") },
            ]}
          />
          <p className="mt-2 text-[12.5px] leading-[19px] text-ink-2">{hint[pick]}</p>
          {err ? <ErrorWell className="mt-4">{err}</ErrorWell> : null}
          <div className="mt-5">
            <Btn kind="primary" onClick={() => void make()} disabled={busy}>
              <Link2 size={14} />
              {busy ? t("Making…") : t("Make link")}
            </Btn>
          </div>
        </div>
      )}
    </div>
  );
}

// Handoff is what happens to one Bot whose last manager is going.
type Handoff = { botId: string; action: string; userId: string };

// A choice for one Bot whose last manager is going.
function HandoffRow({ lm, self, value, onChange }: { lm: LastManaged; self: boolean; value: Handoff; onChange: (h: Handoff) => void }) {
  const heirs = lm.others.filter((p) => p.spaceRole !== "guest");
  const options = [
    ...(heirs.length > 0 ? [{ value: "manager", label: t("Make someone manager") }] : []),
    ...(!self ? [{ value: "take", label: t("Take it over") }] : []),
    { value: "delete", label: t("Delete it"), tone: "text-vermilion" },
  ];
  return (
    <li className="rounded-card bg-surface p-4 shadow-card">
      <div className="mb-3 flex items-center gap-3">
        <Crest index={lm.crest} size={28} />
        <span className="text-[13.5px] font-medium text-ink">{lm.name}</span>
      </div>
      <ChipRadio size="sm" label={lm.name} value={value.action} onChange={(action) => onChange({ ...value, action, userId: action === "manager" ? (value.userId || heirs[0]?.userId) ?? "" : "" })} options={options} />
      {value.action === "manager" ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {heirs.map((p) => (
            <button
              key={p.userId}
              type="button"
              aria-pressed={value.userId === p.userId}
              onClick={() => onChange({ ...value, userId: p.userId })}
              className={`flex h-8 items-center gap-2 rounded-control px-2.5 text-[12.5px] ${
                value.userId === p.userId ? "bg-surface text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "bg-well text-ink-2 hover:text-ink"
              }`}
            >
              <Initials email={p.email} size={20} />
              {p.email}
            </button>
          ))}
        </div>
      ) : null}
      {value.action === "delete" ? <p className="mt-2 text-[12.5px] text-vermilion">{t("Its machine, files, memories and chats are deleted.")}</p> : null}
    </li>
  );
}

// /w/$spaceId/settings/people/$userId: removing someone, or leaving. It is a
// page because it has choices: what happens to the Bots only they manage,
// and what leaves with them.
export function SpaceRemovePage() {
  const { spaceId, userId } = useParams({ from: "/_authed/w/$spaceId/settings/people/$userId" });
  const nav = useNavigate();
  const { space } = useSpaceRole(spaceId);
  const planQ = useQuery(UI.method.planSpaceRemoval, { spaceId, userId }, { retry: false });
  const people = useQuery(UI.method.listSpacePeople, { spaceId });
  const self = people.data?.people.find((p) => p.userId === userId)?.you ?? false;
  const [choices, setChoices] = useState<Record<string, Handoff>>({});
  const [err, setErr] = useState("");
  const back = () => void nav({ to: "/w/$spaceId/settings/people", params: { spaceId } });
  const plan = planQ.data;

  const choiceOf = (lm: LastManaged): Handoff => {
    const c = choices[lm.botId];
    if (c) return c;
    const heir = lm.others.find((p) => p.spaceRole !== "guest");
    if (heir) return { botId: lm.botId, action: "manager", userId: heir.userId };
    return { botId: lm.botId, action: self ? "delete" : "take", userId: "" };
  };

  async function remove() {
    if (!plan) return;
    setErr("");
    try {
      await ui.removeSpacePerson({ spaceId, userId, plan: plan.lastManaged.map(choiceOf) });
      if (self) {
        forgetSpace(spaceId);
        await reload(UI.method.listSpaces);
        void nav({ to: "/" });
        return;
      }
      await reload(UI.method.listSpacePeople, { spaceId });
      back();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  const personal = plan?.lent.filter((l) => !l.shared) ?? [];
  const shared = plan?.lent.filter((l) => l.shared) ?? [];
  const title = self ? t("Leave {{name}}", { name: space?.name ?? "" }) : t("Remove {{who}}", { who: plan?.email ?? "" });
  return (
    <div className="silo-page silo-page-sm rise">
      <PageHead title={title} onBack={back} />
      {planQ.error ? <ErrorWell>{fail(planQ.error)}</ErrorWell> : null}
      {!plan ? (
        planQ.error ? null : <SkeletonRows rows={2} height={64} />
      ) : (
        <>
          {plan.lastManaged.length > 0 ? (
            <section className="mb-6">
              <h3 className="mb-1 text-[13.5px] font-medium text-ink">{t("Bots nobody else manages")}</h3>
              <p className="mb-3 text-[12.5px] text-ink-2">{t("A Bot always has a manager. Choose what happens to each.")}</p>
              <ul className="space-y-2.5">
                {plan.lastManaged.map((lm) => (
                  <HandoffRow key={lm.botId} lm={lm} self={self} value={choiceOf(lm)} onChange={(h) => setChoices((c) => ({ ...c, [lm.botId]: h }))} />
                ))}
              </ul>
            </section>
          ) : null}
          {personal.length > 0 ? (
            <section className="mb-6">
              <h3 className="mb-1 text-[13.5px] font-medium text-ink">{t("Deleted with them")}</h3>
              <p className="mb-2 text-[12.5px] text-ink-2">{t("Personal sign-ins carry their account, so they go too.")}</p>
              <LentList items={personal} />
            </section>
          ) : null}
          {shared.length > 0 ? (
            <section className="mb-6">
              <h3 className="mb-1 text-[13.5px] font-medium text-ink">{t("Signed out")}</h3>
              <p className="mb-2 text-[12.5px] text-ink-2">{t("Shared sign-ins stay in the workspace and wait for someone to sign them in again.")}</p>
              <LentList items={shared} />
            </section>
          ) : null}
          {plan.lastManaged.length === 0 && plan.lent.length === 0 ? (
            <p className="mb-6 text-[13.5px] text-ink-2">{self ? t("You will lose access to the workspace and its Bots.") : t("They will lose access to the workspace and its Bots. Their chats stay with the Bots.")}</p>
          ) : null}
          {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}
          <ArmedButton kind="primary" armedLabel={t("Click again to confirm")} onConfirm={() => void remove()}>
            {self ? t("Leave workspace") : t("Remove {{who}}", { who: plan.email })}
          </ArmedButton>
        </>
      )}
    </div>
  );
}

function LentList({ items }: { items: { kind: string; id: string; name: string; bots: string[]; botsHidden: number }[] }) {
  return (
    <ul className="space-y-1.5">
      {items.map((l) => (
        <li key={l.id} className="flex items-center gap-2 rounded-control bg-well px-3 py-2 text-[13px]">
          <span className="font-medium text-ink">{l.name}</span>
          <span className="text-ink-3">{l.kind === "drive" ? t("Drive") : t("Connector")}</span>
          {l.bots.length + l.botsHidden > 0 ? <span className="ml-auto truncate text-[12px] text-ink-3">{t("in {{count}} Bots", { count: l.bots.length + l.botsHidden })}</span> : null}
        </li>
      ))}
    </ul>
  );
}
