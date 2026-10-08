import { Paperclip } from "lucide-react";
import type { RefObject } from "react";
import type { CollectMemoriesResponse, ModelOption } from "../gen/silo/v1/ui_pb";
import { MemoryCollectButton } from "../MemoryCollect";
import { modelOptions } from "../modelOptions";
import { Select } from "../Select";
import { fitThinking } from "../thinking";
import { ThinkingPicker } from "../ThinkingPicker";
import type { Usage } from "../useRunStream";
import type { Dictation } from "../voice";
import { ContextMeter } from "./ContextMeter";
import { FollowToggle } from "./FollowToggle";
import { MicButton } from "./MicButton";
import { SendStop } from "./SendStop";

// The row under the textarea: attach, mic, model, thinking, the live-steps
// toggle, memory collection and the context meter on the left; the key hint and
// Send/Stop on the right.
export function Toolbar({
  attCount,
  fileInputRef,
  chatId,
  workerConnected,
  dictation,
  voice,
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
  workerConnected?: boolean;
  dictation: Dictation;
  // voice shows the dictation mic.
  voice: boolean;
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
  return (
    <div className="flex items-center gap-1 px-2.5 pt-1 pb-2.5">
      <div className="flex min-w-0 items-center gap-1">
        <button
          type="button"
          title={workerConnected ? "Attach files to /workspace/tmp" : "Start the Bot to attach files"}
          aria-label="Attach files"
          disabled={!workerConnected || !chatId}
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

        {voice ? <MicButton dictation={dictation} disabled={!chatId} /> : null}

        {models && models.length > 0 && onModel ? (
          <Select
            variant="ghost"
            className="min-w-0 max-w-[220px]"
            title="Model for this conversation"
            ariaLabel="Model for this conversation"
            value={model && models.some((m) => m.id === model) ? model : models[0].id}
            onChange={onModel}
            disabled={!chatId}
            options={modelOptions(models)}
          />
        ) : null}

        {thinkingLevels.length > 0 && onThinking ? (
          <ThinkingPicker value={fitThinking(thinking ?? "", thinkingLevels)} levels={thinkingLevels} onChange={onThinking} disabled={!chatId} />
        ) : null}

        <FollowToggle />

        {onCollect ? <MemoryCollectButton chatId={chatId} onCollect={onCollect} /> : null}

        {usage && usage.window > 0 ? <ContextMeter usage={usage} onCompact={onCompact} disabled={sending || !chatId} /> : null}
      </div>

      <span className="min-w-2 flex-1" />

      <span className="mr-1.5 hidden select-none text-[12px] text-ink-3 @min-[460px]:inline-grid" aria-hidden>
        <span className={`col-start-1 row-start-1 text-right transition-opacity duration-[240ms] ease-quiet ${sending ? "opacity-0" : "opacity-100"}`}>
          Enter to send
        </span>
        <span className={`col-start-1 row-start-1 text-right transition-opacity duration-[240ms] ease-quiet ${sending ? "opacity-100" : "opacity-0"}`}>
          Esc stops the reply
        </span>
      </span>

      <SendStop sending={sending} canSend={canSend} onStop={onStop} />
    </div>
  );
}
