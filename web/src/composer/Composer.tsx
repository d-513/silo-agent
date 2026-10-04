import { Upload } from "lucide-react";
import { useRef } from "react";
import type { CollectMemoriesResponse, ModelOption } from "../gen/silo/v1/ui_pb";
import type { Usage } from "../useRunStream";
import { AttachmentChips, type Attachment } from "./AttachmentChips";
import { DictationStrip } from "./DictationStrip";
import { placeholderFor } from "./model";
import { Notice } from "./Notice";
import { Toolbar } from "./Toolbar";
import { useComposerInput } from "./useComposerInput";
import { useDictationInput } from "./useDictationInput";

export type { Attachment } from "./AttachmentChips";

export interface ComposerProps {
  text: string;
  setText: (val: string | ((prev: string) => string)) => void;
  atts: Attachment[];
  onRemoveAtt: (path: string) => void;
  attachErr?: string;
  onAttach: (files: FileList | null) => Promise<void> | void;
  onSend: () => void;
  onStop: () => void;
  sending: boolean;
  chatId?: string;
  workerConnected?: boolean;
  botName?: string;
  models?: ModelOption[];
  model?: string;
  onModel?: (model: string) => void;
  // thinking is the chat's chosen level ("" is the model default); the
  // picker shows only when the current model lists levels.
  thinking?: string;
  onThinking?: (level: string) => void;
  usage?: Usage | null;
  // onCompact summarizes the conversation so far (the context meter's click).
  onCompact?: () => void;
  // voice shows the dictation mic; onTranscribe turns a recording into text.
  voice?: boolean;
  onTranscribe?: (audio: Uint8Array, mime: string) => Promise<string>;
  // onCollect runs the memory collector over this chat now.
  onCollect?: () => Promise<CollectMemoriesResponse>;
}

// The message box: attachments, the textarea and its toolbar, with drag-and-drop
// and dictation around it.
export function Composer({
  text,
  setText,
  atts,
  onRemoveAtt,
  attachErr,
  onAttach,
  onSend,
  onStop,
  sending,
  chatId,
  workerConnected,
  botName,
  models,
  model,
  onModel,
  thinking,
  onThinking,
  usage,
  onCompact,
  voice,
  onTranscribe,
  onCollect,
}: ComposerProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const dictation = useDictationInput(textareaRef, text, setText, onTranscribe);
  const input = useComposerInput({
    textareaRef,
    text,
    attCount: atts.length,
    chatId,
    workerConnected,
    sending,
    recording: dictation.state === "recording",
    onSend,
    onStop,
    onAttach,
  });

  return (
    <div className="shrink-0 px-4 pt-1 pb-4 max-wide:pb-[max(1rem,env(safe-area-inset-bottom))]">
      <div className="mx-auto max-w-[720px]">
        <form
          onSubmit={input.onSubmit}
          className={`@container relative flex flex-col rounded-[18px] bg-surface transition-shadow duration-[200ms] ease-quiet ${
            input.isDragging ? "shadow-[0_0_0_1px_var(--color-emerald)]" : "shadow-float focus-within:shadow-float-focus"
          }`}
          onDragOver={input.onDragOver}
          onDragLeave={input.onDragLeave}
          onDrop={input.onDrop}
        >
          {input.isDragging && (
            <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center gap-2 rounded-[18px] bg-surface text-[13px] font-medium text-emerald">
              <Upload size={18} />
              <span>Drop files to attach to /workspace/tmp</span>
            </div>
          )}

          {atts.length > 0 && <AttachmentChips atts={atts} onRemove={onRemoveAtt} />}

          {attachErr && <Notice text={attachErr} />}

          {dictation.error && dictation.state === "idle" && <Notice text={dictation.error} onDismiss={dictation.clearError} />}

          {dictation.state !== "idle" && <DictationStrip dictation={dictation} />}

          <textarea
            ref={textareaRef}
            rows={1}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={input.onKeyDown}
            onPaste={input.onPaste}
            placeholder={placeholderFor(chatId, botName)}
            disabled={!chatId}
            className="silo-grow block w-full resize-none bg-transparent px-4 pt-3.5 pb-1 text-[15px] leading-6 text-ink outline-none placeholder:text-ink-3 focus-visible:outline-none disabled:cursor-not-allowed"
          />

          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              void onAttach(e.target.files);
              e.target.value = "";
            }}
          />

          <Toolbar
            attCount={atts.length}
            fileInputRef={fileInputRef}
            chatId={chatId}
            workerConnected={workerConnected}
            dictation={dictation}
            voice={!!voice && !!onTranscribe}
            models={models}
            model={model}
            onModel={onModel}
            thinking={thinking}
            onThinking={onThinking}
            onCollect={onCollect}
            usage={usage}
            onCompact={onCompact}
            sending={sending}
            canSend={input.canSend}
            onStop={onStop}
          />
        </form>
      </div>
    </div>
  );
}
