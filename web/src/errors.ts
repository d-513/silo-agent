import { Code, ConnectError } from "@connectrpc/connect";

// errors.ts is the one place the UI classifies a failed call. Only
// Unauthenticated ends the session; a CP restart, a dev-proxy 502, or a network
// drop is transient and must never look like a sign-out.

function code(e: unknown): Code | undefined {
  return e instanceof ConnectError ? e.code : undefined;
}

function networkFailure(e: unknown): boolean {
  if (e instanceof TypeError) return true; // fetch rejects with TypeError when the host is unreachable
  return e instanceof ConnectError && e.cause instanceof TypeError;
}

/** The server says the session is missing or expired. */
export function isSignedOut(e: unknown): boolean {
  return code(e) === Code.Unauthenticated;
}

/** The call never reached a working CP; retrying later can succeed. */
export function isTransient(e: unknown): boolean {
  if (networkFailure(e)) return true;
  const c = code(e);
  if (c === Code.Unavailable || c === Code.DeadlineExceeded) return true;
  // A proxy error page (no Connect body) surfaces as Unknown "HTTP 5xx".
  return c === Code.Unknown && e instanceof ConnectError && /^HTTP 5\d\d$/.test(e.rawMessage);
}

/** The thing asked for does not exist or is not ours; retrying will not help. */
export function isGone(e: unknown): boolean {
  const c = code(e);
  return c === Code.NotFound || c === Code.PermissionDenied;
}

/** A request aborted by the client (unmount, navigation, Stop). */
export function isCanceled(e: unknown): boolean {
  return code(e) === Code.Canceled || (e instanceof DOMException && e.name === "AbortError");
}

const fallback: Partial<Record<Code, string>> = {
  [Code.NotFound]: "Not found.",
  [Code.PermissionDenied]: "You don't have access to that.",
  [Code.Canceled]: "Canceled.",
  [Code.ResourceExhausted]: "Too many requests. Try again in a moment.",
};

/** A short sentence for the user. Never a "[code]" prefix, never empty. */
export function fail(e: unknown): string {
  if (isSignedOut(e)) return "Your session ended. Sign in again.";
  if (isTransient(e)) return "Can't reach Silo. Try again in a moment.";
  const c = code(e);
  const raw = e instanceof ConnectError ? e.rawMessage : e instanceof Error ? e.message.replace(/^\[[^\]]+\]\s*/, "") : "";
  if (raw.trim()) return raw;
  return (c !== undefined && fallback[c]) || "Something went wrong.";
}
