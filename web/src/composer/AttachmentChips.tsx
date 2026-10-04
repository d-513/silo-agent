import { Paperclip, X } from "lucide-react";
import { fmtBytes } from "../format";

export interface Attachment {
  name: string;
  path: string;
  size: number;
}

// The files attached to the message being written, each with a remove button.
export function AttachmentChips({ atts, onRemove }: { atts: Attachment[]; onRemove: (path: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5 px-3 pt-3">
      {atts.map((a) => (
        <span
          key={a.path}
          title={a.path}
          className="rise inline-flex max-w-full items-center gap-1.5 rounded-sm bg-well py-1 pr-1 pl-2.5 text-[12px] text-ink"
        >
          <Paperclip size={12} className="shrink-0 text-ink-2" />
          <span className="max-w-[180px] truncate font-medium">{a.name}</span>
          <span className="shrink-0 font-mono text-[11px] text-ink-3">{fmtBytes(a.size)}</span>
          <button
            type="button"
            title="Remove attachment"
            className="flex h-5 w-5 items-center justify-center rounded-xs text-ink-2 transition-colors duration-[160ms] hover:bg-pressed hover:text-ink"
            onClick={() => onRemove(a.path)}
          >
            <X size={12} />
          </button>
        </span>
      ))}
    </div>
  );
}
