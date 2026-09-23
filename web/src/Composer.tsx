import { ArrowUp, Paperclip, Square, Upload, X } from "lucide-react";
import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FormEvent, type KeyboardEvent } from "react";
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
  const [isFocused, setIsFocused] = useState(false);
  const [isDragging, setIsDragging] = useState(false);

  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    const nextHeight = Math.min(Math.max(el.scrollHeight, 42), 180);
    el.style.height = `${nextHeight}px`;
  }, [text]);

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      if ((text.trim() || atts.length > 0) && chatId) {
        onSend();
      }
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
    if (workerConnected && chatId) {
      setIsDragging(true);
    }
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
    if ((text.trim() || atts.length > 0) && chatId) {
      onSend();
    }
  };

  const canSend = (text.trim().length > 0 || atts.length > 0) && !!chatId;

  return (
    <div className="shrink-0 bg-plaster p-3 pt-2 max-wide:pb-[max(0.75rem,env(safe-area-inset-bottom))]">
      <div className="mx-auto max-w-4xl">
        <form
          onSubmit={handleSubmit}
          className={`@container group relative flex flex-col rounded-[12px] border bg-folio transition-[border-color,background-color] duration-200 ease-quiet ${
            isDragging
              ? "border-pine bg-linen/40"
              : isFocused
                ? "border-bindery"
                : "border-thread hover:border-hover"
          }`}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
        >
          {isDragging && (
            <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center gap-2 rounded-[12px] border-2 border-dashed border-pine bg-folio font-medium text-[13px] text-pine">
              <Upload size={20} />
              <span>Drop files to attach to /workspace/tmp</span>
            </div>
          )}

          {atts.length > 0 && (
            <div className="flex flex-wrap gap-1.5 border-b border-thread-2/60 px-3.5 pt-3 pb-2">
              {atts.map((a) => (
                <span
                  key={a.path}
                  title={a.path}
                  className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-thread-2 bg-cloth/80 px-2.5 py-1 text-[12px] text-iron"
                >
                  <Paperclip size={13} className="shrink-0 text-bindery" />
                  <span className="max-w-[180px] truncate font-medium">{a.name}</span>
                  <span className="shrink-0 font-mono text-[11px] text-stone">{fmtSize(a.size)}</span>
                  <button
                    type="button"
                    title="Remove attachment"
                    className="ml-0.5 rounded-[6px] p-0.5 text-stone transition-colors hover:bg-linen hover:text-carmine"
                    onClick={() => onRemoveAtt(a.path)}
                  >
                    <X size={12} />
                  </button>
                </span>
              ))}
            </div>
          )}

          {attachErr && (
            <div className="flex items-center gap-1.5 px-3.5 pt-2 text-[12px] text-carmine">
              <span className="h-1.5 w-1.5 rounded-full bg-carmine" />
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
            onFocus={() => setIsFocused(true)}
            onBlur={() => setIsFocused(false)}
            placeholder={
              !chatId
                ? "Select or create a chat to begin…"
                : botName
                ? `Ask ${botName}…`
                : "Ask this Bot…"
            }
            disabled={!chatId}
            className="w-full resize-none bg-transparent px-3.5 pt-3 pb-2 text-[14px] leading-relaxed text-iron placeholder:text-stone/60 outline-none max-h-[180px] font-sans"
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

          <div className="flex items-center justify-between px-3 pb-2.5 pt-1">
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                title={workerConnected ? "Attach files (or drag & drop here)" : "Start the Bot to attach files"}
                disabled={!workerConnected || !chatId}
                className="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-[12px] font-medium text-stone transition-colors hover:bg-cloth hover:text-iron disabled:cursor-not-allowed disabled:opacity-40"
                onClick={() => fileInputRef.current?.click()}
              >
                <Paperclip size={15} />
                <span className="hidden sm:inline">Attach</span>
                {atts.length > 0 && (
                  <span className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-bindery-pale px-1 text-[10px] font-semibold text-bindery-deep">
                    {atts.length}
                  </span>
                )}
              </button>

              {models && models.length > 0 && onModel ? (
                <Select
                  variant="ghost"
                  className="max-w-[180px]"
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
                  className="hidden items-center gap-1 rounded-[6px] bg-cloth px-2 py-1 text-[11px] text-stone @min-[540px]:inline-flex"
                  title={`Input ${usage.input} · Output ${usage.output}`}
                >
                  cached {fmtTokens(usage.cacheRead)} / new {fmtTokens(usage.input - usage.cacheRead > 0 ? usage.input - usage.cacheRead : 0)}
                </span>
              ) : null}
            </div>

            <div className="flex items-center gap-2">
              <span className="hidden select-none text-[11px] text-stone/70 @min-[660px]:inline-block">
                ↵ to send · Shift+↵ for newline
              </span>
              {sending ? (
                <button
                  type="button"
                  title="Stop this reply"
                  onClick={onStop}
                  className="inline-flex h-8 items-center gap-2 rounded-[6px] bg-carmine px-3 text-[12px] font-medium text-white shadow-xs transition-[background-color,transform] duration-200 ease-quiet hover:bg-[#b91c1c] active:scale-[0.97]"
                >
                  <span>Stop</span>
                  <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-[4px] bg-white/20">
                    <Square size={12} />
                  </span>
                </button>
              ) : (
                <button
                  type="submit"
                  title="Send message"
                  disabled={!canSend}
                  className={`inline-flex h-8 items-center justify-center gap-2 rounded-[6px] px-2.5 text-[12px] font-medium shadow-xs transition-[background-color,transform] duration-200 ease-quiet ${
                    !canSend
                      ? "cursor-not-allowed bg-cloth text-stone/50 shadow-none"
                      : "bg-bindery text-white hover:bg-bindery-deep active:scale-[0.97]"
                  }`}
                >
                  <span>Send</span>
                  <span className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-[4px] ${!canSend ? "bg-linen" : "bg-white/20"}`}>
                    <ArrowUp size={13} />
                  </span>
                </button>
              )}
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}
