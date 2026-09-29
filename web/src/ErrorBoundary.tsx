import { Component, type ErrorInfo, type ReactNode } from "react";
import { btnClass } from "./Btn";

// ErrorBoundary keeps a render crash inside the page that threw: the rail and
// every other page stay usable. It resets when `resetKey` changes (the route),
// so navigating away is always a way out.
export class ErrorBoundary extends Component<{ resetKey?: string; children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(error, info.componentStack);
  }

  componentDidUpdate(prev: { resetKey?: string }) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: null });
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div role="alert" className="flex h-full min-h-[320px] flex-col items-center justify-center bg-well px-4 text-center wide:px-8">
        <p className="mb-2 max-w-[26rem] text-[22px] leading-7 font-medium tracking-[-0.015em] text-ink">This page hit a snag</p>
        <p className="mb-6 max-w-[32rem] font-mono text-[12.5px] break-words text-ink-3">{error.message || String(error)}</p>
        <div className="flex gap-2">
          <button type="button" className={btnClass("primary")} onClick={() => this.setState({ error: null })}>
            Try again
          </button>
          <button type="button" className={btnClass("secondary")} onClick={() => window.location.reload()}>
            Reload
          </button>
        </div>
      </div>
    );
  }
}
