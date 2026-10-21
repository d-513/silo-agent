import { X } from "lucide-react";
import { useState } from "react";
import { SendStop } from "../composer/SendStop";
import { t } from "../i18n.ts";

// AskBar is how the owner changes the Dashboard: words to the Bot, sent to the
// Dashboard automation's log rather than a chat. A picked widget rides along
// as a chip, so "split it per server" needs no more context.
export function AskBar({
  botName,
  picked,
  onUnpick,
  draft,
  setDraft,
  onAsk,
  big = false,
}: {
  botName: string;
  picked?: { id: string; title: string };
  onUnpick: () => void;
  draft: string;
  setDraft: (s: string) => void;
  onAsk: (text: string, widgetId: string) => Promise<boolean>;
  big?: boolean;
}) {
  const [sending, setSending] = useState(false);
  const can = draft.trim() !== "" && !sending;
  async function send() {
    if (!can) return;
    setSending(true);
    try {
      if (await onAsk(draft.trim(), picked?.id ?? "")) setDraft("");
    } finally {
      setSending(false);
    }
  }
  const placeholder = big
    ? t("What should {{name}} watch for you?", { name: botName })
    : picked
      ? t("What should change about it?")
      : t("Change the board…");
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void send();
      }}
      className="flex items-end gap-2 rounded-[18px] bg-surface py-2 pr-2 pl-3 shadow-float transition-shadow duration-[160ms] focus-within:shadow-float-focus"
    >
      {picked ? (
        <span className="mb-1 inline-flex h-[26px] max-w-[40%] shrink-0 items-center gap-1 rounded-sm bg-cobalt-pale pr-1 pl-2 text-[12.5px] font-medium text-cobalt">
          <span className="truncate">{picked.title}</span>
          <button type="button" aria-label={t("Remove")} onClick={onUnpick} className="grid h-5 w-5 place-items-center rounded-xs hover:bg-surface/60">
            <X size={12} />
          </button>
        </span>
      ) : null}
      <textarea
        rows={1}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void send();
          }
          if (e.key === "Escape" && picked) onUnpick();
        }}
        placeholder={placeholder}
        aria-label={placeholder}
        className="max-h-[120px] min-h-[34px] flex-1 resize-none bg-transparent py-[5px] text-[15px] leading-6 text-ink outline-none [field-sizing:content] placeholder:text-ink-3"
      />
      <SendStop sending={false} canSend={can} onStop={() => {}} />
    </form>
  );
}
