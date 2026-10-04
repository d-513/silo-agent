import { TriangleAlert } from "lucide-react";
import { lazy, Suspense } from "react";
import { SaveButton } from "../Feedback";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { AuditTable } from "./AuditTable";
import { ConnectorVarsPanel } from "./ConnectorVarsPanel";
import { FieldGroup } from "./FieldGroup";
import { ModelSettings } from "./ModelSettings";
import { SaveBar } from "./SaveBar";
import { BOOTSTRAP_NOTE, CONTEXT_NOTE, KNOWLEDGE_NOTE, MEMORY_NOTE, groupOf } from "./fields";
import { useAdminSettings } from "./useAdminSettings";

const YamlEditor = lazy(() => import("../YamlEditor").then((m) => ({ default: m.YamlEditor })));

export function AdminSettings() {
  const {
    fields, values, setValue, engines, providers, models, connVars, yamlText, setYamlText, yamlPath, audit,
    formSaver, yamlSaver, err, dirty, discard, saveForm, saveYaml, saveModels, saveConnVars,
  } = useAdminSettings();

  const envFields = fields.filter((f) => f.source === ConfigSource.ENV);
  const rowsIn = (id: string) => fields.filter((f) => groupOf(f.key) === id);
  const defaultModel = values["model"] ?? "";
  const titleModel = values["model_title"] ?? "";
  const approvalModel = values["model_approval"] ?? "";
  const subagentModel = values["model_subagent"] ?? "";
  const memoryModel = values["model_memory"] ?? "";
  const embedModel = values["embedding_model"] ?? "";
  const voiceModel = values["transcribe_model"] ?? "";

  return (
    <div>
      <div className="grid gap-5">
        <ModelSettings
          models={models}
          defaultModel={defaultModel}
          titleModel={titleModel}
          approvalModel={approvalModel}
          defaultField={fields.find((f) => f.key === "model")}
          titleField={fields.find((f) => f.key === "model_title")}
          approvalField={fields.find((f) => f.key === "model_approval")}
          subagentModel={subagentModel}
          subagentField={fields.find((f) => f.key === "model_subagent")}
          onSubagent={(v) => setValue("model_subagent", v)}
          memoryModel={memoryModel}
          memoryField={fields.find((f) => f.key === "model_memory")}
          onMemory={(v) => setValue("model_memory", v)}
          embedModel={embedModel}
          embedField={fields.find((f) => f.key === "embedding_model")}
          onEmbed={(v) => setValue("embedding_model", v)}
          voiceModel={voiceModel}
          voiceField={fields.find((f) => f.key === "transcribe_model")}
          onVoice={(v) => setValue("transcribe_model", v)}
          onDefault={(v) => setValue("model", v)}
          onTitle={(v) => setValue("model_title", v)}
          onApproval={(v) => setValue("model_approval", v)}
          onSaveModels={saveModels}
        />
        <ConnectorVarsPanel vars={connVars} onSave={saveConnVars} />
        {providers.map((p) => (
          <FieldGroup
            key={p.id}
            title={p.name}
            note={p.description || undefined}
            rows={rowsIn("providers").filter((f) => f.key.startsWith(`providers.${p.id}.`))}
            values={values}
            engines={engines}
            providers={providers}
            onChange={setValue}
          />
        ))}
        <FieldGroup title="Memory" note={MEMORY_NOTE} rows={rowsIn("memory")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Knowledge" note={KNOWLEDGE_NOTE} rows={rowsIn("knowledge")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Context" note={CONTEXT_NOTE} rows={rowsIn("context")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Search" rows={rowsIn("search")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Server" rows={rowsIn("server")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Bootstrap" note={BOOTSTRAP_NOTE} rows={rowsIn("bootstrap")} values={values} engines={engines} providers={providers} onChange={setValue} />
      </div>

      <SaveBar dirty={dirty} state={formSaver.state} error={err} onSave={() => void saveForm()} onDiscard={discard} />

      <h2 className="mt-10 mb-3 text-title">silo.yaml</h2>
      <p className="mb-2 font-mono text-[12px] text-ink-3">{yamlPath || "silo.yaml"}</p>
      {envFields.length > 0 ? (
        <p className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
          <TriangleAlert className="mt-0.5 shrink-0" size={16} />
          <span>Overridden by env and will not apply until unset: {envFields.map((f) => f.envName).join(", ")}</span>
        </p>
      ) : null}
      <Suspense fallback={<div className="h-80 rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface" />}>
        <YamlEditor
          value={yamlText}
          onChange={(v) => setYamlText(v)}
        />
      </Suspense>
      <div className="mt-3">
        <SaveButton state={yamlSaver.state} onClick={() => void saveYaml()}>
          Save YAML
        </SaveButton>
      </div>

      <h2 className="mt-10 mb-3 text-title">Audit</h2>
      <AuditTable audit={audit} />
    </div>
  );
}
