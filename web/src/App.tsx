import { lazy, Suspense, useEffect, useState } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { onSignedOut, ui } from "./api";
import { AccountPage } from "./AccountPage";
import { AuthCtx, useAuth } from "./auth";
import { BotPage } from "./bot/BotPage";
import { BotsPage } from "./BotsPage";
import { isSignedOut } from "./errors";
import { lazyNamed } from "./lazyNamed";
import { preloadMarkdown } from "./mdPlugins";
import { NewBotPage } from "./NewBotPage";
import { OfflineBanner, Unreachable } from "./Offline";
import { PaneFallback } from "./PaneFallback";
import { queryClient } from "./query";
import { Shell } from "./Shell";
import { SignIn } from "./SignIn";

// The admin area and the skills hub are loaded when first opened.
const AdminApp = lazy(() => import("./AdminApp"));
const SkillHub = lazyNamed(() => import("./Skills"), "SkillHub");

function Authed() {
  const { email } = useAuth();
  // Replies with code are common: fetch the highlighter once the browser is idle.
  useEffect(() => preloadMarkdown(), []);
  return (
    <Routes>
        <Route
          path="/"
          element={
            <Shell page="bots">
              <BotsPage />
            </Shell>
          }
        />
        <Route
          path="/new"
          element={
            <Shell page="bots">
              <NewBotPage />
            </Shell>
          }
        />
        <Route
          path="/skills"
          element={
            <Shell page="skills">
              <Suspense fallback={<PaneFallback />}>
                <SkillHub />
              </Suspense>
            </Shell>
          }
        />
        <Route
          path="/bots/:id/*"
          element={
            <Shell page="bots" fill>
              <BotPage />
            </Shell>
          }
        />
        <Route
          path="/admin/*"
          element={
            <Shell page="admin">
              <Suspense fallback={<PaneFallback />}>
                <AdminApp />
              </Suspense>
            </Shell>
          }
        />
        <Route
          path="/account"
          element={
            <Shell page="account">
              <AccountPage email={email} />
            </Shell>
          }
        />
        <Route path="/signin" element={<Navigate to="/" />} />
        <Route path="*" element={<Navigate to="/" />} />
    </Routes>
  );
}

function ToSignIn() {
  const loc = useLocation();
  return <Navigate to="/signin" replace state={{ from: loc.pathname + loc.search }} />;
}

export default function App() {
  const [session, setSession] = useState<{ email: string; admin: boolean } | null | undefined>(undefined);
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
  return (
    <AuthCtx.Provider value={{ email: session?.email ?? "", admin: session?.admin ?? false, setSession }}>
      {session === null ? (
        <Routes>
          <Route path="/signin" element={<SignIn />} />
          <Route path="*" element={<ToSignIn />} />
        </Routes>
      ) : (
        <>
          <Authed />
          <OfflineBanner />
        </>
      )}
    </AuthCtx.Provider>
  );
}

