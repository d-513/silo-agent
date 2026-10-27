import { useQuery } from "@connectrpc/connect-query";
import { useParams } from "@tanstack/react-router";
import { useState } from "react";
import { ui } from "../api";
import { RuleLayerPanel } from "../admin/OperatorRulesPanel";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { ErrorWell, Panel, SkeletonRows, textareaClass } from "../Field";
import { ago } from "../format";
import { UI, type SpaceActivity } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { reload } from "../query";
import { canAdmin, useSpaces } from "./spaces";

// /w/$spaceId/settings/rules: the Workspace's layer of the rules engine,
// between the operator's and each Bot's, and its policy texts.
export function SpaceRules() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/settings/rules" });
  const admin = canAdmin(useSpaces().spaces?.find((s) => s.id === spaceId)?.role);
  const q = useQuery(UI.method.listSpaceRules, { spaceId });
  return (
    <div className="silo-page silo-page-wide rise space-y-6">
      {q.error ? <ErrorWell>{fail(q.error)}</ErrorWell> : null}
      {admin ? (
        <RuleLayerPanel
          title={t("Workspace rules")}
          note={t("A rule here applies to every Bot in the workspace, under the operator's. A locked row is a floor: a Bot's managers may make it stricter, never looser. An unlocked one is a default they override. Use * as the action to cover a whole connector.")}
          empty={t("No workspace rules yet.")}
          rules={q.data?.rules ?? []}
          onSet={async (row) => {
            await ui.setSpaceRule({ spaceId, ...row });
            await reload(UI.method.listSpaceRules, { spaceId });
          }}
          onDelete={async (row) => {
            await ui.deleteSpaceRule({ spaceId, connector: row.connector, action: row.action });
            await reload(UI.method.listSpaceRules, { spaceId });
          }}
        />
      ) : (
        <p className="text-[13.5px] text-ink-2">{t("Only the workspace's admins change its rules.")}</p>
      )}
      {admin && q.data ? <Policies key={spaceId} spaceId={spaceId} approval={q.data.approvalPolicy} output={q.data.outputPolicy} /> : null}
    </div>
  );
}

function Policies({ spaceId, approval: savedApproval, output: savedOutput }: { spaceId: string; approval: string; output: string }) {
  const [approval, setApproval] = useState(savedApproval);
  const [output, setOutput] = useState(savedOutput);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const changed = approval !== savedApproval || output !== savedOutput;
  async function save() {
    setBusy(true);
    setErr("");
    try {
      await ui.updateSpacePolicy({ spaceId, approvalPolicy: approval, outputPolicy: output });
      await reload(UI.method.listSpaceRules, { spaceId });
    } catch (ex) {
      setErr(fail(ex));
    }
    setBusy(false);
  }
  return (
    <Panel title={t("Workspace policies")} note={t("Read after the operator's and before each Bot's own: by auto-approval, and by the output filter.")}>
      <label htmlFor="ws-approval" className="mb-1.5 block text-[12px] font-medium text-ink-3">
        {t("Auto-approval policy")}
      </label>
      <textarea id="ws-approval" className={`${textareaClass} mb-4 min-h-[88px] w-full`} value={approval} onChange={(e) => setApproval(e.target.value)} />
      <label htmlFor="ws-output" className="mb-1.5 block text-[12px] font-medium text-ink-3">
        {t("Output policy")}
      </label>
      <textarea id="ws-output" className={`${textareaClass} mb-4 min-h-[88px] w-full`} value={output} onChange={(e) => setOutput(e.target.value)} />
      {err ? <ErrorWell className="mb-3">{err}</ErrorWell> : null}
      <Btn kind="primary" disabled={busy || !changed} onClick={() => void save()}>
        {busy ? t("Saving…") : t("Save")}
      </Btn>
    </Panel>
  );
}

// What an activity line says it was.
function kindWord(e: SpaceActivity): string {
  switch (e.kind) {
    case "joined":
      return t("{{who}} joined", { who: e.subject });
    case "left":
      return t("{{who}} left", { who: e.subject });
    case "removed":
      return t("{{who}} was removed", { who: e.subject });
    case "role_changed":
      return t("{{who}} is now {{role}}", { who: e.subject, role: e.detail });
    case "bot_created":
      return t("Bot {{name}} made", { name: e.subject });
    case "bot_deleted":
      return t("Bot {{name}} deleted", { name: e.subject });
    case "granted":
      return t("{{what}} given to {{bot}}", { what: e.subject, bot: e.botName });
    case "revoked":
      return t("{{what}} taken from {{bot}}", { what: e.subject, bot: e.botName });
    case "lent":
      return e.detail === "shared" ? t("{{what}} shared with the workspace", { what: e.subject }) : t("{{what}} made personal", { what: e.subject });
    case "signed_in":
      return t("Signed in to {{what}}", { what: e.subject });
  }
  return e.kind;
}

// /w/$spaceId/settings/activity: what happened in the Workspace. Never what
// a Bot did with what it was given: that stays with its managers.
export function SpaceActivityPage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/settings/activity" });
  const q = useQuery(UI.method.listSpaceActivity, { spaceId }, { refetchInterval: 15_000 });
  const events = q.data?.events ?? null;
  return (
    <div className="silo-page silo-page-wide rise">
      <h2 className="text-title text-ink">{t("Activity")}</h2>
      <p className="mb-5 mt-1 max-w-[560px] text-[13.5px] leading-[21px] text-ink-2">
        {t("People joining and leaving, Bots made and deleted, what was given to which Bot, and sign-ins. What a Bot does with it stays with its managers.")}
      </p>
      {q.error ? <ErrorWell>{fail(q.error)}</ErrorWell> : null}
      {events === null ? (
        q.error ? null : <SkeletonRows rows={4} height={44} />
      ) : events.length === 0 ? (
        <p className="text-[13.5px] text-ink-3">{t("Nothing yet.")}</p>
      ) : (
        <ul className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
          {events.map((e) => (
            <li key={e.id} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
              <span className="min-w-0 flex-1 truncate text-ink">{kindWord(e)}</span>
              {e.actor ? <span className="truncate text-[12px] text-ink-3">{e.actor}</span> : null}
              <span className="shrink-0 text-[12px] text-ink-3">{ago(e.createdAt)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
