import { ui } from "./api";

// Opens the connector's sign-in in a popup; the OAuth callback finishes it.
export async function startConnectorAuth(botId: string, id: string) {
  const r = await ui.startConnectorAuth({ botId, id });
  if (r.authorizeUrl) window.open(r.authorizeUrl, "silo-oauth", "width=480,height=720");
}
