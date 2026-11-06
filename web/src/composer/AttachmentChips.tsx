import { Paperclip, X } from "lucide-react";
import { Spinner } from "../Feedback";
import { fmtBytes } from "../format";
import { t } from "../i18n.ts";

export interface Attachment {
  name: string;
  path: string;
  size: number;
}

// A file still on its way to the Bot's tmp folder.
export interface Upload {
  key: string;
  name: string;
  size: number;
}

// The files attached to the message being written, each with a remove button,
// and those still uploading, each with a spinner.
export function AttachmentChips({ atts, uploading = [], onRemove }: { atts: Attachment[]; uploading?: Upload[]; onRemove: (path: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5 px-3 pt-3">
      {uploading.map((u) => (
        <span key={u.key} title={t("Uploading {{name}}…", { name: u.name })} className="inline-flex max-w-full items-center gap-1.5 rounded-sm bg-well py-1 pr-2 pl-2.5 text-label text-ink-2">
          <Spinner size={12} />
          <span className="max-w-[180px] truncate font-medium">{u.name}</span>
          <span className="shrink-0 font-mono text-micro text-ink-3">{fmtBytes(u.size)}</span>
        </span>
      ))}
      {atts.map((a) => (
        <span
          key={a.path}
          title={a.path}
          className="rise inline-flex max-w-full items-center gap-1.5 rounded-sm bg-well py-1 pr-1 pl-2.5 text-label text-ink"
        >
          <Paperclip size={12} className="shrink-0 text-ink-2" />
          <span className="max-w-[180px] truncate font-medium">{a.name}</span>
          <span className="shrink-0 font-mono text-micro text-ink-3">{fmtBytes(a.size)}</span>
          <button
            type="button"
            title={t("Remove attachment")}
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
