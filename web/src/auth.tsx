import { useSyncExternalStore } from "react";

// The signed-in user, held outside React so the router's guards can read it
// the moment it changes. undefined = not known yet (the boot check is out).
export type Session = { email: string; admin: boolean };

let session: Session | null | undefined;
const listeners = new Set<() => void>();

export function getSession() {
  return session;
}

export function setSession(s: Session | null) {
  session = s;
  listeners.forEach((l) => l());
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
