import { Plus } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { useBots } from "./bots";
import { btnClass } from "./Btn";
import { Crest } from "./Crest";
import { Lamp, StatusWord } from "./Lamp";

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

export function BotsPage() {
  const { bots, err } = useBots();
  const loading = bots === null;
  const empty = !loading && bots.length === 0;
  return (
    <div className="p-4 wide:p-7">
      {!empty ? (
        <div className="mb-6 flex items-center justify-between gap-3">
          <div>
            <h1 className="text-title">Bots</h1>
            <p className="mt-0.5 text-[12.5px] leading-[18px] text-ink-2">Machines you can open.</p>
          </div>
          <Link to="/new" className={btnClass("primary")}>
            <Plus size={16} />
            New Bot
          </Link>
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
          <p className="mb-2 text-[40px] leading-[48px] font-medium tracking-[-0.02em]">No Bots yet</p>
          <p className="mb-7 text-[14px] text-ink-2">A Bot is its own machine. It does not share files with the others.</p>
          <Link to="/new" className={btnClass("primary")}>
            <Plus size={16} />
            New Bot
          </Link>
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
    </div>
  );
}
