import { ui } from "./api";

// Opens a connection's sign-in in a popup; the OAuth callback finishes it.
// From a Bot it names the Bot's grant; from the Workspace's hub, the
// connection itself.
// shared is the signer's say-so (Who can use this sign-in): false, only Bots
// they alone use.
export async function startConnectorAuth(target: ({ botId: string; id: string } | { connectorId: string }) & { shared?: boolean }) {
  const r = await ui.startConnectorAuth(target);
  if (r.authorizeUrl) window.open(r.authorizeUrl, "silo-oauth", "width=480,height=720");
}
