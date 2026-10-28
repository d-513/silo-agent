import { AudioLines } from "lucide-react";
import { Tip, TipTitle } from "../Tip";
import { t } from "../i18n.ts";

// TalkButton starts a call with the Bot in this chat: speak, and it answers
// out loud. What is said is written into the chat.
export function TalkButton({ onTalk, botName }: { onTalk: () => void; botName: string }) {
  return (
    <Tip
      content={
        <>
          <TipTitle>{t("Talk with {{name}}", { name: botName || t("the Bot") })}</TipTitle>
          <p className="mt-1">{t("A voice call in this chat. What you both say is written here.")}</p>
        </>
      }
    >
      <button
        type="button"
        aria-label={t("Talk with {{name}}", { name: botName || t("the Bot") })}
        className="flex h-[34px] shrink-0 items-center gap-1.5 rounded-control px-2 text-[13px] font-medium text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms]"
        onClick={onTalk}
      >
        <AudioLines size={17} />
        <span className="hidden @min-[520px]:inline">{t("Talk")}</span>
      </button>
    </Tip>
  );
}
