import { useQuery } from "@connectrpc/connect-query";
import { useNavigate } from "@tanstack/react-router";
import { UserPlus, X } from "lucide-react";
import { useRef, useState } from "react";
import { ui } from "../api";
import { btnClass } from "../Btn";
import { ChipRadio } from "../ChipRadio";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { UI, type Bot, type SpacePerson } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { reload } from "../query";
import { useDismiss } from "../useMenu";
import { Initials } from "./People";

const roleOptions = (guest: boolean) => [
  { value: "use", label: t("Can use") },
  { value: "manage", label: t("Can manage"), disabled: guest, title: guest ? t("A guest can only use a Bot.") : undefined },
];

// BotPeople is a Bot's Settings → People: who can use it and who manages it.
// Managers change the list; everyone else reads it and may leave.
export function BotPeople({ bot }: { bot: Bot }) {
  const nav = useNavigate();
  const botId = bot.id;
  const q = useQuery(UI.method.listBotPeople, { botId });
  const [err, setErr] = useState("");
  const people = q.data?.people ?? null;
  const candidates = q.data?.candidates ?? [];
  const manage = bot.role === "manage";
  const managers = people?.filter((p) => p.role === "manage").length ?? 0;
  const refresh = () => reload(UI.method.listBotPeople, { botId });

  async function act(fn: () => Promise<unknown>) {
    setErr("");
    try {
      await fn();
      await refresh();
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
            {t("Everyone on {{name}} shares what it holds: its memory, files, machine and connections. Only its managers change what it may do.", { name: bot.name })}
          </p>
        </div>
        {manage ? <AddPeople botId={botId} candidates={candidates} onAdded={() => void refresh()} /> : null}
      </div>
      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}
      {q.error ? <ErrorWell className="mb-4">{fail(q.error)}</ErrorWell> : null}
      {people === null ? (
        <SkeletonRows rows={2} height={56} />
      ) : (
        <ul className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
          {people.map((p) => {
            const last = p.role === "manage" && managers <= 1;
            return (
              <li key={p.userId} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <Initials email={p.email} />
                <div className="min-w-0 flex-1">
                  <span className="truncate text-[13.5px] font-medium text-ink">{p.email}</span>
                  {p.you ? <span className="ml-2 text-[12px] text-ink-3">{t("you")}</span> : null}
                </div>
                {manage && !last ? (
                  <ChipRadio
                    size="sm"
                    label={t("Role of {{who}}", { who: p.email })}
                    value={p.role}
                    onChange={(role) => void act(() => ui.setBotRole({ botId, userId: p.userId, role }))}
                    options={roleOptions(p.spaceRole === "guest")}
                  />
                ) : (
                  <span className="rounded-sm bg-well px-2 py-1 text-[12px] font-medium text-ink-2">{p.role === "manage" ? t("Can manage") : t("Can use")}</span>
                )}
                {(manage || p.you) && !last ? (
                  <ArmedButton
                    kind="ghost"
                    size="sm"
                    armedLabel={p.you ? t("Click again to leave") : t("Click again to remove")}
                    onConfirm={() =>
                      void act(async () => {
                        await ui.removeBotPerson({ botId, userId: p.userId });
                        if (p.you) {
                          await reload(UI.method.listBots);
                          void nav({ to: "/" });
                        }
                      })
                    }
                  >
                    {p.you ? t("Leave this Bot") : t("Remove")}
                  </ArmedButton>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      {manage && managers <= 1 ? <p className="mt-3 text-[12.5px] text-ink-3">{t("A Bot always has a manager. Make someone else a manager before you step down.")}</p> : null}
    </div>
  );
}

// AddPeople is the picker of the Workspace's people who are not on the Bot.
// A refusal says why, with the way out; secrets someone else set are named
// before the second click.
function AddPeople({ botId, candidates, onAdded }: { botId: string; candidates: SpacePerson[]; onAdded: () => void }) {
  const [open, setOpen] = useState(false);
  const [role, setRole] = useState("use");
  const [err, setErr] = useState("");
  const [confirm, setConfirm] = useState<{ userId: string; secrets: string[] } | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  useDismiss(open, trigger, menu, () => setOpen(false));

  async function add(p: SpacePerson, confirmed: boolean) {
    setErr("");
    try {
      const r = await ui.addBotPerson({ botId, userId: p.userId, role: p.role === "guest" ? "use" : role, confirmed });
      if (r.confirmSecrets.length > 0) {
        setConfirm({ userId: p.userId, secrets: r.confirmSecrets });
        return;
      }
      setConfirm(null);
      setOpen(false);
      onAdded();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  return (
    <div className="relative">
      <button ref={trigger} type="button" className={btnClass("primary")} onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <UserPlus size={15} />
        {t("Add people")}
      </button>
      {open ? (
        <div ref={menu} role="dialog" aria-label={t("Add people")} className="absolute right-0 z-40 mt-2 w-[340px] rounded-control bg-surface p-3 shadow-slip">
          <div className="rise">
            <div className="mb-2 flex items-center justify-between">
              <ChipRadio size="sm" label={t("Role")} value={role} onChange={setRole} options={roleOptions(false)} />
              <button type="button" aria-label={t("Close")} className="text-ink-3 hover:text-ink" onClick={() => setOpen(false)}>
                <X size={14} />
              </button>
            </div>
            {candidates.length === 0 ? (
              <p className="px-1 py-2 text-[12.5px] text-ink-3">{t("Everyone in the workspace is on this Bot. Invite people from the workspace's People tab.")}</p>
            ) : (
              <ul className="max-h-[260px] overflow-auto">
                {candidates.map((p) => (
                  <li key={p.userId}>
                    <button
                      type="button"
                      onClick={() => void add(p, confirm?.userId === p.userId)}
                      className="flex h-10 w-full items-center gap-2.5 rounded-sm px-2 text-left text-[13px] text-ink hover:bg-well"
                    >
                      <Initials email={p.email} size={22} />
                      <span className="min-w-0 flex-1 truncate">{p.email}</span>
                      {p.role === "guest" ? <span className="text-[11px] text-ink-3">{t("Guest", { context: "role" })}</span> : null}
                    </button>
                    {confirm?.userId === p.userId ? (
                      <p className="mx-2 mb-2 rounded-sm bg-well px-2.5 py-2 text-[12px] leading-[18px] text-ink-2">
                        {t("They will be able to have the Bot use secrets someone else set: {{names}}. Click again to add them.", { names: confirm.secrets.join(", ") })}
                      </p>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
            {err ? <p className="mt-2 px-1 text-[12.5px] leading-[18px] text-vermilion">{err}</p> : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}
