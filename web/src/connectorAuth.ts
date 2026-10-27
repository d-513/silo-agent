import { ui } from "./api";

// Opens a connection's sign-in in a popup; the OAuth callback finishes it.
// From a Bot it names the Bot's grant; from the Workspace's hub, the
// connection itself.
export async function startConnectorAuth(target: { botId: string; id: string } | { connectorId: string }) {
  const r = await ui.startConnectorAuth(target);
  if (r.authorizeUrl) window.open(r.authorizeUrl, "silo-oauth", "width=480,height=720");
}
