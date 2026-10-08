import { Plug } from "lucide-react";

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
    return <img src={previewUrl} alt="" className="silo-brand shrink-0 rounded-sm object-cover" style={{ width: size, height: size }} />;
  }
  if (hasImage && id) {
    return (
      <img
        src={`/connectors/${id}/image`}
        alt=""
        className="silo-brand shrink-0 rounded-sm object-cover"
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      className="flex shrink-0 items-center justify-center rounded-sm bg-well text-ink-3"
      style={{ width: size, height: size }}
    >
      <Plug size={Math.round(size * 0.45)} />
    </span>
  );
}

export function McpChip({ transport }: { transport?: string }) {
  const label = transport === "builtin" ? "Built-in" : "MCP";
  return <span className="rounded-sm bg-ink-2 px-1.5 py-0.5 text-[11px] font-medium text-canvas">{label}</span>;
}

export function CustomChip() {
  return <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">Custom</span>;
}

export function CategoryChip({ label }: { label: string }) {
  if (!label) return null;
  return (
    <span className="inline-flex shrink-0 items-center whitespace-nowrap rounded-sm bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">
      {label}
    </span>
  );
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
    <div className="flex overflow-hidden rounded-sm border border-line">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          disabled={o.disabled}
          title={o.title}
          className={`h-9 flex-1 ${o.disabled ? "bg-pressed text-ink-3" : value === o.id ? "bg-cobalt-pale" : "bg-surface text-ink-3"}`}
          onClick={() => onChange(o.id)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
