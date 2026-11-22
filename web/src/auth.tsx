import { useSyncExternalStore } from "react";

// The signed-in user, held outside React so the router's guards can read it
// the moment it changes. undefined = not known yet (the boot check is out).
export type Session = { email: string; admin: boolean };

let session: Session | null | undefined;
const listeners = new Set<() => void>();

export function getSession() {
  return session;
}

// landing is where the next sign-in goes instead of back to where the session
// ended: Admin, for the first admin's first sign-in (SignInResponse.welcome).
let landing: string | undefined;

export function setSession(s: Session | null, land?: string) {
  session = s;
  landing = land;
  listeners.forEach((l) => l());
}

/** The landing a sign-in asked for, once. */
export function takeLanding() {
  const l = landing;
  landing = undefined;
  return l;
}

/** Called on every sign-in and sign-out. */
export function onSession(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useSession() {
  return useSyncExternalStore(onSession, getSession);
}

export function useAuth() {
  const s = useSession();
  return { email: s?.email ?? "", admin: s?.admin ?? false, setSession };
}
