import { lazy, Suspense } from "react";

// The TanStack Query panel: what is cached, what is polling, what is stale. It
// is a development tool only (never in a build), and off until asked for so it
// does not sit on the page: in the browser console run
//   localStorage.setItem("silo.devtools", "1")
// and reload; removeItem turns it off again.
const Panel = import.meta.env.DEV ? lazy(() => import("@tanstack/react-query-devtools").then((m) => ({ default: m.ReactQueryDevtools }))) : null;

function wanted() {
  try {
    return localStorage.getItem("silo.devtools") === "1";
  } catch {
    return false;
  }
}

export function Devtools() {
  if (!Panel || !wanted()) return null;
  return (
    <Suspense fallback={null}>
      <Panel initialIsOpen={false} buttonPosition="bottom-right" />
    </Suspense>
  );
}
