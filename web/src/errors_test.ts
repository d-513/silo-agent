import { Code, ConnectError } from "@connectrpc/connect";
import { fail, isGone, isSignedOut, isTransient } from "./errors.ts";

function eq<T>(got: T, want: T, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

// Messages: the "[code]" prefix never reaches the user.
eq(fail(new ConnectError("bot name taken", Code.AlreadyExists)), "bot name taken", "raw server message");
eq(fail(new ConnectError("", Code.NotFound)), "Not found.", "empty message falls back per code");
eq(fail(new Error("[internal] boom")), "boom", "plain Error with a code prefix");
eq(fail("nope"), "Something went wrong.", "non-error value");
eq(fail(new ConnectError("session store unavailable", Code.Unavailable)), "Can't reach Silo. Try again in a moment.", "unavailable");
eq(fail(new ConnectError("", Code.Unauthenticated)), "Your session ended. Sign in again.", "signed out");

// Only Unauthenticated is a sign-out. Everything else keeps the session.
eq(isSignedOut(new ConnectError("", Code.Unauthenticated)), true, "unauthenticated");
eq(isSignedOut(new ConnectError("", Code.Unavailable)), false, "unavailable is not a sign-out");
eq(isSignedOut(new ConnectError("admin only", Code.PermissionDenied)), false, "permission denied is not a sign-out");
eq(isSignedOut(new TypeError("Failed to fetch")), false, "network error is not a sign-out");

// Transient: the CP is restarting, the dev proxy has no upstream, or the network dropped.
eq(isTransient(new ConnectError("HTTP 502", Code.Unavailable)), true, "bad gateway");
eq(isTransient(new ConnectError("", Code.DeadlineExceeded)), true, "deadline");
eq(isTransient(ConnectError.from(new TypeError("Failed to fetch"))), true, "fetch failure");
eq(isTransient(new ConnectError("HTTP 500", Code.Unknown)), true, "proxy 500 without a Connect body");
eq(isTransient(new TypeError("NetworkError when attempting to fetch resource.")), true, "raw fetch TypeError");
eq(isTransient(new ConnectError("disk full", Code.Unknown)), false, "a real server error is not transient");
eq(isTransient(new ConnectError("", Code.Canceled)), false, "canceled is not a connection problem");
eq(isTransient(new ConnectError("", Code.Unauthenticated)), false, "unauthenticated");

eq(isGone(new ConnectError("", Code.NotFound)), true, "not found");
eq(isGone(new ConnectError("", Code.PermissionDenied)), true, "permission denied");
eq(isGone(new ConnectError("", Code.Unavailable)), false, "unavailable is not gone");

console.log("errors ok");
