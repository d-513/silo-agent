import { useReachable } from "./api";
import { Spinner } from "./Feedback";

// OfflineBanner is the one "Silo is unreachable" signal while signed in. Pages
// keep their last data and keep polling; the next successful call hides it.
export function OfflineBanner() {
  const reachable = useReachable();
  if (reachable) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      className="pointer-events-none fixed inset-x-0 top-[calc(env(safe-area-inset-top)+0.75rem)] z-50 flex justify-center px-4"
    >
      <div className="flex items-center gap-2 rounded-control bg-ink px-3 py-2 text-[13px] font-medium text-on-ink shadow-card">
        <Spinner size={13} tone="on-ink" />
        Can't reach Silo. Reconnecting…
      </div>
    </div>
  );
}

// Unreachable is the boot screen when the first session check cannot reach the
// CP: the session is unknown, so neither the app nor Sign in is honest yet.
export function Unreachable() {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-canvas px-4 text-center" aria-busy="true">
      <Spinner size={18} />
      <p className="text-[15px] font-medium text-ink">Can't reach Silo</p>
      <p className="max-w-[24rem] text-[13px] text-ink-3">Retrying. This clears on its own once the control plane is back.</p>
    </div>
  );
}
