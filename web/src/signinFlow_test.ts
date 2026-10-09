import { eq } from "./testing.ts";
import { oidcStart, passwordProblem, signInError } from "./signinFlow.ts";

const origin = "http://localhost:5173";
const enc = encodeURIComponent;

// Only codes the server sends are worded; a link cannot put text on the page.
eq(signInError("oidc_no_account"), "There is no Silo account for you yet. Ask an admin for an invite.", "a known code");
eq(signInError("Call 555-0100 to unlock your account"), "", "free text is ignored");
eq(signInError("toString"), "", "an object key is not a code");
eq(signInError(undefined), "", "no error");
eq(signInError(""), "", "empty error");

// Where the OIDC round trip lands afterwards.
eq(oidcStart("", origin), "/auth/oidc/start", "nowhere in particular");
eq(oidcStart(`?from=${enc("/bots/abc/run?x=1")}`, origin), `/auth/oidc/start?rd=${enc("/bots/abc/run?x=1")}`, "back to the page the session ended on");
const handoff = "/tunnels/auth?name=quiet-amber-heron&rd=%2Fpage";
eq(oidcStart(`?next=${enc(handoff)}`, origin), `/auth/oidc/start?rd=${enc(handoff)}`, "a tunnel handoff");
eq(oidcStart(`?from=${enc("/admin")}&next=${enc(handoff)}`, origin), `/auth/oidc/start?rd=${enc(handoff)}`, "the handoff wins over from");
eq(oidcStart(`?from=${enc("https://evil.example/")}`, origin), "/auth/oidc/start", "another site");
eq(oidcStart(`?from=${enc("//evil.example/")}`, origin), "/auth/oidc/start", "protocol-relative");
eq(oidcStart(`?from=${enc("/\\evil.example")}`, origin), "/auth/oidc/start", "backslash trick");
eq(oidcStart(`?from=${enc("/signin?from=/x")}`, origin), "/auth/oidc/start", "not back to sign-in");
eq(oidcStart(`?next=${enc("/admin")}`, origin), "/auth/oidc/start", "next is only for the handoff");

eq(passwordProblem("short", "short"), "Use at least 8 characters.", "too short");
eq(passwordProblem("ąęółżźćń", "ąęółżźćń"), "", "eight characters, not eight bytes");
eq(passwordProblem("long-enough", "long-enouhg"), "The two passwords are not the same.", "a typo in the second");
eq(passwordProblem("long-enough", "long-enough"), "", "fine");
eq(passwordProblem("x".repeat(257), "x".repeat(257)), "Use at most 256 characters.", "too long");
