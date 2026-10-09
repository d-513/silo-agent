import { tunnelNext } from "./signinNext.ts";

// What the sign-in page says when an OIDC round trip comes back with
// ?error=<code>. The server only ever sends a code (internal/app/account/oidc.go),
// and anything else in the address is ignored, so a link cannot put its own
// words on this page.
const ERRORS: Record<string, string> = {
  oidc_off: "Single sign-on is not set up on this server.",
  oidc_unavailable: "The sign-in provider could not be reached. Try again in a moment.",
  oidc_expired: "That sign-in took too long or was started somewhere else. Try again.",
  oidc_denied: "The sign-in was cancelled or refused by the provider.",
  oidc_failed: "The provider's answer could not be verified. Try again, or tell an admin.",
  oidc_unverified: "The provider did not confirm your email address, so Silo cannot tell whose account this is.",
  oidc_no_account: "There is no Silo account for you yet. Ask an admin for an invite.",
  oidc_conflict: "That email belongs to an account signed in a different way. Ask an admin.",
  oidc_disabled: "This account is disabled. Ask an admin.",
};

export function signInError(code: string | undefined): string {
  return (code && Object.hasOwn(ERRORS, code) && ERRORS[code]) || "";
}

const START = "/auth/oidc/start";

// oidcStart is the address that begins signing in with the OIDC provider,
// carrying where to land afterwards: a private tunnel's handoff (`next`) wins,
// then the page the session ended on (`from`). The server checks it again.
export function oidcStart(search: string, origin: string): string {
  const rd = tunnelNext(search, origin) ?? plainPath(new URLSearchParams(search).get("from"));
  return rd ? `${START}?rd=${encodeURIComponent(rd)}` : START;
}

function plainPath(p: string | null): string | null {
  if (!p || !p.startsWith("/") || p.startsWith("//") || p.includes("\\") || p.startsWith("/signin")) return null;
  return p;
}

// passwordProblem is why a new password cannot be set yet, or "" when it can.
// The server has the last word (8 to 256 characters).
export function passwordProblem(password: string, again: string): string {
  if ([...password].length < 8) return "Use at least 8 characters.";
  if ([...password].length > 256) return "Use at most 256 characters.";
  if (password !== again) return "The two passwords are not the same.";
  return "";
}
