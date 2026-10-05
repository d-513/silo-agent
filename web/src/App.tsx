import { RouterProvider } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { onSignedOut, ui } from "./api";
import { setSession, useSession } from "./auth";
import { isSignedOut } from "./errors";
import { Unreachable } from "./Offline";
import { queryClient } from "./query";
import { router } from "./router";

export default function App() {
  const session = useSession();
  const [unreachable, setUnreachable] = useState(false);
  // Only an Unauthenticated answer means signed out. Anything else (the CP
  // restarting, a proxy 502, a store error) retries with backoff instead of
  // showing a Sign in screen for a session that may be fine.
  useEffect(() => {
    let dead = false;
    let t: ReturnType<typeof setTimeout> | undefined;
    const check = (delay: number) => {
      ui.me({})
        .then((r) => {
          if (dead) return;
          setUnreachable(false);
          setSession(r.user ? { email: r.user.email, admin: r.user.admin } : null);
        })
        .catch((e) => {
          if (dead) return;
          if (isSignedOut(e)) {
            setUnreachable(false);
            setSession(null);
            return;
          }
          setUnreachable(true);
          t = setTimeout(() => check(Math.min(delay * 2, 8000)), delay);
        });
    };
    check(500);
    return () => {
      dead = true;
      clearTimeout(t);
    };
  }, []);
  useEffect(() => onSignedOut(() => setSession(null)), []);
  // Nothing one account loaded is shown to the next.
  useEffect(() => {
    if (session === null) queryClient.clear();
  }, [session]);
  if (session === undefined) return unreachable ? <Unreachable /> : null;
  // The routes guard themselves on the session (router.tsx).
  return <RouterProvider router={router} />;
}
