import { ArrowUp, FoldVertical, Mic, Paperclip, Square, UnfoldVertical, Upload, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FormEvent, type KeyboardEvent } from "react";
import { setAutoExpand, useAutoExpand } from "./autoExpand";
import { fmtClock, insertDictation, useDictation, type Dictation } from "./voice";
import { fmtBytes } from "./format";
import { Select } from "./Select";
import { Tip, TipAction, TipTitle } from "./Tip";
import { MemoryCollectButton } from "./MemoryCollect";
import { fitThinking } from "./thinking";
import { ThinkingPicker } from "./ThinkingPicker";
import type { CollectMemoriesResponse, ModelOption } from "./gen/silo/v1/ui_pb";
import { modelOptions } from "./modelOptions";

export interface Attachment {
  name: string;
  path: string;
  size: number;
}

export interface Usage {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  window: number;
}

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

function fmtTokens(n: number) {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(n);
}

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
  const textRef = useRef(text);
  textRef.current = text;
  const dictation = useDictation(
    (audio, mime) => (onTranscribe ? onTranscribe(audio, mime) : Promise.reject(new Error("Dictation is off."))),
    (spoken) => {
      // Splice at the caret (or over the selection) the textarea last had.
      const el = textareaRef.current;
      const cur = textRef.current;
      const r = insertDictation(cur, el?.selectionStart ?? cur.length, el?.selectionEnd ?? cur.length, spoken);
      setText(r.text);
      requestAnimationFrame(() => {
        el?.focus();
        el?.setSelectionRange(r.caret, r.caret);
      });
    },
  );
  const recording = dictation.state === "recording";
  const cancelTake = dictation.cancel;

  // Esc drops a take wherever focus is.
  useEffect(() => {
    if (!recording) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        cancelTake();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [recording, cancelTake]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const autoExpand = useAutoExpand();
  const currentModel = models?.find((m) => m.id === model) ?? models?.[0];
  const thinkingLevels = currentModel?.thinkingLevels ?? [];

  // field-sizing: content grows the textarea natively; older engines get a
  // scrollHeight fallback clamped to the same 26–168px.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el || nativeGrow) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(Math.max(el.scrollHeight, 26), 168)}px`;
  }, [text]);

  const canSend = (text.trim().length > 0 || atts.length > 0) && !!chatId;

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Escape" && recording) return; // the window listener cancels the take
    if (e.key === "Escape" && sending) {
      e.preventDefault();
      onStop();
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      if (canSend) onSend();
    }
  };

  const handlePaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (e.clipboardData.files && e.clipboardData.files.length > 0) {
      e.preventDefault();
      void onAttach(e.clipboardData.files);
    }
  };

  const handleDragOver = (e: DragEvent) => {
    e.preventDefault();
    if (workerConnected && chatId) setIsDragging(true);
  };

  const handleDragLeave = (e: DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  };

  const handleDrop = (e: DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0 && workerConnected && chatId) {
      void onAttach(e.dataTransfer.files);
    }
  };

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (sending && !canSend) {
      onStop();
      return;
    }
    if (canSend) onSend();
  };

  return (
    <div className="shrink-0 px-4 pt-1 pb-4 max-wide:pb-[max(1rem,env(safe-area-inset-bottom))]">
      <div className="mx-auto max-w-[720px]">
        <form
          onSubmit={handleSubmit}
          className={`@container relative flex flex-col rounded-[18px] bg-surface transition-shadow duration-[200ms] ease-quiet ${
            isDragging ? "shadow-[0_0_0_1px_var(--color-emerald)]" : "shadow-float focus-within:shadow-float-focus"
          }`}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
        >
          {isDragging && (
            <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center gap-2 rounded-[18px] bg-surface text-[13px] font-medium text-emerald">
              <Upload size={18} />
              <span>Drop files to attach to /workspace/tmp</span>
            </div>
          )}

          {atts.length > 0 && (
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
                    onClick={() => onRemoveAtt(a.path)}
                  >
                    <X size={12} />
                  </button>
                </span>
              ))}
            </div>
          )}

          {attachErr && (
            <div role="alert" className="flex items-center gap-1.5 px-4 pt-2.5 text-[12px] text-vermilion">
              <span className="h-1.5 w-1.5 rounded-full bg-vermilion" />
              <span>{attachErr}</span>
            </div>
          )}

          {dictation.error && dictation.state === "idle" && (
            <div role="alert" className="flex items-center gap-1.5 px-4 pt-2.5 text-[12px] text-vermilion">
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
              <span className="min-w-0 truncate">{dictation.error}</span>
              <button
                type="button"
                title="Dismiss"
                aria-label="Dismiss"
                className="ml-auto flex h-5 w-5 shrink-0 items-center justify-center rounded-xs text-ink-3 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
                onClick={dictation.clearError}
              >
                <X size={12} />
              </button>
            </div>
          )}

          {dictation.state !== "idle" && <DictationStrip dictation={dictation} />}

          <textarea
            ref={textareaRef}
            rows={1}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            placeholder={!chatId ? "Select or create a chat to begin…" : botName ? `Ask ${botName}…` : "Ask this Bot…"}
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
                {atts.length > 0 && (
                  <span className="pop absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-ink px-1 text-[10px] font-semibold text-white">
                    {atts.length}
                  </span>
                )}
              </button>

              {voice && onTranscribe ? <MicButton dictation={dictation} disabled={!chatId} /> : null}

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
                <ThinkingPicker
                  value={fitThinking(thinking ?? "", thinkingLevels)}
                  levels={thinkingLevels}
                  onChange={onThinking}
                  disabled={!chatId}
                />
              ) : null}

              <Tip
                closeOnClick={false}
                content={
                  <>
                    <TipTitle aside={autoExpand ? "On" : "Off"}>Follow live steps</TipTitle>
                    <p className="mt-1">
                      {autoExpand
                        ? "Thinking and tool rows open while they run and fold shut when they finish."
                        : "Thinking and tool rows stay folded. Click a row to open it."}
                    </p>
                    <TipAction>Click to turn {autoExpand ? "off" : "on"}</TipAction>
                  </>
                }
              >
                <button
                  type="button"
                  aria-label="Open thinking and tool rows while they run"
                  aria-pressed={autoExpand}
                  className={`flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-control transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well active:scale-[.94] active:duration-[70ms] ${
                    autoExpand ? "text-ink" : "text-ink-3 hover:text-ink"
                  }`}
                  onClick={() => setAutoExpand(!autoExpand)}
                >
                  {autoExpand ? <UnfoldVertical size={16} /> : <FoldVertical size={16} />}
                </button>
              </Tip>

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
        </form>
      </div>
    </div>
  );
}

