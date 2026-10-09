import { Braces, Cpu, Database, History, KeyRound, Plug, Search, Server, Timer, Variable, Waypoints, type LucideIcon } from "lucide-react";
import { useEffect, useRef } from "react";
import { Link, Outlet, useParams } from "@tanstack/react-router";
import { ActiveBar } from "../bot/SideNav";
import { ErrorWell, SkeletonRows } from "../Field";
import { SaveBar } from "./SaveBar";
import { FIRST_SECTION, SECTIONS, isFormSection, isSection, type SectionId } from "./sections";
import { SettingsFormContext, useAdminSettings } from "./useAdminSettings";

const ICONS: Record<SectionId, LucideIcon> = {
  models: Cpu,
  providers: Plug,
  search: Search,
  memory: Database,
  runs: Timer,
  connectors: Variable,
  tunnels: Waypoints,
  signin: KeyRound,
  server: Server,
  yaml: Braces,
  audit: History,
};

// AdminSettings is the frame of Admin → Settings: the categories on the left
// and the open one beside them. It holds the form, so an edit survives a look
// at another category, and one Save bar covers them all.
export function AdminSettings() {
  const form = useAdminSettings();
  const { section } = useParams({ strict: false });
  const open: SectionId = isSection(section) ? section : FIRST_SECTION;
  const inForm = isFormSection(open);

  return (
    <SettingsFormContext.Provider value={form}>
      <div className="flex gap-8 max-wide:flex-col max-wide:gap-4">
        <SectionList open={open} />
        <div className="min-w-0 flex-1">
          {!inForm && form.err ? <ErrorWell className="mb-4">{form.err}</ErrorWell> : null}
          {form.loading ? <SkeletonRows rows={4} height={88} /> : <Outlet />}
          {inForm ? <SaveBar dirty={form.dirty} state={form.formSaver.state} error={form.err} onSave={() => void form.saveForm()} onDiscard={form.discard} /> : null}
        </div>
      </div>
    </SettingsFormContext.Provider>
  );
}

// The categories: a column that stays in view on a wide window, one scrolling
// row of chips on a narrow one.
function SectionList({ open }: { open: SectionId }) {
  const nav = useRef<HTMLElement>(null);
  // In the chip row the open category can sit past the edge: bring it in.
  useEffect(() => {
    nav.current?.querySelector("[data-tab-on]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [open]);
  return (
    <nav
      ref={nav}
      aria-label="Settings"
      className="flex shrink-0 gap-0.5 max-wide:-mx-4 max-wide:overflow-x-auto max-wide:overscroll-x-contain max-wide:px-4 max-wide:[scrollbar-width:none] wide:sticky wide:top-7 wide:w-[196px] wide:flex-col wide:self-start max-wide:[&::-webkit-scrollbar]:hidden"
    >
      {SECTIONS.map((s) => {
        const on = s.id === open;
        const Icon = ICONS[s.id];
        return (
          <span key={s.id} className="contents">
            {s.id === "yaml" ? <span aria-hidden className="shrink-0 bg-line max-wide:mx-1.5 max-wide:h-6 max-wide:w-px max-wide:self-center wide:mx-3 wide:my-2 wide:h-px" /> : null}
            <Link
              to="/admin/settings/$section"
              params={{ section: s.id }}
              data-tab-on={on || undefined}
              aria-current={on ? "page" : undefined}
              className={`relative flex h-9 shrink-0 items-center gap-2.5 rounded-control px-3 text-[13.5px] font-medium whitespace-nowrap transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px] ${
                on ? "bg-well text-ink" : "text-ink-2 hover:bg-well hover:text-ink"
              }`}
            >
              <span className="contents max-wide:hidden">
                <ActiveBar on={on} />
              </span>
              <Icon size={15} className="shrink-0" />
              {s.label}
            </Link>
          </span>
        );
      })}
    </nav>
  );
}
