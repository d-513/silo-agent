import { AudioLines, Binary, Braces, Cpu, Database, KeyRound, Mail, Plug, Scale, Search, Server, ShieldCheck, Split, Timer, Type, Variable, Waypoints, type LucideIcon } from "lucide-react";
import { useEffect, useRef } from "react";
import { Link, Outlet, useMatch, useParams } from "@tanstack/react-router";
import { ActiveBar } from "../bot/SideNav";
import { ErrorWell, SkeletonRows } from "../Field";
import { SaveBar } from "../SaveBar";
import { FIRST_SECTION, MODEL_PAGES, SECTIONS, isFormSection, isModelPage, isSection, type ModelPage, type SectionId } from "./sections";
import { SettingsFormContext, useAdminSettings } from "./useAdminSettings";
import { t } from "../i18n.ts";

const ICONS: Record<SectionId, LucideIcon> = {
  models: Cpu,
  search: Search,
  memory: Database,
  runs: Timer,
  connectors: Variable,
  rules: Scale,
  tunnels: Waypoints,
  mail: Mail,
  signin: KeyRound,
  server: Server,
  yaml: Braces,
  network: ShieldCheck,
};

// The pages under Models go by the kind of model they hold.
const MODEL_ICONS: Record<ModelPage, LucideIcon> = {
  providers: Plug,
  text: Type,
  decision: Split,
  embedding: Binary,
  voice: AudioLines,
};

// AdminSettings is the frame of Admin → Settings: the categories on the left
// and the open one beside them. It holds the form, so an edit survives a look
// at another category, and one Save bar covers them all.
export function AdminSettings() {
  const form = useAdminSettings();
  const { section, page } = useParams({ strict: false });
  const inModels = !!useMatch({ from: "/_authed/admin/settings/models", shouldThrow: false });
  const open: SectionId = inModels ? "models" : isSection(section) ? section : FIRST_SECTION;
  // The page under Models, none on the map itself.
  const sub = inModels && isModelPage(page) ? page : undefined;
  const inForm = isFormSection(open);

  return (
    <SettingsFormContext.Provider value={form}>
      <div className="flex gap-8 max-wide:flex-col max-wide:gap-4">
        <SectionList open={open} sub={sub} />
        <div className="min-w-0 flex-1">
          {inModels ? <ModelTabs sub={sub} /> : null}
          {!inForm && form.err ? <ErrorWell className="mb-4">{form.err}</ErrorWell> : null}
          {form.loading ? <SkeletonRows rows={4} height={88} /> : <Outlet />}
          {inForm ? <SaveBar dirty={form.dirty} state={form.formSaver.state} error={form.err} onSave={() => void form.saveForm()} onDiscard={form.discard} /> : null}
        </div>
      </div>
    </SettingsFormContext.Provider>
  );
}

