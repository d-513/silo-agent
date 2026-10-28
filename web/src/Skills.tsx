import { BookOpen, RotateCcw, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useMemo, useState, type ReactNode } from "react";
import { useParams } from "@tanstack/react-router";
import { ui } from "./api";
import { Btn } from "./Btn";
import { fail } from "./errors";
import { ArmedButton } from "./Feedback";
import { ErrorWell } from "./Field";
import { SkillBrowserOverlay } from "./FileBrowser";
import { skillSource } from "./fs";
import { UI, type Bot, type Skill } from "./gen/silo/v1/ui_pb";
import { ChooseBots, UsedBy, useManagedBots } from "./connectors/BotPick";
import { reload } from "./query";
import { widePage } from "./PageHead";
import { InstallPanel } from "./skills/InstallPanel";
import { filterSkills } from "./skills/model";
import { SkillCard } from "./skills/SkillCard";
import { TabPill, TabPills } from "./TabPills";
import { ToolbarSearch } from "./ToolbarSearch";
import { t } from "./i18n.ts";

function SkillGrid({ columns = 2, children }: { columns?: 1 | 2; children: ReactNode }) {
  return <div className={`grid grid-cols-1 gap-4 ${columns === 2 ? "md:grid-cols-2" : ""}`}>{children}</div>;
}

// A titled run of cards with its count, like a category in the connector library.
function Section({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2 border-b border-line/70 pb-2">
        <h3 className="text-sm font-semibold text-ink">{title}</h3>
        <span className="rounded-full bg-well px-2 py-0.5 text-[10px] font-medium text-ink-3">{count}</span>
      </div>
      {children}
    </section>
  );
}

function EmptyState({ title, children, actions }: { title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="rounded-card border border-dashed border-line bg-surface px-5 py-8 text-center wide:p-12">
      <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-well text-ink-3">
        <BookOpen size={28} />
      </div>
      <h3 className="text-base font-semibold text-ink">{title}</h3>
      <p className="mx-auto mt-1 max-w-md text-[13px] text-ink-2">{children}</p>
      {actions ? <div className="mt-6 flex flex-wrap items-center justify-center gap-3">{actions}</div> : null}
    </div>
  );
}

function NoMatch({ search, onClear }: { search: string; onClear: () => void }) {
  return (
    <div className="rounded-card bg-surface p-8 text-center text-ink-3 shadow-card">
      <p className="text-[13px]">{t("No skills match “{{search}}”.", { search })}</p>
      <button type="button" className="mt-2 text-[12px] font-medium text-cobalt hover:underline" onClick={onClear}>
        {t("Clear search")}
      </button>
    </div>
  );
}

function SkeletonCards() {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2" aria-busy="true" aria-label={t("Loading")}>
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="skeleton h-[140px] rounded-card" />
      ))}
    </div>
  );
}

