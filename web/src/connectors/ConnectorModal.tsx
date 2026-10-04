import { X } from "lucide-react";
import type { ReactNode } from "react";

// The overlay the connector dialogs share: a card over a dimmed page with a
// close button. `wide` is the error viewer's width; `rise` animates it in;
// `dismissOnBackdrop` lets a press outside the card close it.
export function ConnectorModal({
  onClose,
  wide,
  rise,
  dismissOnBackdrop,
  children,
}: {
  onClose: () => void;
  wide?: boolean;
  rise?: boolean;
  dismissOnBackdrop?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className="fixed inset-0 z-30 flex items-start justify-center overflow-y-auto bg-ink/30 backdrop-blur-xs py-[6vh] px-4"
      onClick={dismissOnBackdrop ? onClose : undefined}
    >
      <div
        className={`w-full ${wide ? "max-w-[720px]" : "max-w-[560px]"} rounded-card shadow-card bg-surface p-6 shadow-slip relative${rise ? " rise" : ""}`}
        onClick={dismissOnBackdrop ? (e) => e.stopPropagation() : undefined}
      >
        <button type="button" className="absolute top-4 right-4 text-ink-2 hover:text-ink p-1 rounded-md transition-colors" onClick={onClose} title="Close">
          <X size={18} />
        </button>
        {children}
      </div>
    </div>
  );
}