// The categories: a column that stays in view on a wide window, one scrolling
// row of chips on a narrow one. Models holds its pages under it in the column;
// in the chip row they are a row of their own over the page (ModelTabs).
function SectionList({ open, sub }: { open: SectionId; sub?: ModelPage }) {
  const nav = useRef<HTMLElement>(null);
  // In the chip row the open category can sit past the edge: bring it in.
  useEffect(() => {
    nav.current?.querySelector("[data-tab-on], [data-parent-on]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [open]);
  return (
    <nav
      ref={nav}
      aria-label={t("Settings")}
      className="flex shrink-0 gap-0.5 max-wide:-mx-4 max-wide:overflow-x-auto max-wide:overscroll-x-contain max-wide:px-4 max-wide:[scrollbar-width:none] wide:sticky wide:top-7 wide:w-[196px] wide:flex-col wide:self-start max-wide:[&::-webkit-scrollbar]:hidden"
    >
      {SECTIONS.map((s) => {
        const Icon = ICONS[s.id];
        // On a page under Models, Models itself is the lit parent, not the page.
        const parent = s.id === open && !!sub;
        const on = s.id === open && !sub;
        return (
          <span key={s.id} className="contents">
            {s.id === "yaml" ? <span aria-hidden className="shrink-0 bg-line max-wide:mx-1.5 max-wide:h-6 max-wide:w-px max-wide:self-center wide:mx-3 wide:my-2 wide:h-px" /> : null}
            <Link
              to={s.id === "models" ? "/admin/settings/models" : "/admin/settings/$section"}
              params={s.id === "models" ? {} : { section: s.id }}
              data-tab-on={on || undefined}
              data-parent-on={parent || undefined}
              aria-current={on ? "page" : undefined}
              className={`relative flex h-9 shrink-0 items-center gap-2.5 rounded-control px-3 text-[13.5px] font-medium whitespace-nowrap transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px] ${
                on ? "bg-well text-ink" : parent ? "text-ink max-wide:bg-well hover:bg-well" : "text-ink-2 hover:bg-well hover:text-ink"
              }`}
            >
              <span className="contents max-wide:hidden">
                <ActiveBar on={on} />
              </span>
              <Icon size={15} className="shrink-0" />
              {t(s.label)}
            </Link>
            {s.id === "models" ? (
              // Its pages, on a hairline that hangs from it.
              <span className="relative mb-1.5 ml-[19px] flex flex-col gap-px pl-3 max-wide:hidden">
                <span aria-hidden className="absolute top-0.5 bottom-0.5 left-0 w-px bg-line-strong" />
                {MODEL_PAGES.map((m) => {
                  const lit = m.id === sub;
                  const Sub = MODEL_ICONS[m.id];
                  return (
                    <Link
                      key={m.id}
                      to="/admin/settings/models/$page"
                      params={{ page: m.id }}
                      data-tab-on={lit || undefined}
                      aria-current={lit ? "page" : undefined}
                      className={`relative flex h-8 items-center gap-2 rounded-control px-2.5 text-[13px] whitespace-nowrap transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px] ${
                        lit ? "bg-well font-medium text-ink" : "text-ink-2 hover:bg-well hover:text-ink"
                      }`}
                    >
                      {/* The lit page marks the hairline it hangs on. */}
                      <span aria-hidden className={`absolute top-1.5 bottom-1.5 -left-3 w-0.5 -translate-x-[0.5px] rounded-full bg-cobalt transition-opacity duration-200 ease-quiet ${lit ? "opacity-100" : "opacity-0"}`} />
                      <Sub size={14} className={`shrink-0 ${lit ? "text-ink" : "text-ink-3"}`} />
                      {t(m.label)}
                    </Link>
                  );
                })}
              </span>
            ) : null}
          </span>
        );
      })}
    </nav>
  );
}

// ModelTabs is the row of Models' pages over the page, on a narrow window
// only (a wide one has them in the column).
function ModelTabs({ sub }: { sub?: ModelPage }) {
  return (
    <nav aria-label={t("Models")} className="-mx-4 mb-4 flex gap-1 overflow-x-auto overscroll-x-contain px-4 [scrollbar-width:none] wide:hidden [&::-webkit-scrollbar]:hidden">
      <Tab to="/admin/settings/models" lit={!sub} icon={Cpu} label={t("Map")} />
      {MODEL_PAGES.map((m) => (
        <Tab key={m.id} to="/admin/settings/models/$page" page={m.id} lit={m.id === sub} icon={MODEL_ICONS[m.id]} label={t(m.label)} />
      ))}
    </nav>
  );
}

function Tab({ to, page, lit, icon: Icon, label }: { to: "/admin/settings/models" | "/admin/settings/models/$page"; page?: ModelPage; lit: boolean; icon: LucideIcon; label: string }) {
  return (
    <Link
      to={to}
      params={page ? { page } : {}}
      aria-current={lit ? "page" : undefined}
      className={`flex h-8 shrink-0 items-center gap-1.5 rounded-full px-3 text-[12.5px] font-medium whitespace-nowrap transition-[background-color,color,box-shadow] duration-[160ms] ease-quiet ${
        lit ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-well hover:text-ink"
      }`}
    >
      <Icon size={13} className="shrink-0" />
      {label}
    </Link>
  );
}
