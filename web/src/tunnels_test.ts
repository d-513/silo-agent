import { eq } from "./testing.ts";
import { parsePort, tunnelNotice } from "./tunnels.ts";

eq(parsePort("8000"), { port: 8000 }, "plain port");
eq(parsePort("  3000 "), { port: 3000 }, "surrounding space");
eq(parsePort("08000"), { port: 8000 }, "leading zero");
eq(parsePort("1"), { port: 1 }, "lowest");
eq(parsePort("65535"), { port: 65535 }, "highest");

const bad = (s: string, want: string) => {
  const r = parsePort(s);
  if (!("error" in r) || !r.error.includes(want)) throw new Error(`parsePort(${JSON.stringify(s)}) = ${JSON.stringify(r)}, want an error mentioning ${want}`);
};
bad("", "port");
bad("   ", "port");
bad("abc", "number");
bad("80.5", "number");
bad("-1", "number");
bad("1e3", "number");
bad("0", "1");
bad("65536", "65535");
bad("99999999999999999999", "65535");
bad("5900", "desktop");
bad("9222", "Chromium");

// What the page says when tunnels cannot be used: names the key to set, and
// only sends admins to the settings page.
eq(tunnelNotice("ok", false), null, "ok has no notice");
eq(tunnelNotice("off", false)?.admin, false, "off for a member");
eq(tunnelNotice("no_host", false)?.text.includes("tunnels.host"), true, "no_host names the key");
eq(tunnelNotice("no_host", true)?.admin, true, "no_host links admins to settings");
eq(tunnelNotice("off", true)?.text.includes("tunnels.enabled"), true, "off names the switch");
eq(tunnelNotice("", true), null, "unknown state is not a notice");
