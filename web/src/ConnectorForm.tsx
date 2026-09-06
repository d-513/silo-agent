import { CaretRight, Plugs } from "@phosphor-icons/react";
import type { Connector } from "./gen/silo/v1/ui_pb";

export type HeaderDraft = { name: string; value: string };

export type ConnectorDraft = {
  name: string;
  description: string;
  httpUrl: string;
  auth: string;
  defaultMode: string;
  transport: string;
  headers: HeaderDraft[];
  oauthClientId: string;
  oauthClientSecret: string;
  hasOauthClientSecret: boolean;
  image?: Uint8Array;
  imageType: string;
  hasImage: boolean;
  clearImage: boolean;
  imageId?: string;
  previewUrl?: string;
};

export function emptyDraft(): ConnectorDraft {
  return {
    name: "",
    description: "",
    httpUrl: "",
    auth: "none",
    defaultMode: "ask",
    transport: "http",
    headers: [{ name: "", value: "" }],
    oauthClientId: "",
    oauthClientSecret: "",
    hasOauthClientSecret: false,
    imageType: "",
    hasImage: false,
    clearImage: false,
  };
}

export function draftFrom(c: Connector): ConnectorDraft {
  return {
    name: c.name,
    description: c.description,
    httpUrl: c.httpUrl,
    auth: c.auth || "none",
    defaultMode: c.defaultMode || "ask",
    transport: c.transport || "http",
    headers: c.headerKeys.length ? c.headerKeys.map((h) => ({ name: h.name, value: "" })) : [{ name: "", value: "" }],
    oauthClientId: c.oauthClientId || "",
    oauthClientSecret: "",
    hasOauthClientSecret: c.hasOauthClientSecret,
    imageType: "",
    hasImage: c.hasImage,
    clearImage: false,
    imageId: c.id,
  };
}

export function specOf(d: ConnectorDraft) {
  return {
    name: d.name,
    description: d.description,
    httpUrl: d.httpUrl,
    auth: d.auth,
    defaultMode: d.defaultMode,
    transport: d.transport,
    headers: d.headers.filter((h) => h.name.trim()),
    oauthClientId: d.oauthClientId,
    oauthClientSecret: d.oauthClientSecret,
    image: d.image,
    imageType: d.imageType,
    clearImage: d.clearImage,
  };
}

