import { ui } from "./api";
import { t } from "./i18n.ts";

// Opens a connection's sign-in in a popup; the OAuth callback finishes it.
// From a Bot it names the Bot's grant; from the Workspace's hub, the
// connection itself.
// shared is the signer's say-so (Who can use this sign-in): false, only Bots
// they alone use.
//
// Call it straight from the press: the window opens before anything is
// awaited (a popup blocker, Safari's above all, allows only that) and is
// pointed at the provider once the server answers. A blocked window or a
// failed start is an error, so the caller does not carry on as if signed in.
export async function startConnectorAuth(target: ({ botId: string; id: string } | { connectorId: string }) & { shared?: boolean }) {
  const w = window.open("about:blank", "silo-oauth", "width=480,height=720");
  if (!w) throw new Error(t("Your browser blocked the sign-in window. Allow pop-ups for Silo and try again."));
  try {
    const r = await ui.startConnectorAuth(target);
    if (!r.authorizeUrl) {
      w.close();
      return;
    }
    w.location.href = r.authorizeUrl;
  } catch (e) {
    w.close();
    throw e;
  }
}
