import { useQuery } from "@connectrpc/connect-query";
import { Plus, Trash2 } from "lucide-react";
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { ui } from "./api";
import { useBots } from "./bots";
import { btnClass } from "./Btn";
import { Crest } from "./Crest";
import { fail } from "./errors";
import { ArmedButton } from "./Feedback";
import { UI, type OtherBot } from "./gen/silo/v1/ui_pb";
import { Lamp, StatusWord } from "./Lamp";
import { reload } from "./query";
import { Initials } from "./space/People";
import { canAdmin, canBuild, useSpaces } from "./space/spaces";
import { Tip } from "./Tip";
import { t } from "./i18n.ts";

function FolioSkeleton() {
  return (
    <div className="flex gap-4 rounded-card bg-surface p-5 shadow-card" aria-hidden>
      <div className="skeleton h-14 w-14 shrink-0 rounded-card" />
      <div className="min-w-0 flex-1 py-1">
        <div className="skeleton mb-2.5 h-4 w-28 rounded-xs" />
        <div className="skeleton mb-2.5 h-3 w-44 rounded-xs" />
        <div className="skeleton h-3 w-16 rounded-xs" />
      </div>
    </div>
  );
}

// BotsPage is a Workspace's home: the Bots in it the user is on.
export function BotsPage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId" });
  const { bots: all, err } = useBots();
  const { spaces } = useSpaces();
  const space = spaces?.find((s) => s.id === spaceId);
  const bots = all?.filter((b) => b.spaceId === spaceId) ?? null;
  const loading = bots === null;
  const empty = !loading && bots.length === 0;
  const build = canBuild(space?.role);
  return (
    <div className="p-4 wide:p-7">
      {!empty ? (
        <div className="mb-6 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <h1 className="truncate text-title">{space?.name ?? t("Bots")}</h1>
            <p className="mt-0.5 text-[12.5px] leading-[18px] text-ink-2">{t("Machines you can open.")}</p>
          </div>
          {build ? (
            <Link to="/w/$spaceId/new" params={{ spaceId }} className={btnClass("primary")}>
              <Plus size={16} />{t("New Bot")}
            </Link>
          ) : null}
        </div>
      ) : null}
      {err && (
        <p role="alert" className="mb-4 text-[13px] text-vermilion">
          {err}
        </p>
      )}
      {loading ? (
        <div className="grid grid-cols-1 gap-4 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
          <FolioSkeleton />
          <FolioSkeleton />
          <FolioSkeleton />
        </div>
      ) : empty ? (
        <div className="rise pt-[12vh]">
          <p className="mb-2 text-[40px] leading-[48px] font-medium tracking-[-0.02em]">{t("No Bots yet")}</p>
          <p className="mb-7 text-[14px] text-ink-2">{t("A Bot is its own machine. It does not share files with the others.")}</p>
          {build ? (
            <Link to="/w/$spaceId/new" params={{ spaceId }} className={btnClass("primary")}>
              <Plus size={16} />{t("New Bot")}
            </Link>
          ) : null}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 min-[1100px]:grid-cols-2 min-[1440px]:grid-cols-3">
          {bots.map((b, i) => (
            <Link
              key={b.id}
              to="/bots/$botId/run"
              params={{ botId: b.id }}
              style={{ animationDelay: `${Math.min(i * 45, 270)}ms` }}
              className="rise blink group relative flex items-center gap-4 overflow-hidden rounded-card bg-surface p-5 shadow-card transition-[box-shadow,transform] duration-[200ms] ease-quiet hover:-translate-y-px hover:shadow-float active:scale-[.995] active:duration-[70ms] motion-reduce:hover:translate-y-0"
            >
              {b.status === "needs_you" ? (
                <span aria-hidden className="absolute inset-y-0 left-0 w-[2px] bg-vermilion" />
              ) : null}
              <Crest index={b.crest} size={56} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-card-title leading-5">{b.name}</div>
                {b.description ? <div className="mt-0.5 truncate text-[12.5px] leading-[18px] text-ink-2">{b.description}</div> : null}
                <div className="mt-2 flex items-center gap-2">
                  <Lamp status={b.status} />
                  <StatusWord status={b.status} />
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
      {canAdmin(space?.role) ? <OtherBots spaceId={spaceId} /> : null}
    </div>
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
    <section className="mt-10">
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
