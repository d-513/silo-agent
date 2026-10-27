import { ChevronRight, Search } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { inputClass } from "../Field";
import { PageHead } from "../PageHead";
import { Tip, TipAction, TipTitle } from "../Tip";
import type { DriveTemplate } from "../gen/silo/v1/ui_pb";
import { CATEGORY_LABEL, DriveMark } from "./DriveMark";
import { t } from "../i18n.ts";

export function Gallery({ templates, admin, onPick, onBack }: { templates: DriveTemplate[]; admin: boolean; onPick: (key: string) => void; onBack: () => void }) {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase();
    return s ? templates.filter((tpl) => `${tpl.title} ${tpl.blurb} ${CATEGORY_LABEL[tpl.category] ? t(CATEGORY_LABEL[tpl.category]) : ""}`.toLowerCase().includes(s)) : templates;
  }, [q, templates]);
  const groups = Object.keys(CATEGORY_LABEL)
    .map((c) => ({ c, items: shown.filter((tpl) => tpl.category === c) }))
    .filter((g) => g.items.length > 0);

  return (
    <div className="silo-page">
      <PageHead title={t("Add a drive")} subtitle={t("Mount cloud storage or a file server as a folder the Bot can use.")} onBack={onBack} />
      <div className="relative mb-6">
        <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
        <input autoFocus className={`${inputClass} pl-9`} placeholder={t("Search providers")} value={q} onChange={(e) => setQ(e.target.value)} aria-label={t("Search providers")} />
      </div>
      {groups.length === 0 ? <p className="text-[13px] text-ink-3">{t("Nothing matches “{{search}}”.", { search: q })}</p> : null}
      <div className="space-y-6">
        {groups.map(({ c, items }) => (
          <section key={c}>
            <h3 className="mb-2.5 text-label-caps uppercase text-ink-3">{t(CATEGORY_LABEL[c])}</h3>
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              {items.map((tpl) => {
                const tile = (
                  <button
                    key={tpl.key}
                    type="button"
                    aria-disabled={!tpl.available || undefined}
                    className={`group flex items-center gap-3.5 rounded-card bg-surface p-3.5 text-left shadow-card transition-[box-shadow,transform] duration-[160ms] ease-quiet ${
                      tpl.available || admin ? "hover:-translate-y-px hover:shadow-float active:scale-[.995]" : "cursor-default"
                    }`}
                    onClick={() => {
                      if (tpl.available) onPick(tpl.key);
                      else if (admin) void navigate({ to: "/admin/drives" });
                    }}
                  >
                    <DriveMark svg={tpl.iconSvg} size={40} muted={!tpl.available} />
                    <div className="min-w-0 flex-1">
                      <div className={`text-[14px] font-medium leading-5 ${tpl.available ? "text-ink" : "text-ink-3"}`}>{tpl.title}</div>
                      <div className="truncate text-[12.5px] leading-[18px] text-ink-3">{tpl.available ? tpl.blurb : t("Needs admin setup")}</div>
                    </div>
                    {tpl.available ? <ChevronRight size={15} className="shrink-0 text-ink-3 transition-transform duration-[160ms] ease-quiet group-hover:translate-x-0.5" /> : null}
                  </button>
                );
                return tpl.available ? (
                  tile
                ) : (
                  <Tip
                    key={tpl.key}
                    content={
                      <>
                        <TipTitle>{t("{{title}} is not set up yet", { title: tpl.title })}</TipTitle>
                        {admin ? (
                          <>
                            <p className="mt-1 text-[12px] leading-[17px] text-ink-2">{t("It needs an OAuth client registered with {{provider}}, once for everyone.", { provider: tpl.authLabel || tpl.title })}</p>
                            <TipAction>{t("Click to set it up in Admin → Drives")}</TipAction>
                          </>
                        ) : (
                          <p className="mt-1 text-[12px] leading-[17px] text-ink-2">{t("An admin has to enable it once, in Admin → Drives.")}</p>
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
