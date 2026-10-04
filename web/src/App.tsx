import { useEffect, useState } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { onSignedOut, ui } from "./api";
import { AdminConnectors } from "./AdminConnectors";
import { AdminDrives } from "./AdminDrives";
import { AccountPage, AdminDebug, AdminLayout, AdminSearchExtract, AdminSettings } from "./Admin";
import { AuthCtx, useAuth } from "./auth";
import { BotPage } from "./bot/BotPage";
import { BotsPage } from "./BotsPage";
import { BotsProvider } from "./bots";
import { isSignedOut } from "./errors";
import { NewBotPage } from "./NewBotPage";
import { OfflineBanner, Unreachable } from "./Offline";
import { Shell } from "./Shell";
import { SignIn } from "./SignIn";
import { AdminSkills, SkillHub } from "./Skills";

function AdminGate() {
  const { admin } = useAuth();
  if (!admin) return <Navigate to="/" replace />;
  return <AdminLayout />;
}

function Authed() {
  const { email } = useAuth();
  return (
    <BotsProvider>
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
              <SkillHub />
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
          path="/admin"
          element={
            <Shell page="admin">
              <AdminGate />
            </Shell>
          }
        >
          <Route index element={<Navigate to="settings" replace />} />
          <Route path="settings" element={<AdminSettings />} />
          <Route path="connectors/*" element={<AdminConnectors />} />
          <Route path="skills" element={<AdminSkills />} />
          <Route path="search-extract" element={<AdminSearchExtract />} />
          <Route path="drives" element={<AdminDrives />} />
          <Route path="debug" element={<AdminDebug />} />
        </Route>
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
    </BotsProvider>
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

