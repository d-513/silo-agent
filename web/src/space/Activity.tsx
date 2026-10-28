import { useQuery } from "@connectrpc/connect-query";
import { useParams } from "@tanstack/react-router";
import { fail } from "../errors";
import { ErrorWell, SkeletonRows } from "../Field";
import { ago } from "../format";
import { UI, type SpaceActivity } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";

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

// /w/$spaceId/activity: what happened in the Workspace. Never what a Bot did
// with what it was given: that stays with its managers.
export function SpaceActivityPage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/activity" });
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
