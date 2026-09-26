import { ArrowUp, Paperclip, Upload, X } from "lucide-react";
import { useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FormEvent, type KeyboardEvent } from "react";
import { fmtSize } from "./fs";
import { Select } from "./Select";
import type { ModelOption } from "./gen/silo/v1/ui_pb";

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
  usage?: Usage | null;
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
  usage,
}: ComposerProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isDragging, setIsDragging] = useState(false);

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
                  <span className="shrink-0 font-mono text-[11px] text-ink-3">{fmtSize(a.size)}</span>
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

              {models && models.length > 0 && onModel ? (
                <Select
                  variant="ghost"
                  className="min-w-0 max-w-[220px]"
                  title="Model for this conversation"
                  ariaLabel="Model for this conversation"
                  value={model && models.some((m) => m.id === model) ? model : models[0].id}
                  onChange={onModel}
                  disabled={!chatId}
                  options={models.map((m) => ({ value: m.id, label: m.label || m.id, hint: m.label && m.label !== m.id ? m.id : undefined }))}
                />
              ) : null}

              {usage && (usage.cacheRead > 0 || usage.cacheWrite > 0) ? (
                <span
                  className="hidden items-center rounded-sm bg-well px-2 py-1 font-mono text-[11px] text-ink-3 @min-[540px]:inline-flex"
                  title={`Input ${usage.input} · Output ${usage.output}`}
                >
                  cached {fmtTokens(usage.cacheRead)} / new {fmtTokens(usage.input - usage.cacheRead > 0 ? usage.input - usage.cacheRead : 0)}
                </span>
              ) : null}
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
