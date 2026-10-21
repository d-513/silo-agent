import { X } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "./i18n.ts";

const widths = {
  md: "max-w-[560px] p-6",
  wide: "max-w-[720px] p-6",
  // A page-sized form (a channel, a drive) brings its own padding.
  page: "max-w-[1040px] p-2",
};

// Modal is a card over a dimmed page with a close button: a dialog to
// assistive tech, closed by Escape (unless something inside spent it, like an
// open menu) and, with `dismissOnBackdrop`, by a press outside the card. It is
// portaled to the body, so it never sits inside whatever opened it. `rise`
// animates it in.
export function Modal({
  onClose,
  size = "md",
  rise,
  dismissOnBackdrop,
  label,
  children,
}: {
  onClose: () => void;
  size?: keyof typeof widths;
  rise?: boolean;
  dismissOnBackdrop?: boolean;
  label?: string;
  children: ReactNode;
}) {
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) close.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return createPortal(
    <div
      className={`fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-ink/30 px-4 backdrop-blur-xs ${size === "page" ? "py-[4vh]" : "py-[6vh]"}`}
      onClick={dismissOnBackdrop ? onClose : undefined}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={label}
        className={`relative w-full rounded-card bg-surface shadow-slip ${widths[size]} ${rise ? "rise" : ""}`}
        onClick={dismissOnBackdrop ? (e) => e.stopPropagation() : undefined}
      >
        <button
          type="button"
          className="absolute top-4 right-4 z-10 rounded-md p-1 text-ink-2 transition-colors hover:text-ink"
          onClick={onClose}
          aria-label={t("Close")}
          title={t("Close")}
        >
          <X size={18} />
        </button>
        {children}
      </div>
    </div>,
    document.body,
  );
}
