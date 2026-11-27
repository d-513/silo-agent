import { AudioLines, Braces, Cpu, Database, KeyRound, LockKeyhole, Mail, Scale, Search, Server, ShieldCheck, Timer, Variable, Waypoints, type LucideIcon } from "lucide-react";
import { useEffect, useRef } from "react";
import { Link, Outlet, useMatch, useParams } from "@tanstack/react-router";
import { ActiveBar } from "../bot/SideNav";
import { ErrorWell, SkeletonRows } from "../Field";
import { SaveBar } from "../SaveBar";
import { FIRST_SECTION, SECTIONS, isFormSection, isSection, type SectionId } from "./sections";
import { SettingsFormContext, useAdminSettings } from "./useAdminSettings";
import { t } from "../i18n.ts";

const ICONS: Record<SectionId, LucideIcon> = {
  models: Cpu,
  search: Search,
  memory: Database,
  runs: Timer,
  voice: AudioLines,
  connectors: Variable,
  rules: Scale,
  tunnels: Waypoints,
  mail: Mail,
  signin: KeyRound,
  server: Server,
  yaml: Braces,
  network: ShieldCheck,
  https: LockKeyhole,
};

// AdminSettings is the frame of Admin → Settings: the categories in a column
// of their own beside the open one. It holds the form, so an edit survives a
// look at another category, and one Save bar covers them all.
export function AdminSettings() {
  const form = useAdminSettings();
  const { section } = useParams({ strict: false });
  const inModels = !!useMatch({ from: "/_authed/admin/settings/models", shouldThrow: false });
  const open: SectionId = inModels ? "models" : isSection(section) ? section : FIRST_SECTION;
  const inForm = isFormSection(open);

  return (
    <SettingsFormContext.Provider value={form}>
      <div className="flex min-h-full max-wide:flex-col">
        <SectionList open={open} />
        <div className="silo-admin-form">
          {!inForm && form.err ? <ErrorWell className="mb-4">{form.err}</ErrorWell> : null}
          {form.loading ? <SkeletonRows rows={4} height={88} /> : <Outlet />}
          {inForm ? <SaveBar dirty={form.dirty} state={form.formSaver.state} error={form.err} onSave={() => void form.saveForm()} onDiscard={form.discard} /> : null}
        </div>
      </div>
    </SettingsFormContext.Provider>
  );
}

// The categories: a column that stays in view on a wide window, one scrolling
// row of the same rows on a narrow one.
function SectionList({ open }: { open: SectionId }) {
  const nav = useRef<HTMLElement>(null);
  // In the narrow row the open category can sit past the edge: bring it in.
  useEffect(() => {
    nav.current?.querySelector("[data-tab-on]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [open]);
  return (
    <nav
      ref={nav}
      aria-label={t("Settings")}
      className="flex shrink-0 gap-0.5 max-wide:overflow-x-auto max-wide:overscroll-x-contain max-wide:px-4 max-wide:pt-3 max-wide:[scrollbar-width:none] wide:sticky wide:top-0 wide:max-h-screen wide:w-[228px] wide:flex-col wide:self-start wide:overflow-y-auto wide:pt-[30px] wide:pr-3 wide:pb-6 wide:pl-5 max-wide:[&::-webkit-scrollbar]:hidden"
    >
      <h2 className="mb-1 flex h-7 shrink-0 items-center px-3 text-section text-ink-3 max-wide:hidden">{t("Settings")}</h2>
      {SECTIONS.map((s) => {
        const Icon = ICONS[s.id];
        const on = s.id === open;
        return (
          <span key={s.id} className="contents">
            {s.id === "yaml" ? <span aria-hidden className="shrink-0 bg-line max-wide:mx-1.5 max-wide:h-6 max-wide:w-px max-wide:self-center wide:mx-3 wide:my-2 wide:h-px" /> : null}
            <Link
              to={s.id === "models" ? "/admin/settings/models" : "/admin/settings/$section"}
              params={s.id === "models" ? {} : { section: s.id }}
              data-tab-on={on || undefined}
              aria-current={on ? "page" : undefined}
              className={`relative flex h-9 shrink-0 items-center gap-2.5 rounded-control px-3 text-row font-medium whitespace-nowrap transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px] ${
                on ? "bg-well text-ink" : "text-ink-2 hover:bg-well hover:text-ink"
              }`}
            >
              <span className="contents max-wide:hidden">
                <ActiveBar on={on} />
              </span>
              <Icon size={15} className={`shrink-0 ${on ? "text-ink" : "text-ink-3"}`} />
              {t(s.label)}
            </Link>
          </span>
        );
      })}
    </nav>
  );
}