// ContextMeter is how full the model's context window was on the last turn;
// clicking it compacts the conversation into a summary.
function ContextMeter({ usage, onCompact, disabled }: { usage: Usage; onCompact?: () => void; disabled: boolean }) {
  const used = usage.input + usage.output;
  const frac = Math.min(1, used / usage.window);
  const pct = Math.round(frac * 100);
  const r = 6;
  const circ = 2 * Math.PI * r;
  const tone = frac >= 0.9 ? "text-vermilion" : frac >= 0.7 ? "text-ink" : "text-ink-3";
  const barTone = frac >= 0.9 ? "bg-vermilion" : frac >= 0.7 ? "bg-ink" : "bg-ink-3";
  const can = !disabled && !!onCompact;
  return (
    <Tip
      content={
        <div className="w-[220px]">
          <TipTitle aside={`${pct}%`}>Context window</TipTitle>
          <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-well">
            <div className={`h-full rounded-full ${barTone}`} style={{ width: `${Math.max(2, pct)}%` }} />
          </div>
          <dl className="mt-2 grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
            <dt>Used</dt>
            <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(used)}</dd>
            <dt>Model limit</dt>
            <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(usage.window)}</dd>
            {usage.cacheRead > 0 ? (
              <>
                <dt>From cache</dt>
                <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(usage.cacheRead)}</dd>
              </>
            ) : null}
          </dl>
          <p className="mt-2">Near the limit the conversation is summarized automatically; the thread keeps everything.</p>
          {onCompact ? <TipAction>{can ? "Click to compact now" : "You can compact once the reply finishes"}</TipAction> : null}
        </div>
      }
    >
      <button
        type="button"
        aria-label={`Context ${pct}% full. Compact conversation`}
        aria-disabled={!can}
        onClick={can ? onCompact : undefined}
        className={`flex h-[34px] shrink-0 items-center gap-1.5 rounded-control px-2 font-mono text-[11px] transition-[background-color,color,transform] duration-[160ms] ease-quiet ${
          can ? "hover:bg-well hover:text-ink active:scale-[.96] active:duration-[70ms]" : "cursor-default"
        } ${tone}`}
      >
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden className="shrink-0 -rotate-90">
          <circle cx="8" cy="8" r={r} fill="none" stroke="currentColor" strokeOpacity="0.18" strokeWidth="2" />
          <circle
            cx="8"
            cy="8"
            r={r}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeDasharray={circ}
            strokeDashoffset={circ * (1 - frac)}
            className="transition-[stroke-dashoffset] duration-[400ms] ease-quiet"
          />
        </svg>
        <span className="hidden @min-[400px]:inline">{pct}%</span>
      </button>
    </Tip>
  );
}

