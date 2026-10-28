import { Upload } from "lucide-react";
import { useRef, useState, type KeyboardEvent } from "react";
import type { CollectMemoriesResponse, ModelOption } from "../gen/silo/v1/ui_pb";
import type { Usage } from "../useRunStream";
import { AttachmentChips, type Attachment } from "./AttachmentChips";
import { QuoteChips } from "../quote/QuoteChips";
import type { DraftQuote } from "../quote/model";
import { DictationStrip } from "./DictationStrip";
import { placeholderFor } from "./model";
import { CommandNotice, Notice, type Said } from "./Notice";
import { completion, matchCommands, needsArgs, slashQuery, type SlashCommand } from "./slash.ts";
import { SlashMenu } from "./SlashMenu";
import { Toolbar } from "./Toolbar";
import { useComposerInput } from "./useComposerInput";
import { useDictationInput } from "./useDictationInput";
import { t } from "../i18n.ts";

export type { Attachment } from "./AttachmentChips";
export type { Said } from "./Notice";

export interface ComposerProps {
  text: string;
  setText: (val: string | ((prev: string) => string)) => void;
  atts: Attachment[];
  onRemoveAtt: (path: string) => void;
  quotes?: DraftQuote[];
  onRemoveQuote?: (id: string) => void;
  attachErr?: string;
  onAttach: (files: FileList | null) => Promise<void> | void;
  onSend: () => void;
  onStop: () => void;
  sending: boolean;
  chatId?: string;
  // The empty chat a first message makes: it takes input without an id.
  fresh?: boolean;
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
  // talk offers a call with the Bot in this chat.
  talk?: boolean;
  onTalk?: () => void;
  onTranscribe?: (audio: Uint8Array, mime: string) => Promise<string>;
  // onCollect runs the memory collector over this chat now.
  onCollect?: () => Promise<CollectMemoriesResponse>;
  // commands are the slash commands the menu offers; onCommand runs one
  // picked from it.
  commands?: SlashCommand[];
  onCommand?: (text: string) => void;
  // said is what the last command said back, or the question it asks.
  said?: Said | null;
  onDismissSaid?: () => void;
}

// The message box: attachments, the textarea and its toolbar, with drag-and-drop
// and dictation around it.
export function Composer({
  text,
  setText,
  atts,
  onRemoveAtt,
  quotes = [],
  onRemoveQuote,
  attachErr,
  onAttach,
  onSend,
  onStop,
  sending,
  chatId,
  fresh,
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
  talk,
  onTalk,
  onTranscribe,
  onCollect,
  commands = [],
  onCommand,
  said,
  onDismissSaid,
}: ComposerProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const dictation = useDictationInput(textareaRef, text, setText, onTranscribe);
  const ready = !!chatId || !!fresh;
  const input = useComposerInput({
    textareaRef,
    text,
    attCount: atts.length + quotes.length,
    ready,
    workerConnected,
    sending,
    recording: dictation.state === "recording",
    onSend,
    onStop,
    onAttach,
  });

  // The slash menu: open while a bare "/name" is typed, until Esc.
  const query = slashQuery(text);
  const [closedFor, setClosedFor] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const items = query === null || query === closedFor ? [] : matchCommands(commands, query);
  const pickCommand = (c: SlashCommand, run: boolean) => {
    if (run && !needsArgs(c) && onCommand) {
      setText("");
      onCommand(`/${c.name}`);
      return;
    }
    setText(completion(c));
    textareaRef.current?.focus();
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (items.length > 0) {
      const at = Math.min(active, items.length - 1);
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setActive((at + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length);
        return;
      }
      if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing)) {
        e.preventDefault();
        pickCommand(items[at], e.key === "Enter");
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setClosedFor(query);
        return;
      }
    }
    input.onKeyDown(e);
  };

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
              <span>{t("Drop files to attach to /workspace/tmp")}</span>
            </div>
          )}

          {items.length > 0 && <SlashMenu items={items} active={Math.min(active, items.length - 1)} onHover={setActive} onPick={(c) => pickCommand(c, true)} />}

          {said && <CommandNotice said={said} onDismiss={() => onDismissSaid?.()} />}

          {quotes.length > 0 && <QuoteChips quotes={quotes} onRemove={(id) => onRemoveQuote?.(id)} />}

          {atts.length > 0 && <AttachmentChips atts={atts} onRemove={onRemoveAtt} />}

          {attachErr && <Notice text={attachErr} />}

          {dictation.error && dictation.state === "idle" && <Notice text={dictation.error} onDismiss={dictation.clearError} />}

          {dictation.state !== "idle" && <DictationStrip dictation={dictation} />}

          <textarea
            ref={textareaRef}
            rows={1}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setActive(0);
            }}
            onKeyDown={onKeyDown}
            onPaste={input.onPaste}
            placeholder={placeholderFor(ready, botName)}
            disabled={!ready}
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
            ready={ready}
            workerConnected={workerConnected}
            dictation={dictation}
            voice={!!voice && !!onTranscribe}
            talk={talk && onTalk ? onTalk : undefined}
            botName={botName}
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
