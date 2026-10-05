import { eq } from "./testing.ts";
import { channelLook } from "./statusLook.ts";

// A QR login waiting to be scanned is not "Connected" and not an error: it
// needs the owner, and says what for.
eq(channelLook("pending_auth").word, "Needs linking", "a pending login is named");
eq(channelLook("pending_auth").lamp, "needs_you", "a pending login lights the needs-you lamp");

eq(channelLook("connected").word, "Connected", "connected");
eq(channelLook("error").lamp, "needs_you", "error");
eq(channelLook("something_new").word, "something new", "an unknown status is shown, not hidden");
