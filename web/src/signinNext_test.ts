import { eq } from "./testing.ts";
import { tunnelNext } from "./signinNext.ts";

const origin = "http://localhost:5173";
const enc = encodeURIComponent;

// The one place sign-in may send the browser by full navigation: the CP's
// private-tunnel handoff, with its query intact.
const auth = "/tunnels/auth?name=quiet-amber-heron&rd=%2Fpage%3Fa%3D1";
eq(tunnelNext(`?next=${enc(auth)}`, origin), auth, "the handoff path and query survive");
eq(tunnelNext(`?x=1&next=${enc(auth)}`, origin), auth, "next among other params");

// Anything else is ignored (sign-in falls back to its normal return path).
eq(tunnelNext("", origin), null, "no next");
eq(tunnelNext("?next=", origin), null, "empty next");
eq(tunnelNext(`?next=${enc("/")}`, origin), null, "an SPA path is not a handoff");
eq(tunnelNext(`?next=${enc("/tunnels/other")}`, origin), null, "another tunnels path");
eq(tunnelNext(`?next=${enc("/tunnels/authx?name=a")}`, origin), null, "prefix lookalike");
eq(tunnelNext(`?next=${enc("//evil.example/tunnels/auth?name=a")}`, origin), null, "protocol-relative");
eq(tunnelNext(`?next=${enc("https://evil.example/tunnels/auth?name=a")}`, origin), null, "another origin");
eq(tunnelNext(`?next=${enc("http://localhost:5173/tunnels/auth?name=a")}`, origin), "/tunnels/auth?name=a", "same origin absolute reduces to a path");
eq(tunnelNext(`?next=${enc("/\\evil.example")}`, origin), null, "backslash trick");
eq(tunnelNext(`?next=${enc("javascript:alert(1)")}`, origin), null, "javascript url");
