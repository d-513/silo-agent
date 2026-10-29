import { Paperclip } from "lucide-react";
import type { RefObject } from "react";
import type { CollectMemoriesResponse, ModelOption } from "../gen/silo/v1/ui_pb";
import { modelOptions } from "../modelOptions";
import { Select } from "../Select";
import { fitThinking } from "../thinking";
import { ThinkingPicker } from "../ThinkingPicker";
import type { Usage } from "../useRunStream";
import type { Dictation } from "../voice";
import { ConversationMenu } from "./ConversationMenu";
import { MicButton } from "./MicButton";
import { TalkButton } from "./TalkButton";
import { SendStop } from "./SendStop";
import { t } from "../i18n.ts";

// The row under the textarea, in three groups: what goes into the message
// (attach, dictate), who answers it (model, thinking), and on the right the
// conversation menu (context, compact, memories, live steps), Talk and Send/Stop.
export function Toolbar({
  attCount,
  fileInputRef,
  chatId,
  ready,
  workerConnected,
  dictation,
  voice,
  talk,
  botName,
  models,
  model,
  onModel,
  thinking,
  onThinking,
  onCollect,
  usage,
  onCompact,
  sending,
  canSend,
  onStop,
}: {
  attCount: number;
  fileInputRef: RefObject<HTMLInputElement | null>;
  chatId?: string;
  // A message can be written: a chat is open, or the empty one a send makes.
  ready: boolean;
  workerConnected?: boolean;
  dictation: Dictation;
  // voice shows the dictation mic.
  voice: boolean;
  // talk starts a call with the Bot in this chat.
  talk?: () => void;
  botName?: string;
  models?: ModelOption[];
  model?: string;
  onModel?: (model: string) => void;
  thinking?: string;
  onThinking?: (level: string) => void;
  onCollect?: () => Promise<CollectMemoriesResponse>;
  usage?: Usage | null;
  onCompact?: () => void;
  sending: boolean;
  canSend: boolean;
  onStop: () => void;
}) {
  const currentModel = models?.find((m) => m.id === model) ?? models?.[0];
  const thinkingLevels = currentModel?.thinkingLevels ?? [];
  const pickModel = models && models.length > 0 && onModel;
  const pickThinking = thinkingLevels.length > 0 && onThinking;
  return (
    <div className="flex items-center gap-2 px-2.5 pt-1 pb-2.5">
      {/* What goes into the message. */}
      <div className="flex shrink-0 items-center gap-0.5">
        <button
          type="button"
          title={workerConnected ? t("Attach files to /workspace/tmp") : t("Start the Bot to attach files")}
          aria-label={t("Attach files")}
          disabled={!workerConnected || !ready}
          className="relative flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-control text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:text-ink-3 disabled:opacity-50 disabled:hover:bg-transparent"
          onClick={() => fileInputRef.current?.click()}
        >
          <Paperclip size={17} />
          {attCount > 0 && (
            <span className="pop absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-ink px-1 text-[10px] font-semibold text-on-ink">
              {attCount}
            </span>
          )}
        </button>
        {voice ? <MicButton dictation={dictation} disabled={!ready} /> : null}
      </div>

      {/* Who answers it, and how hard it thinks. */}
      {pickModel || pickThinking ? (
        <div className="flex min-w-0 items-center gap-0.5">
          {pickModel ? (
            <Select
              variant="ghost"
              className="min-w-0 max-w-[200px]"
              title={t("Model for this conversation")}
              ariaLabel={t("Model for this conversation")}
              value={model && models.some((m) => m.id === model) ? model : models[0].id}
              onChange={onModel}
              disabled={!ready}
              options={modelOptions(models)}
            />
          ) : null}
          {pickThinking ? (
            <ThinkingPicker value={fitThinking(thinking ?? "", thinkingLevels)} levels={thinkingLevels} onChange={onThinking} disabled={!ready} />
          ) : null}
        </div>
      ) : null}

      <span className="flex-1" />

      {/* The conversation itself, a call, and Send. */}
      <div className="flex shrink-0 items-center gap-1">
        <ConversationMenu usage={usage} onCompact={onCompact} compactBlocked={sending || !chatId} chatId={chatId} onCollect={onCollect} />
        {talk ? <TalkButton onTalk={talk} botName={botName ?? ""} /> : null}
        <SendStop sending={sending} canSend={canSend} onStop={onStop} />
      </div>
    </div>
  );
}
