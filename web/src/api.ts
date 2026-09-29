import { createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { useSyncExternalStore } from "react";
import { isCanceled, isSignedOut, isTransient } from "./errors";
import { UI } from "./gen/silo/v1/ui_pb";

// Every RPC reports here, so no page decides on its own that the user is
// signed out or that Silo is down.

type Listener = () => void;

let reachable = true;
const netListeners = new Set<Listener>();
function setReachable(v: boolean) {
  if (reachable === v) return;
  reachable = v;
  netListeners.forEach((l) => l());
}

/** false while calls fail with a transient error; the next success clears it. */
export function useReachable(): boolean {
  return useSyncExternalStore(
    (l) => {
      netListeners.add(l);
      return () => netListeners.delete(l);
    },
    () => reachable,
  );
}

const signedOut = new Set<Listener>();
/** Called when any RPC (other than SignIn) says the session is gone. */
export function onSignedOut(l: Listener): () => void {
  signedOut.add(l);
  return () => signedOut.delete(l);
}

const watch: Interceptor = (next) => async (req) => {
  try {
    const res = await next(req);
    setReachable(true);
    return res;
  } catch (e) {
    if (isSignedOut(e)) {
      setReachable(true);
      // A wrong password is Unauthenticated too; that is not a session ending.
      if (req.method.name !== "SignIn") signedOut.forEach((l) => l());
    } else if (isTransient(e)) {
      setReachable(false);
    } else if (!isCanceled(e)) {
      setReachable(true);
    }
    throw e;
  }
};

const transport = createConnectTransport({
  baseUrl: "/",
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
  interceptors: [watch],
});

export const ui = createClient(UI, transport);
