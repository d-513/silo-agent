import { ChevronRight, MessageSquareQuote } from "lucide-react";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Btn } from "../Btn";
import type { FeedPost } from "../gen/silo/v1/ui_pb";
import { feedStamp } from "../format";
import { t } from "../i18n.ts";
import { Md } from "../Md";
import { sourceLabel } from "./Asks";

// SinceLooked is the Feed's new posts, shown on the Dashboard for the visit
// that read them: the server marks them read when the page opens, and they
// stay here, dotted, until the owner leaves. A row opens in place.
export function SinceLooked({ botId, posts, onQuote }: { botId: string; posts: FeedPost[]; onQuote: (p: FeedPost, apart: boolean) => Promise<void> }) {
  const [open, setOpen] = useState(posts.length === 1 ? posts[0].id : "");
  const [quoting, setQuoting] = useState("");
  if (!posts.length) return null;
  return (
    <section aria-label={t("Since you last looked")} className="rise rounded-card bg-surface p-1.5 shadow-card">
      <div className="flex items-center gap-2 px-2.5 pt-1.5 pb-1">
        <h3 className="text-[13px] font-medium text-ink">{t("Since you last looked")}</h3>
        <span className="text-[12.5px] text-ink-3">{t("{{count}} posts", { count: posts.length })}</span>
        <Link to="/bots/$botId/feed" params={{ botId }} className="ml-auto text-[12.5px] font-medium text-cobalt hover:text-cobalt-deep hover:underline">
          {t("All in Feed")}
        </Link>
      </div>
      <ul>
        {posts.map((p) => {
          const on = open === p.id;
          const src = sourceLabel(p);
          return (
            <li key={p.id} className={`rounded-control transition-colors duration-[160ms] ${on ? "bg-well" : "hover:bg-well"}`}>
              <button
                type="button"
                aria-expanded={on}
                onClick={() => setOpen(on ? "" : p.id)}
                className="flex w-full min-w-0 items-start gap-2.5 px-2.5 py-2 text-left"
              >
                <span aria-label={t("New")} className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" />
                <span className="min-w-0 flex-1">
                  <span className="flex min-w-0 items-baseline gap-2.5 text-[13px]">
                    <span className="truncate font-medium text-ink">{p.title || t("Post")}</span>
                    {src ? <span className="hidden shrink-0 truncate font-mono text-[11.5px] text-ink-3 sm:inline">{src}</span> : null}
                    <span className="ml-auto shrink-0 font-mono text-[11.5px] text-ink-3">{feedStamp(p.createdAt)}</span>
                  </span>
                  {!on ? <span className="mt-px block truncate text-[12.5px] text-ink-3">{p.body.replace(/[#*_`>]/g, "").slice(0, 200)}</span> : null}
                </span>
                <ChevronRight size={14} className={`mt-[3px] shrink-0 text-ink-3 transition-transform duration-[260ms] ease-quiet ${on ? "rotate-90" : ""}`} />
              </button>
              {on ? (
                <div className="px-2.5 pb-2 pl-[26px]">
                  <div className="silo-reply text-[15px] leading-[25px]">
                    <Md text={p.body} />
                  </div>
                  <Btn
                    kind="ghost"
                    size="sm"
                    className="mt-1 -ml-2.5"
                    disabled={quoting !== ""}
                    icon={<MessageSquareQuote size={13} />}
                    onClick={(e) => {
                      setQuoting(p.id);
                      void onQuote(p, e.metaKey || e.ctrlKey).finally(() => setQuoting(""));
                    }}
                  >
                    {t("Reply")}
                  </Btn>
                  <Btn
                    kind="ghost"
                    size="sm"
                    className="mt-1"
                    disabled={quoting !== ""}
                    onClick={() => {
                      setQuoting(p.id);
                      void onQuote(p, true).finally(() => setQuoting(""));
                    }}
                  >
                    {t("Reply in a new chat")}
                  </Btn>
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