// The mic toggles a take: Mic to start, a vermilion square to stop and
// transcribe. It is disabled (with the reason as its title) where the browser
// cannot record.
function MicButton({ dictation, disabled }: { dictation: Dictation; disabled: boolean }) {
  const { state, supported } = dictation;
  const recording = state === "recording";
  const busy = state === "transcribing";
  const title = !supported
    ? "Dictation needs https or localhost and a browser that can record"
    : recording
      ? "Stop and transcribe"
      : busy
        ? "Transcribing…"
        : "Dictate";
  return (
    <button
      type="button"
      title={title}
      aria-label={recording ? "Stop dictation" : "Dictate"}
      aria-pressed={recording}
      disabled={disabled || !supported || busy}
      className={`flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-control transition-[background-color,color,transform] duration-[160ms] ease-quiet active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent ${
        recording ? "bg-vermilion-pale text-vermilion" : "text-ink-2 hover:bg-well hover:text-ink disabled:text-ink-3"
      }`}
      onClick={() => (recording ? dictation.stop() : void dictation.start())}
    >
      {recording ? <Square size={13} fill="currentColor" strokeWidth={0} /> : <Mic size={17} />}
    </button>
  );
}

// DictationStrip sits above the toolbar while a take is live: a breathing dot,
// the clock, a five-bar level meter, and Cancel. While the CP transcribes it
// becomes a shimmering "Transcribing…".
function DictationStrip({ dictation }: { dictation: Dictation }) {
  if (dictation.state === "transcribing") {
    return (
      <div className="flex h-8 items-center px-4 pt-2 text-[12px]" aria-live="polite">
        <span className="shimmer-text font-medium">Transcribing…</span>
      </div>
    );
  }
  const bars = [0.35, 0.7, 1, 0.7, 0.35];
  return (
    <div className="rise flex h-8 items-center gap-2 px-4 pt-2 text-[12px] text-ink-2" aria-live="polite">
      <span className="breathe h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
      <span className="font-medium text-ink">Listening</span>
      <span className="font-mono text-ink-3 tabular-nums">{fmtClock(dictation.elapsed)}</span>
      <span className="flex h-3.5 items-center gap-[3px]" aria-hidden>
        {bars.map((w, i) => (
          <span
            key={i}
            className="w-[3px] rounded-full bg-ink-3 transition-[height] duration-[80ms]"
            style={{ height: `${Math.max(3, Math.round(14 * Math.min(1, dictation.level * w * 1.6)))}px` }}
          />
        ))}
      </span>
      <span className="flex-1" />
      <span className="hidden text-ink-3 @min-[460px]:inline">Esc cancels</span>
      <button
        type="button"
        title="Cancel dictation"
        aria-label="Cancel dictation"
        className="flex h-6 w-6 items-center justify-center rounded-xs text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
        onClick={dictation.cancel}
      >
        <X size={13} />
      </button>
    </div>
  );
}

const nativeGrow = typeof CSS !== "undefined" && CSS.supports?.("field-sizing", "content");

// Send and Stop are one 34px button. The arrow lifts out while the square
// rotates in (320ms settle); the fill crossfades over 240ms.
function SendStop({ sending, canSend, onStop }: { sending: boolean; canSend: boolean; onStop: () => void }) {
  const fill = sending ? "bg-vermilion hover:bg-vermilion-deep" : canSend ? "bg-ink hover:bg-black" : "bg-pressed";
  const glyph = "col-start-1 row-start-1 transition-[transform,opacity] duration-[320ms] ease-settle motion-reduce:transition-opacity";
  return (
    <button
      type={sending ? "button" : "submit"}
      onClick={sending ? onStop : undefined}
      disabled={!sending && !canSend}
      title={sending ? "Stop this reply" : "Send message"}
      aria-label={sending ? "Stop this reply" : "Send message"}
      className={`grid h-[34px] w-[34px] shrink-0 place-items-center rounded-[11px] transition-[background-color,transform] duration-[240ms] ease-quiet active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:active:scale-100 ${fill}`}
    >
      <ArrowUp
        size={17}
        strokeWidth={2.25}
        className={`${glyph} ${canSend && !sending ? "text-white" : "text-ink-3"} ${
          sending ? "-translate-y-2.5 scale-[.6] opacity-0" : "opacity-100"
        }`}
      />
      <span
        className={`${glyph} h-2.5 w-2.5 rounded-[3px] bg-white ${sending ? "opacity-100" : "-rotate-90 scale-[.6] opacity-0"}`}
      />
    </button>
  );
}
