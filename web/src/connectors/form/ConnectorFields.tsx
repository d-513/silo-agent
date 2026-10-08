import { ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { ToggleRow } from "../../Switch";
import { CategoryChip, ConnectorMark, McpChip, Segmented } from "./atoms";
import { BuiltinConfig } from "./BuiltinConfig";
import { withImage, withName, withoutImage, type ConnectorDraft } from "./draft";
import { identTyped } from "./ident";
import { EnvEditor, HeadersEditor } from "./ListEditors";
import { TextRow } from "./TextRow";
import { TransportFields } from "./Transport";

type Edit = <K extends keyof ConnectorDraft>(k: K, v: ConnectorDraft[K]) => void;

const categorySuggestions = [
  "Developer Tools",
  "Project Management",
  "Productivity",
  "Finance",
  "DevOps & Monitoring",
  "Documentation",
  "Communication",
  "Custom",
];

// Collapsed by default: the fields most people never need to touch.
function AdvancedSettings({ children }: { children: ReactNode }) {
  return (
    <details className="group mb-6">
      <summary className="flex cursor-pointer items-center gap-2 rounded-sm bg-well px-3 py-2 text-[12px] font-medium tracking-wide text-ink-3">
        <ChevronRight size={12} className="shrink-0 transition-transform group-open:rotate-90" />
        Advanced settings
      </summary>
      <div className="mt-4">{children}</div>
    </details>
  );
}

function PromptField({ value, set }: { value: ConnectorDraft; set: Edit }) {
  return (
    <>
      <label className="mb-1 block text-[12px] font-medium text-ink-3">Prompt</label>
      <p className="mb-2 text-ink-2">Extra instructions added to the system prompt while this connector is authorized and its tools are ready.</p>
      <textarea
        className="mb-3 min-h-24 w-full rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3 py-2"
        value={value.prompt}
        onChange={(e) => set("prompt", e.target.value)}
        placeholder="When to use this connector, and how."
      />
    </>
  );
}

// The connector's picture: a preview, a file picker and a way to drop it.
function ImageField({ value, onChange }: { value: ConnectorDraft; onChange: (next: ConnectorDraft) => void }) {
  async function onFile(f: File | undefined) {
    if (!f) return;
    if (value.previewUrl) URL.revokeObjectURL(value.previewUrl);
    const buf = new Uint8Array(await f.arrayBuffer());
    onChange(withImage(value, buf, f.type, URL.createObjectURL(f)));
  }
  return (
    <>
      <label className="mb-1 block text-[12px] font-medium text-ink-3">Image</label>
      <div className="mb-3 flex items-center gap-3">
        <ConnectorMark id={value.imageId} hasImage={value.hasImage && !value.clearImage} previewUrl={value.previewUrl} />
        <input type="file" accept="image/*" onChange={(e) => void onFile(e.target.files?.[0])} />
        {value.hasImage && !value.clearImage && (
          <button
            type="button"
            className="text-ink-2 hover:text-vermilion"
            onClick={() => {
              if (value.previewUrl) URL.revokeObjectURL(value.previewUrl);
              onChange(withoutImage(value));
            }}
          >
            Remove
          </button>
        )}
      </div>
    </>
  );
}

// What happens to an action no rule covers.
function DefaultModeField({ value, set, builtin }: { value: ConnectorDraft; set: Edit; builtin: boolean }) {
  return (
    <>
      <div className="mb-1 text-[12px] font-medium text-ink-3">Default for tools</div>
      <p className="mb-2 text-ink-2">
        {builtin
          ? "Each action already has its own default (reading allowed, sending asks); this covers anything else. Rules can still override."
          : "Used when there is no rule for an action. Rules can still override."}
      </p>
      <div className="mb-3">
        <Segmented
          value={value.defaultMode}
          onChange={(v) => set("defaultMode", v)}
          options={[
            { id: "allow", label: "Allow" },
            { id: "ask", label: "Ask" },
            { id: "deny", label: "Deny" },
          ]}
        />
      </div>
    </>
  );
}

// The catalog header: a big mark, the name, and what kind of connector it is.
function CatalogHeader({ value, guide }: { value: ConnectorDraft; guide?: string }) {
  return (
    <>
      <div className="mb-5 flex items-center gap-4">
        <ConnectorMark id={value.imageId} hasImage={value.hasImage && !value.clearImage} previewUrl={value.previewUrl} size={72} />
        <div className="min-w-0">
          <div className="text-title">{value.name}</div>
          <div className="mt-1 flex items-center gap-2">
            <McpChip transport={value.builtin ? "builtin" : value.transport} />
            {value.category && <CategoryChip label={value.category} />}
          </div>
        </div>
      </div>
      {guide && (
        <div className="mb-5 rounded-card border-l-4 border-cobalt bg-well px-4 py-3">
          <p className="mb-2 text-label-caps uppercase text-ink-3">Before you add</p>
          <p className="whitespace-pre-wrap text-[15px] leading-6 text-ink">{guide}</p>
        </div>
      )}
    </>
  );
}

export function ConnectorFields({
  value,
  onChange,
  existing,
  fromCatalog,
  catalogGuide,
  allowStdioImage,
  allowAutoAttach,
  allowIdentifier,
  showConfig = true,
}: {
  value: ConnectorDraft;
  onChange: (next: ConnectorDraft) => void;
  existing?: boolean;
  fromCatalog?: boolean;
  catalogGuide?: string;
  allowStdioImage?: boolean;
  allowAutoAttach?: boolean;
  // Library presets carry an identifier; a Bot's own connector has none.
  allowIdentifier?: boolean;
  // A library preset holds no account, so the admin form hides the config.
  showConfig?: boolean;
}) {
  const builtin = !!value.builtin;
  const set: Edit = (k, v) => onChange({ ...value, [k]: v });

  const settings = (
    <>
      <TextRow
        label="Name"
        value={value.name}
        onChange={(e) => onChange(withName(value, e.target.value, !!allowIdentifier && !existing))}
        required={!fromCatalog}
      />
      {allowIdentifier && (
        <TextRow
          label="Identifier"
          hint={
            <>
              What this preset is called in <span className="font-mono">autoenable_connectors</span>. Lowercase letters, digits and
              underscores; no two presets share one.
            </>
          }
          mono
          value={value.identifier}
          onChange={(e) => set("identifier", identTyped(e.target.value))}
          placeholder="fal_ai"
          spellCheck={false}
          autoCapitalize="off"
          required
        />
      )}
      {!fromCatalog && (
        <>
          <TextRow label="Description" value={value.description} onChange={(e) => set("description", e.target.value)} />
          <TextRow
            label="Category"
            value={value.category}
            onChange={(e) => set("category", e.target.value)}
            placeholder="e.g. Developer Tools, Productivity, Custom"
            list="connector-category-suggestions"
          />
          <datalist id="connector-category-suggestions">
            {categorySuggestions.map((c) => (
              <option key={c} value={c} />
            ))}
          </datalist>
        </>
      )}
      <PromptField value={value} set={set} />
      <ImageField value={value} onChange={onChange} />
      <div className="mb-1 text-[12px] font-medium text-ink-3">Type</div>
      <p className="mb-3 text-ink-2">{builtin ? "Built-in — runs on the Silo server" : "MCP"}</p>
      {!builtin && <TransportFields value={value} set={set} existing={existing} fromCatalog={fromCatalog} allowStdioImage={allowStdioImage} />}
      <DefaultModeField value={value} set={set} builtin={builtin} />
      {allowAutoAttach && (
        <ToggleRow
          className="mb-3 rounded-card shadow-card bg-surface px-3 py-2.5"
          label="Add to new bots by default"
          hint={
            value.autoenabled ? (
              <>
                On for every new bot: <span className="font-mono">autoenable_connectors</span> names this preset (Settings → Connector
                options), whatever this switch says.
              </>
            ) : (
              "New bots get this connector automatically. Removing it from a bot does not re-add it."
            )
          }
          on={value.autoAttach || value.autoenabled}
          disabled={value.autoenabled}
          onChange={(v) => set("autoAttach", v)}
        />
      )}
      {builtin ? null : value.transport === "stdio" ? <EnvEditor value={value} existing={existing} set={set} /> : <HeadersEditor value={value} existing={existing} set={set} />}
    </>
  );

  const config = builtin && showConfig ? <BuiltinConfig value={value} onChange={onChange} /> : null;

  if (!fromCatalog) {
    if (!config) return settings;
    return (
      <>
        {config}
        <AdvancedSettings>{settings}</AdvancedSettings>
      </>
    );
  }

  return (
    <>
      <CatalogHeader value={value} guide={catalogGuide} />
      {config}
      <AdvancedSettings>{settings}</AdvancedSettings>
    </>
  );
}
