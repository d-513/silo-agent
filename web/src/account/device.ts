// deviceName turns a User-Agent into the few words a person recognises their
// session by: "Firefox on macOS". It is a label, not detection: anything it
// does not know is named plainly.
export function deviceName(ua: string): string {
  if (!ua.trim()) return "Unknown device";
  if (/SiloClient|CFNetwork|Darwin\//i.test(ua) && !/Mozilla/i.test(ua)) return "Silo for iOS";
  const browser = browserOf(ua);
  const os = osOf(ua);
  if (browser && os) return `${browser} on ${os}`;
  return browser || os || "Another app";
}

function browserOf(ua: string): string {
  // Order matters: Edge and Opera also say Chrome, and Chrome also says Safari.
  if (/Edg(e|A|iOS)?\//.test(ua)) return "Edge";
  if (/OPR\/|Opera/.test(ua)) return "Opera";
  if (/Firefox\/|FxiOS\//.test(ua)) return "Firefox";
  if (/Chrome\/|CriOS\/|Chromium\//.test(ua)) return "Chrome";
  if (/Safari\//.test(ua)) return "Safari";
  return "";
}

function osOf(ua: string): string {
  if (/iPhone|iPad|iPod/.test(ua)) return "iOS";
  if (/Android/.test(ua)) return "Android";
  if (/Windows/.test(ua)) return "Windows";
  if (/Mac OS X|Macintosh/.test(ua)) return "macOS";
  if (/CrOS/.test(ua)) return "ChromeOS";
  if (/Linux/.test(ua)) return "Linux";
  return "";
}

const METHODS: Record<string, string> = { password: "Password", oidc: "Single sign-on", invite: "Invite link" };

/** How a session was started, in words. */
export function methodName(method: string): string {
  return METHODS[method] ?? "Signed in";
}
