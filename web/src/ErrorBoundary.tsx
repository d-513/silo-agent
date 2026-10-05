import { Component, type ErrorInfo, type ReactNode } from "react";
import { btnClass } from "./Btn";

// Snag is what a crashed page shows. The router renders it for a route that
// threw (router.tsx), so the rail and every other page stay usable.
export function Snag({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <div role="alert" className="flex h-full min-h-[320px] flex-col items-center justify-center bg-well px-4 text-center wide:px-8">
      <p className="mb-2 max-w-[26rem] text-title text-ink">This page hit a snag</p>
      <p className="mb-6 max-w-[32rem] font-mono text-[12.5px] break-words text-ink-3">{(error instanceof Error && error.message) || String(error)}</p>
      <div className="flex gap-2">
        <button type="button" className={btnClass("primary")} onClick={onRetry}>
          Try again
        </button>
        <button type="button" className={btnClass("secondary")} onClick={() => window.location.reload()}>
          Reload
        </button>
      </div>
    </div>
  );
}

// ErrorBoundary is the last resort around the whole app, for a crash outside
// any route.
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(error, info.componentStack);
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return <Snag error={error} onRetry={() => this.setState({ error: null })} />;
  }
}
