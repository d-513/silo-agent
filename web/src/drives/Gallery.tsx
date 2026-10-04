import { ChevronRight, Search } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { inputClass } from "../Field";
import { PageHead } from "../PageHead";
import { Tip, TipAction, TipTitle } from "../Tip";
import type { DriveTemplate } from "../gen/silo/v1/ui_pb";
import { CATEGORY_LABEL, DriveMark } from "./DriveMark";

export function Gallery({ botId, templates, admin, onBack }: { botId: string; templates: DriveTemplate[]; admin: boolean; onBack: () => void }) {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase();
    return s ? templates.filter((t) => `${t.title} ${t.blurb} ${CATEGORY_LABEL[t.category] ?? ""}`.toLowerCase().includes(s)) : templates;
  }, [q, templates]);
  const groups = Object.keys(CATEGORY_LABEL)
    .map((c) => ({ c, items: shown.filter((t) => t.category === c) }))
    .filter((g) => g.items.length > 0);

  return (
    <div className="silo-page">
      <PageHead title="Add a drive" subtitle="Mount cloud storage or a file server as a folder the Bot can use." onBack={onBack} />
      <div className="relative mb-6">
        <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
        <input autoFocus className={`${inputClass} pl-9`} placeholder="Search providers" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search providers" />
      </div>
      {groups.length === 0 ? <p className="text-[13px] text-ink-3">Nothing matches “{q}”.</p> : null}
      <div className="space-y-6">
        {groups.map(({ c, items }) => (
          <section key={c}>
            <h3 className="mb-2.5 text-label-caps uppercase text-ink-3">{CATEGORY_LABEL[c]}</h3>
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              {items.map((t) => {
                const tile = (
                  <button
                    key={t.key}
                    type="button"
                    aria-disabled={!t.available || undefined}
                    className={`group flex items-center gap-3.5 rounded-card bg-surface p-3.5 text-left shadow-card transition-[box-shadow,transform] duration-[160ms] ease-quiet ${
                      t.available || admin ? "hover:-translate-y-px hover:shadow-float active:scale-[.995]" : "cursor-default"
                    }`}
                    onClick={() => {
                      if (t.available) navigate(`/bots/${botId}/drives/new/${t.key}`);
                      else if (admin) navigate("/admin/drives");
                    }}
                  >
                    <DriveMark svg={t.iconSvg} size={40} muted={!t.available} />
                    <div className="min-w-0 flex-1">
                      <div className={`text-[14px] font-medium leading-5 ${t.available ? "text-ink" : "text-ink-3"}`}>{t.title}</div>
                      <div className="truncate text-[12.5px] leading-[18px] text-ink-3">{t.available ? t.blurb : "Needs admin setup"}</div>
                    </div>
                    {t.available ? <ChevronRight size={15} className="shrink-0 text-ink-3 transition-transform duration-[160ms] ease-quiet group-hover:translate-x-0.5" /> : null}
                  </button>
                );
                return t.available ? (
                  tile
                ) : (
                  <Tip
                    key={t.key}
                    content={
                      <>
                        <TipTitle>{t.title} is not set up yet</TipTitle>
                        {admin ? (
                          <>
                            <p className="mt-1 text-[12px] leading-[17px] text-ink-2">It needs an OAuth client registered with {t.authLabel || t.title}, once for everyone.</p>
                            <TipAction>Click to set it up in Admin → Drives</TipAction>
                          </>
                        ) : (
                          <p className="mt-1 text-[12px] leading-[17px] text-ink-2">An admin has to enable it once, in Admin → Drives.</p>
                        )}
                      </>
                    }
                  >
                    {tile}
                  </Tip>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
