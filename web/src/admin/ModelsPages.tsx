import { useState } from "react";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { btnClass } from "../Btn";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { TabPill, TabPills } from "../TabPills";
import { AllowedModels } from "./AllowedModels";
import { Band, Group, Groups, Notice } from "./kit";
import { ModelsHead } from "./ModelsHead";
import { ModelPicker } from "./ModelPicker";
import { ProviderSettings } from "./ProviderSettings";
import { ProviderMap } from "./ProviderMap";
import { accountsOf, isReady, resolveJobs, type JobDef } from "./providerJobs";
import { KINDS, kindWord, type ModelKind } from "./providerModels";
import { isModelPage } from "./sections";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

// The Models part of Admin → Settings, as three pages under one head: Jobs
// (the map, where every job's model is picked), Providers (who serves them),
// and Allowed models (what a chat and a Bot may pick from).

// ModelMapPage is Jobs: every job and the provider that does it. A job is
// picked right here, so this page alone is enough to set Silo's models up.
export function ModelMapPage() {
  const { providers, values, fields } = useSettingsForm();
  const navigate = useNavigate();
  const [picking, setPicking] = useState<JobDef | null>(null);
  const jobs = resolveJobs(values);
  const ready = (id: string) => isReady(id, fields, values, ConfigSource.ENV, accountsOf(providers));
  const none = providers.length > 0 && !providers.some((p) => ready(p.id));

  return (
    <>
      <ModelsHead note={t("Which model does each job. Press a job to pick its model; the line shows who serves it.")} />
      <Groups>
        <Group>
          {none ? (
            <Notice>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                <span className="min-w-0 flex-1 basis-[240px]">{t("No provider is set up yet. Give one its key first, then pick a model for each job.")}</span>
                <Link to="/admin/settings/models/$page" params={{ page: "providers" }} className={btnClass("primary", "", "sm")}>
                  {t("Set up a provider")}
                </Link>
              </div>
            </Notice>
          ) : null}
          <ProviderMap
            jobs={jobs}
            providers={providers}
            ready={ready}
            onJob={(j) => setPicking(j)}
            onPick={(id) => void navigate({ to: "/admin/settings/models/$page", params: { page: "providers" }, search: { provider: id } })}
          />
          <Band className="flex flex-wrap items-center gap-x-5 gap-y-1.5 text-label text-ink-3">
            <span className="flex items-center gap-2">
              <span aria-hidden className="h-px w-5 bg-ink-3" />
              {t("Set for this job")}
            </span>
            <span className="flex items-center gap-2">
              <span aria-hidden className="w-5 border-t border-dashed border-ink-3" />
              {t("Follows another setting")}
            </span>
            <span className="flex items-center gap-2">
              <span aria-hidden className="h-[6px] w-[6px] rounded-full bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]" />
              {t("Nobody does it")}
            </span>
          </Band>
        </Group>
      </Groups>
      {picking ? <ModelPicker job={picking} onClose={() => setPicking(null)} /> : null}
    </>
  );
}

export function ModelsPage() {
  const { page } = useParams({ from: "/_authed/admin/settings/models/$page" });
  if (!isModelPage(page)) return null;
  return page === "providers" ? <ProviderSettings /> : <AllowedPage />;
}

// AllowedPage is the allowlists, one kind at a time: what a chat's model
// menu, a Bot's settings and the tools may name beyond the jobs' own models.
function AllowedPage() {
  const { allowedOf } = useSettingsForm();
  const { kind: asked } = useSearch({ from: "/_authed/admin/settings/models/$page" });
  const navigate = useNavigate();
  const kind: ModelKind = (KINDS as string[]).includes(asked ?? "") ? (asked as ModelKind) : "text";
  return (
    <>
      <ModelsHead page="allowed" note={t("What a chat, a Bot and the tools may pick from, beyond the models the jobs use. A change here is saved at once.")} />
      <div className="silo-scroll-x mb-6 flex">
        <TabPills>
          {KINDS.map((k) => (
            <TabPill key={k} on={k === kind} count={allowedOf(k).length} onClick={() => void navigate({ to: "/admin/settings/models/$page", params: { page: "allowed" }, search: { kind: k }, replace: true })}>
              <span className="whitespace-nowrap">{kindWord(k)}</span>
            </TabPill>
          ))}
        </TabPills>
      </div>
      <Groups>
        <AllowedModels kind={kind} />
      </Groups>
    </>
  );
}