export function ConnectorMark({
  id,
  hasImage,
  previewUrl,
  size = 40,
}: {
  id?: string;
  hasImage: boolean;
  previewUrl?: string;
  size?: number;
}) {
  if (previewUrl) {
    return <img src={previewUrl} alt="" className="shrink-0 rounded object-cover" style={{ width: size, height: size }} />;
  }
  if (hasImage && id) {
    return (
      <img
        src={`/connectors/${id}/image`}
        alt=""
        className="shrink-0 rounded object-cover"
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      className="flex shrink-0 items-center justify-center rounded bg-cloth text-stone"
      style={{ width: size, height: size }}
    >
      <Plugs size={Math.round(size * 0.45)} />
    </span>
  );
}

export function McpChip() {
  return <span className="rounded bg-slate px-1.5 py-0.5 text-[11px] font-medium text-plaster">MCP</span>;
}

export function CustomChip() {
  return <span className="rounded bg-cloth px-1.5 py-0.5 text-[11px] font-medium text-stone">Custom</span>;
}

export function Segmented({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (v: string) => void;
  options: { id: string; label: string; disabled?: boolean; title?: string }[];
}) {
  return (
    <div className="flex overflow-hidden rounded border border-thread">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          disabled={o.disabled}
          title={o.title}
          className={`h-9 flex-1 ${o.disabled ? "bg-linen text-stone" : value === o.id ? "bg-bindery-pale" : "bg-folio text-stone"}`}
          onClick={() => onChange(o.id)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function ConnectorFields({
  value,
  onChange,
  existing,
  fromCatalog,
  catalogGuide,
}: {
  value: ConnectorDraft;
  onChange: (next: ConnectorDraft) => void;
  existing?: boolean;
  fromCatalog?: boolean;
  catalogGuide?: string;
}) {
  function set<K extends keyof ConnectorDraft>(k: K, v: ConnectorDraft[K]) {
    onChange({ ...value, [k]: v });
  }

  async function onFile(f: File | undefined) {
    if (!f) return;
    if (value.previewUrl) URL.revokeObjectURL(value.previewUrl);
    const buf = new Uint8Array(await f.arrayBuffer());
    onChange({
      ...value,
      image: buf,
      imageType: f.type,
      clearImage: false,
      hasImage: true,
      previewUrl: URL.createObjectURL(f),
    });
  }

  const settings = (
    <>
      <label className="mb-1 block text-[12px] font-medium text-stone">Name</label>
      <input className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3" value={value.name} onChange={(e) => set("name", e.target.value)} required={!fromCatalog} />
      {!fromCatalog && (
        <>
          <label className="mb-1 block text-[12px] font-medium text-stone">Description</label>
          <input className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3" value={value.description} onChange={(e) => set("description", e.target.value)} />
        </>
      )}
      <label className="mb-1 block text-[12px] font-medium text-stone">Image</label>
      <div className="mb-3 flex items-center gap-3">
        <ConnectorMark id={value.imageId} hasImage={value.hasImage && !value.clearImage} previewUrl={value.previewUrl} />
        <input type="file" accept="image/*" onChange={(e) => void onFile(e.target.files?.[0])} />
        {value.hasImage && !value.clearImage && (
          <button
            type="button"
            className="text-stone hover:text-carmine"
            onClick={() => {
              if (value.previewUrl) URL.revokeObjectURL(value.previewUrl);
              onChange({ ...value, clearImage: true, image: undefined, hasImage: false, previewUrl: undefined });
            }}
          >
            Remove
          </button>
        )}
      </div>
      <div className="mb-1 text-[12px] font-medium text-stone">Type</div>
      <p className="mb-3 text-stone">MCP</p>
      <div className="mb-1 text-[12px] font-medium text-stone">Transport</div>
      <div className="mb-3">
        <Segmented
          value={value.transport}
          onChange={(v) => set("transport", v)}
          options={[
            { id: "http", label: "HTTP" },
            { id: "stdio", label: "STDIO", disabled: true, title: "Coming later" },
          ]}
        />
      </div>
      <label className="mb-1 block text-[12px] font-medium text-stone">URL</label>
      <input className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3 font-mono" value={value.httpUrl} onChange={(e) => set("httpUrl", e.target.value)} placeholder="https://…" required={!fromCatalog} />
      <div className="mb-1 text-[12px] font-medium text-stone">Auth</div>
      <div className="mb-3">
        <Segmented
          value={value.auth}
          onChange={(v) => set("auth", v)}
          options={[
            { id: "none", label: "None" },
            { id: "oauth", label: "OAuth" },
          ]}
        />
      </div>
      {value.auth === "oauth" && (
        <>
          <p className="mb-2 text-stone">
            Leave blank when the server registers clients itself. GitHub and similar need a pre-registered OAuth App whose callback is <span className="font-mono">/oauth/callback</span> on this Silo.
          </p>
          <label className="mb-1 block text-[12px] font-medium text-stone">OAuth Client ID</label>
          <input
            className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3 font-mono"
            value={value.oauthClientId}
            onChange={(e) => set("oauthClientId", e.target.value)}
            autoComplete="off"
          />
          <label className="mb-1 block text-[12px] font-medium text-stone">OAuth Client Secret</label>
          <input
            className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3 font-mono"
            type="password"
            value={value.oauthClientSecret}
            onChange={(e) => set("oauthClientSecret", e.target.value)}
            placeholder={existing && value.hasOauthClientSecret ? "unchanged" : ""}
            autoComplete="new-password"
          />
        </>
      )}
      <div className="mb-1 text-[12px] font-medium text-stone">Default for tools</div>
      <p className="mb-2 text-stone">Used when there is no rule for an action. Rules can still override.</p>
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
      <div className="mb-1 text-[12px] font-medium text-stone">Extra headers</div>
      <p className="mb-2 text-stone">Values are stored on the Control Plane and never shown again.</p>
      {value.headers.map((h, i) => (
        <div key={i} className="mb-2 flex gap-2">
          <input
            className="h-9 w-40 rounded border border-thread bg-folio px-2 font-mono"
            placeholder="Name"
            value={h.name}
            onChange={(e) => set("headers", value.headers.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
          />
          <input
            className="h-9 min-w-0 flex-1 rounded border border-thread bg-folio px-2 font-mono"
            placeholder={existing ? "unchanged" : "Value"}
            type="password"
            value={h.value}
            onChange={(e) => set("headers", value.headers.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
          />
        </div>
      ))}
      <button type="button" className="mb-6 text-bindery" onClick={() => set("headers", [...value.headers, { name: "", value: "" }])}>
        Add header
      </button>
    </>
  );

  if (!fromCatalog) return settings;

  return (
    <>
      <div className="mb-5 flex items-center gap-4">
        <ConnectorMark id={value.imageId} hasImage={value.hasImage && !value.clearImage} previewUrl={value.previewUrl} size={72} />
        <div className="min-w-0">
          <div className="text-[22px] font-medium tracking-tight">{value.name}</div>
          <div className="mt-1">
            <McpChip />
          </div>
        </div>
      </div>
      {catalogGuide && (
        <div className="mb-5 rounded-[10px] border-l-4 border-bindery bg-cloth px-4 py-3">
          <p className="mb-2 text-[11px] font-medium uppercase tracking-[0.08em] text-bindery">Before you add</p>
          <p className="whitespace-pre-wrap text-[15px] leading-6 text-iron">{catalogGuide}</p>
        </div>
      )}
      <details className="group mb-6">
        <summary className="flex cursor-pointer items-center gap-2 rounded bg-cloth px-3 py-2 text-[12px] font-medium tracking-wide text-stone">
          <CaretRight size={12} className="shrink-0 transition-transform group-open:rotate-90" />
          Advanced settings
        </summary>
        <div className="mt-4">{settings}</div>
      </details>
    </>
  );
}
