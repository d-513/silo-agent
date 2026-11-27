import { ListChecks, Plug, Workflow, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { AdminHead } from "./kit";
import { MODEL_PAGES, type ModelPage } from "./sections";
import { t } from "../i18n.ts";

const PAGE_ICONS: Record<ModelPage, LucideIcon> = { providers: Plug, allowed: ListChecks };

// ModelsHead opens each of the three pages alike: the name, the sentence of
// the page that is open, a figure where one helps, and the pages as tabs.
export function ModelsHead({ page, note, aside }: { page?: ModelPage; note: string; aside?: ReactNode }) {
  const tab = "relative flex h-9 shrink-0 items-center gap-2 text-small font-medium whitespace-nowrap transition-colors duration-[160ms] ease-quiet outline-offset-[-2px]";
  const look = (on: boolean) => `${tab} ${on ? "text-ink" : "text-ink-3 hover:text-ink"}`;
  const bar = (on: boolean) => <span aria-hidden className={`absolute inset-x-0 bottom-0 h-0.5 rounded-full bg-cobalt transition-opacity duration-200 ease-quiet ${on ? "opacity-100" : "opacity-0"}`} />;
  return (
    <AdminHead title={t("Models")} note={note} aside={aside}>
      <nav aria-label={t("Models")} className="silo-scroll-x mt-5 flex gap-6 shadow-[inset_0_-1px_0_var(--color-line)]">
        <Link to="/admin/settings/models" className={look(!page)} data-tab-on={!page || undefined} aria-current={!page ? "page" : undefined}>
          <Workflow size={14} />
          {t("Jobs")}
          {bar(!page)}
        </Link>
        {MODEL_PAGES.map((m) => {
          const Icon = PAGE_ICONS[m.id];
          const on = m.id === page;
          return (
            <Link key={m.id} to="/admin/settings/models/$page" params={{ page: m.id }} className={look(on)} data-tab-on={on || undefined} aria-current={on ? "page" : undefined}>
              <Icon size={14} />
              {t(m.label)}
              {bar(on)}
            </Link>
          );
        })}
      </nav>
    </AdminHead>
  );
}
