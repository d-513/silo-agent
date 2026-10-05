// Sign-in can be sent somewhere that is not an SPA route: a private tunnel's
// control-plane handoff (/tunnels/auth) needs the browser to leave the SPA and
// hit the server. `next` is therefore honored only for that one path, on this
// origin, so it cannot be turned into an open redirect.
const HANDOFF = "/tunnels/auth";

// tunnelNext returns the path+query to navigate to after sign-in, or null when
// the page has no (acceptable) `next`.
export function tunnelNext(search: string, origin: string): string | null {
  const raw = new URLSearchParams(search).get("next");
  if (!raw) return null;
  let u: URL;
  try {
    u = new URL(raw, origin);
  } catch {
    return null;
  }
  if (u.origin !== origin || u.pathname !== HANDOFF) return null;
  // A backslash or protocol-relative start is read as another host by some
  // browsers; the parsed origin check above already refused those, but a
  // relative path must also be a plain absolute path.
  if (!/^https?:/i.test(raw) && (!raw.startsWith("/") || raw.startsWith("//") || raw.includes("\\"))) return null;
  return u.pathname + u.search;
}