function Header({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  return (
    <div className="mb-6 space-y-1.5">
      <div className="flex items-center gap-2.5">
        <h1 className="text-title text-ink">{title}</h1>
        {count !== undefined ? <span className="rounded-full bg-well px-2 py-0.5 text-[11px] font-semibold text-ink-3">{count}</span> : null}
      </div>
      <p className="max-w-3xl text-[13px] leading-relaxed text-ink-2">{children}</p>
    </div>
  );
}

export function SkillRows({
  rows,
  columns = 2,
  onRemove,
  onOpen,
  bots,
  onToggle,
}: {
  rows: Skill[];
  columns?: 1 | 2;
  // onRemove offers Remove on the rows the server says the caller may remove.
  onRemove?: (name: string) => void;
  onOpen: (s: Skill) => void;
  // In a Workspace: the Bots the caller manages, each a switch per skill.
  bots?: Bot[];
  onToggle?: (s: Skill, botId: string, on: boolean) => Promise<void>;
}) {
  return (
    <SkillGrid columns={columns}>
      {rows.map((s) => (
        <SkillCard
          key={s.kind + s.name}
          name={s.name}
          description={s.description}
          source={s.source}
          catalog={s.seeded}
          onOpen={() => onOpen(s)}
          actions={
            <>
              {bots && onToggle ? (
                <>
                  <UsedBy ids={s.usedBy} />
                  <ChooseBots bots={bots} granted={s.usedBy} onToggle={(botId, on) => onToggle(s, botId, on)} />
                </>
              ) : null}
              {onRemove && (s.kind === "library" || s.canRemove) ? (
                <ArmedButton
                  kind="ghost"
                  size="sm"
                  armedLabel={t("Click again to remove")}
                  icon={<Trash2 size={13} />}
                  onConfirm={() => onRemove(s.name)}
                >
                  {t("Remove")}
                </ArmedButton>
              ) : null}
            </>
          }
        />
      ))}
    </SkillGrid>
  );
}

function SkillPeek({ scope, name, spaceId = "", onClose }: { scope: string; name: string; spaceId?: string; onClose: () => void }) {
  const source = useMemo(() => skillSource(scope, name, spaceId), [scope, name, spaceId]);
  return <SkillBrowserOverlay key={scope + name} source={source} onClose={onClose} />;
}

export function AdminSkills() {
  const q = useQuery(UI.method.listSkills, { scope: "library" });
  const rows = q.data?.skills ?? null;
  const [actErr, setErr] = useState("");
  const err = actErr || (q.error ? fail(q.error) : "");
  const [open, setOpen] = useState("");
  const load = () => reload(UI.method.listSkills);
  async function seed() {
    setErr("");
    try {
      await ui.seedSkills({});
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  async function remove(name: string) {
    setErr("");
    try {
      await ui.deleteSkill({ scope: "library", name });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <h2 className="text-title">{t("Skills Library")}</h2>
        <Btn kind="secondary" type="button" onClick={() => void seed()} icon={<RotateCcw size={12} />}>
          {t("Re-add defaults")}
        </Btn>
      </div>
      <p className="mb-6 text-[13px] leading-relaxed text-ink-2">{t("Site skills every Bot can enable. Install from a GitHub URL, owner/repo, or a skill zip.")}</p>
      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}
      <InstallPanel scope="library" onDone={() => void load()} />
      {rows === null ? (
        <SkeletonCards />
      ) : rows.length === 0 ? (
        <EmptyState title={t("The library is empty")}>{t("Install a skill above, or re-add the defaults.")}</EmptyState>
      ) : (
        <SkillRows rows={rows} columns={1} onRemove={(n) => void remove(n)} onOpen={(s) => setOpen(s.name)} />
      )}
      {open ? <SkillPeek scope="library" name={open} onClose={() => setOpen("")} /> : null}
    </div>
  );
}

type Scope = "space" | "library";

// SpaceSkills is the Workspace's Skills tab in Customize: its own skills,
// which its Bots switch on, and the site-wide library beside them.
export function SpaceSkills() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/skills" });
  const [scope, setScope] = useState<Scope>("space");
  const spaceQ = useQuery(UI.method.listSkills, { scope: "space", spaceId });
  const libraryQ = useQuery(UI.method.listSkills, { scope: "library", spaceId });
  const bots = useManagedBots(spaceId) ?? [];
  const lists: Record<Scope, Skill[]> | null = spaceQ.data && libraryQ.data ? { space: spaceQ.data.skills, library: libraryQ.data.skills } : null;
  const [search, setSearch] = useState("");
  const [actErr, setErr] = useState("");
  const failed = spaceQ.error ?? libraryQ.error;
  const err = actErr || (failed ? fail(failed) : "");
  const [open, setOpen] = useState("");
  const load = () => reload(UI.method.listSkills);
  async function remove(name: string) {
    setErr("");
    try {
      await ui.deleteSkill({ scope: "space", spaceId, name });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  // A skill is switched on or off per Bot; the server keeps one name on per Bot.
  async function toggle(s: Skill, botId: string, on: boolean) {
    setErr("");
    try {
      await ui.setBotSkill({ botId, kind: s.kind, name: s.name, enabled: on });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  const rowProps = { onOpen: (s: Skill) => setOpen(s.name), bots, onToggle: toggle };

  const all = lists?.[scope] ?? [];
  const shown = useMemo(() => filterSkills(all, search), [all, search]);
  const catalog = shown.filter((s) => s.seeded);
  const added = shown.filter((s) => !s.seeded);

  return (
    <div className={widePage}>
      <Header title={t("Skills")}>
        {t("Skills are instructions and scripts a Bot loads when a task calls for them. Workspace skills belong to this workspace; Library skills are site-wide. Choose which of your Bots have each one on.")}
      </Header>

      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}

      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <TabPills>
          <TabPill on={scope === "space"} onClick={() => setScope("space")} count={lists?.space.length}>
            <span>{t("Workspace")}</span>
          </TabPill>
          <TabPill on={scope === "library"} onClick={() => setScope("library")} count={lists?.library.length}>
            <span>{t("Library")}</span>
          </TabPill>
        </TabPills>
        <ToolbarSearch value={search} onChange={setSearch} placeholder={t("Search skills...")} />
      </div>

      {scope === "space" ? (
        <InstallPanel
          scope="space"
          spaceId={spaceId}
          onDone={() => void load()}
        />
      ) : null}

      {lists === null ? (
        err ? null : <SkeletonCards />
      ) : all.length === 0 ? (
        scope === "space" ? (
          <EmptyState
            title={t("No workspace skills yet")}
            actions={
              <Btn kind="secondary" type="button" onClick={() => setScope("library")}>
                {t("Browse the Library")}
              </Btn>
            }
          >
            {t("Install one above to teach this workspace's Bots something new, or see what the Library already offers.")}
          </EmptyState>
        ) : (
          <EmptyState title={t("The Library is empty")}>{t("An admin can add site-wide skills in Admin → Skills Library.")}</EmptyState>
        )
      ) : shown.length === 0 ? (
        <NoMatch search={search} onClear={() => setSearch("")} />
      ) : scope === "space" ? (
        <SkillRows rows={shown} onRemove={(n) => void remove(n)} {...rowProps} />
      ) : catalog.length > 0 && added.length > 0 ? (
        <div className="space-y-7">
          <Section title={t("Catalog")} count={catalog.length}>
            <SkillRows rows={catalog} {...rowProps} />
          </Section>
          <Section title={t("Added by admins")} count={added.length}>
            <SkillRows rows={added} {...rowProps} />
          </Section>
        </div>
      ) : (
        <SkillRows rows={shown} {...rowProps} />
      )}
      {open ? <SkillPeek scope={scope} name={open} spaceId={spaceId} onClose={() => setOpen("")} /> : null}
    </div>
  );
}
