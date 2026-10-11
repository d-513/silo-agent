import { ConfigSource, type Provider, type SearchEngine } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";

export const LABELS: Record<string, string> = {
  model: msg("Default model"),
  model_title: msg("Chat title model"),
  model_approval: msg("Auto-approval model"),
  model_subagent: msg("Subagent model"),
  model_memory: msg("Memory save model"),
  model_decision: msg("Default decision model"),
  embedding_model: msg("Embedding model"),
  transcribe_model: msg("Speech-to-text model"),
  "memory.in_prompt": msg("Memories in the prompt"),
  "memory.collect": msg("Collect from idle chats"),
  "knowledge.enabled": msg("Document search"),
  "knowledge.sync_interval": msg("Re-check folders every"),
  "knowledge.ocr": msg("Read scans and images (OCR)"),
  "tunnels.enabled": msg("Tunnels"),
  "tunnels.host": msg("Domain suffix"),
  "tunnels.scheme": msg("Link scheme"),
  "mail.enabled": msg("Mail"),
  "mail.domain": msg("Mail domain"),
  "mail.addr": msg("SMTP listen address"),
  "mail.max_size_mb": msg("Largest message (MB)"),
  "mail.tls_cert": msg("TLS certificate file"),
  "mail.tls_key": msg("TLS key file"),
  "context.window": msg("Fallback context window"),
  "context.compact_at": msg("Compact at"),
  "runs.max_duration": msg("Max run duration"),
  "changes.enabled": msg("Track workspace changes"),
  "changes.max_file_mb": msg("Largest file kept (MB)"),
  "changes.keep_days": msg("Keep history for (days)"),
  debug: msg("Debug logging"),
  "search.engine": msg("Engine"),
  http_addr: msg("Listen address"),
  public_url: msg("Public URL"),
  cp_url: msg("Control plane URL"),
  data_dir: msg("Data directory"),
  database_url: msg("Database URL"),
  docker_host: msg("Docker host"),
  bot_image: msg("Bot image"),
  mcp_stdio_image: msg("STDIO MCP image"),
  "drives.image": msg("Drive sidecar image"),
  "drives.mount_root": msg("Drive mount root"),
  "drives.cache_max_size": msg("Drive cache limit"),
  "outbound.block_private": msg("Block private addresses"),
  "outbound.allow": msg("Exceptions"),
  "auth.password": msg("Password sign-in"),
  "auth.trusted_proxies": msg("Trusted proxies"),
  "auth.host_cookie": msg("Host-only session cookie"),
  "oidc.issuer": msg("Issuer"),
  "oidc.client_id": msg("Client ID"),
  "oidc.client_secret": msg("Client secret"),
  "oidc.scopes": msg("Scopes"),
  "oidc.label": msg("Button label"),
  "oidc.auto_create": msg("Create accounts on sign-in"),
  "oidc.allowed_domains": msg("Allowed email domains"),
  "oidc.groups_claim": msg("Groups claim"),
  "oidc.admin_group": msg("Admin group"),
  "bootstrap.email": msg("Email"),
  "bootstrap.password": msg("Password"),
};

export const HINTS: Record<string, string> = {
  "context.window": msg("Tokens, used when the provider does not report one (OpenRouter does). Per-model overrides go in silo.yaml under context.windows."),
  "context.compact_at": msg("Fraction of the window (0.1–0.98) at which a run summarizes its history before the next model call."),
  "changes.max_file_mb": msg("A bigger file is recorded by its size only, never its content. 1 to 100."),
  "changes.keep_days": msg("Older snapshots are forgotten. 1 to 365."),
  "runs.max_duration": msg("How long one run may go on (120m, 2h). -1 is unlimited. A lead waiting on its subagents counts its sleep toward this."),
  embedding_model: msg("For long-term memories and indexed documents. OpenAI-compatible, 1536-wide; switching models makes old memories match poorly (documents are re-embedded on the next sync)."),
  "knowledge.sync_interval": msg("A Go duration such as 15m or 1h (minimum 1m)."),
  "knowledge.ocr": msg("Reads scanned PDF pages and image files (English and Polish) with tesseract in the Bot's box. A folder of photos makes syncing slow; turn it off for those."),
  "tunnels.host": msg("A tunnel is served at <name>.<this domain>. Needs a wildcard DNS record and TLS certificate for *.<this domain> pointing at the control plane, on a domain separate from the control plane's own. Unset, local development uses localhost and the control plane's port."),
  "tunnels.scheme": msg("http or https. Leave empty to follow the Public URL; set it when TLS ends at a proxy and the Public URL is an internal address."),
  "mail.domain":
    msg("Every Bot's address is <name>@<this domain>. Point the domain's MX record at this server; until one does, nothing arrives. Changing it moves every address at once."),
  "mail.addr":
    msg("Where the control plane listens for SMTP, as host:port or :port. Other mail servers deliver to port 25, so publish or forward 25 to this port (or set :25 if the control plane may bind it). A reverse proxy for the web UI does not carry mail."),
  "mail.max_size_mb": msg("A message over this, attachments included, is refused. 1 to 50."),
  "mail.tls_cert": msg("A PEM certificate for STARTTLS, with the key below. Empty, a self-signed one is made at start, which is all that mail servers ask for."),
  "mail.tls_key": msg("The PEM private key that goes with the certificate."),
  "drives.mount_root": msg("Where drive mounts live, as a path the container engine sees."),
  "drives.cache_max_size": msg("The most one drive may cache on disk, such as 10G."),
  "outbound.block_private":
    msg("Refuses loopback, private (10.x, 172.16-31.x, 192.168.x), link-local and other internal addresses, and names that resolve to one, for everything Silo itself connects to because a user typed it: a connector's URL and sign-in, an Email or Calendar account, a skill's download address."),
  "outbound.allow":
    msg("Internal addresses that stay reachable, comma-separated: an IP or a CIDR, optionally with one port (10.0.4.20, 10.0.5.0/24, 127.0.0.1:9223). Names are not taken."),
  "auth.password": msg("Off, people sign in with OIDC only. It only takes effect while OIDC is configured, so it cannot lock everyone out."),
  "auth.trusted_proxies":
    msg("IPs or CIDRs of the reverse proxies in front of Silo, comma-separated (127.0.0.1, 10.0.0.0/8). X-Forwarded-For is believed only from them. Behind a proxy with this unset, every visitor looks like the proxy and shares one sign-in limit."),
  "auth.host_cookie":
    msg("Names the session cookie __Host-silo_session, which only this exact host can set. Turn it on when tunnels live under the same domain as Silo (silo.example.com and *.tunnels.example.com): otherwise a tunnelled app can plant a session cookie and sign a visitor into another account. It needs an https Public URL and is ignored without one. Changing it signs everyone out once."),
  "oidc.issuer": msg("The provider's issuer URL, the one that serves /.well-known/openid-configuration."),
  "oidc.scopes": msg("Space-separated. openid is always asked for; add the scope that carries groups if the admin group is used."),
  "oidc.label": msg("The text of the button on the sign-in page."),
  "oidc.auto_create": msg("Makes an account the first time someone signs in. Off, only people who already have an account with the same verified email get in."),
  "oidc.allowed_domains": msg("Comma-separated. Limits creating accounts on sign-in to these email domains. Empty allows any."),
  "oidc.groups_claim": msg("The ID token claim that lists a person's groups."),
  "oidc.admin_group": msg("Members of this group are admins, checked at each sign-in. Empty leaves roles to Admin → Users."),
  transcribe_model: msg("Composer dictation and the transcribe tool. Any OpenAI-compatible /audio/transcriptions model (local/… for LocalAI, Speaches, vLLM, whisper.cpp). off disables voice."),
};

export const PLACEHOLDERS: Record<string, string> = {
  embedding_model: "openrouter/openai/text-embedding-3-small",
  transcribe_model: "openrouter/openai/whisper-1",
  "tunnels.host": "tunnels.example.com",
  "tunnels.scheme": msg("follows Public URL"),
  "mail.domain": "bots.example.com",
  "mail.addr": ":2525",
  "mail.tls_cert": "self-signed",
  "mail.tls_key": "self-signed",
  "oidc.issuer": "https://id.example.com",
  "oidc.scopes": "openid email profile",
  "oidc.label": msg("Single sign-on"),
  "oidc.groups_claim": "groups",
  "oidc.allowed_domains": "example.com",
};

export const CONTEXT_NOTE = msg("When a conversation nears the model's context window, it is summarized into one turn. The thread keeps everything; the model sees the summary.");

export const RUNS_NOTE = msg("A run is one reply: every model call and tool call from a message to the answer.");

export const CHANGES_NOTE =
  msg("Each Bot's machine snapshots /workspace before and after every run, and the Changes pane shows the difference as diffs. The history stays on the Bot's own disk, beside the workspace. Drives, tmp/, bot/ and dependency folders are never read, and a Bot keeps at most 300 snapshots and 1 GB. Drives get a journal instead: what was written, deleted or renamed on them, without any content. Turning this off stops both; what was recorded stays.");

export const CONTAINERS_NOTE = msg("The container engine and the images a Bot's machine and its sidecars start from. A running Bot keeps its old image until its container is reset.");

export const MEMORY_NOTE =
  msg("Memories in the prompt shows the Bot every long-term memory while they are few, and a summary written in the background once they are many; nothing is searched while you wait, and the Bot looks up details with recall. Collecting reads each chat once it has been quiet for 10 minutes and saves the facts and lessons the Bot missed, one cheap call per chat. The embedding and memory save models are under Models.");

export const KNOWLEDGE_NOTE =
  msg("Folders the owner picks on a Bot's Knowledge page are indexed with the embedding model (under Models) and searched by the Bot's search_docs tool. Drive folders are re-checked four times less often. Turning this off stops syncing and hides the tool; what is already indexed stays. OCR is CPU work inside the Bot's box, one scan at a time per folder.");

export const TUNNELS_NOTE =
  msg("Tunnels give the owner addresses for services running on a Bot's machine, served by this control plane at <name>.<domain suffix>. Changing the suffix moves every existing tunnel at once; private tunnels ask their owner to sign in again.");

export const MAIL_NOTE =
  msg("Gives every Bot a receive-only address. The control plane runs its own small SMTP listener and files what arrives in the Bot's inbox, where the Bot reads it with list_mail and read_mail. Nothing is ever sent, so there is no relay or sender reputation to look after. On as soon as a domain is set; changes here apply within a few seconds, without a restart.");

export const OUTBOUND_NOTE =
  msg("Silo makes these connections itself, so without this a user can point a connector at 127.0.0.1 or at a machine on your network and read what answers. Turn it on when people you do not fully trust have accounts. A library connector you pointed at an internal address (the Lightpanda preset) keeps working for as long as a Bot's copy still points where yours does. This does not cover the models, search or sign-in providers you set up here, and it does not limit what a Bot's own machine can reach.");

export const AUTH_NOTE =
  msg("Wrong passwords, two-factor codes and invite links are limited per account and address: five tries, then a wait that doubles from 30 seconds to 15 minutes.");

export const OIDC_NOTE =
  msg("One OpenID Connect provider (Authentik, Keycloak, Google, Entra and the like). On when the issuer and client ID are set. An account is matched by the email the provider has verified.");

export const BOOTSTRAP_NOTE = msg("First admin only. Ignored after a user exists. Restart required.");

export function groupOf(key: string) {
  if (key === "model" || key === "model_title" || key === "model_approval" || key === "model_subagent" || key === "model_memory" || key === "model_decision" || key === "embedding_model" || key === "transcribe_model") return "models";
  if (key.startsWith("memory.")) return "memory";
  if (key.startsWith("knowledge.")) return "knowledge";
  if (key.startsWith("tunnels.")) return "tunnels";
  if (key.startsWith("mail.")) return "mail";
  if (key.startsWith("context.")) return "context";
  if (key.startsWith("runs.")) return "runs";
  if (key.startsWith("changes.")) return "changes";
  if (key.startsWith("drives.") || key === "docker_host" || key === "bot_image" || key === "mcp_stdio_image") return "containers";
  if (key.startsWith("providers.")) return "providers";
  if (key.startsWith("search.")) return "search";
  if (key.startsWith("bootstrap.")) return "bootstrap";
  if (key.startsWith("outbound.")) return "outbound";
  if (key.startsWith("auth.")) return "auth";
  if (key.startsWith("oidc.")) return "oidc";
  return "server";
}

export function labelOf(key: string, engines: SearchEngine[], providers: Provider[]) {
  if (LABELS[key]) return t(LABELS[key]);
  const p = /^providers\.(.+)\.(.+)$/.exec(key);
  if (p) {
    const prov = providers.find((x) => x.id === p[1]);
    const f = prov?.fields.find((x) => x.key === p[2]);
    if (f?.label) return f.label;
  }
  const m = /^search\.(.+)\.(.+)$/.exec(key);
  if (m) {
    const eng = engines.find((e) => e.id === m[1]);
    const f = eng?.fields.find((x) => x.key === m[2]);
    if (f?.label) return f.label;
  }
  return key;
}

// hintOf is the sentence under a field: ours, else the one the provider or
// search engine declares for its own setting.
export function hintOf(key: string, engines: SearchEngine[], providers: Provider[]) {
  if (HINTS[key]) return t(HINTS[key]);
  const p = /^providers\.(.+)\.(.+)$/.exec(key);
  if (p) return providers.find((x) => x.id === p[1])?.fields.find((x) => x.key === p[2])?.description || undefined;
  const m = /^search\.(.+)\.(.+)$/.exec(key);
  if (m) return engines.find((e) => e.id === m[1])?.fields.find((x) => x.key === m[2])?.description || undefined;
  return undefined;
}

/** The example a field shows while empty. */
export function placeholderOf(key: string): string | undefined {
  return PLACEHOLDERS[key] ? t(PLACEHOLDERS[key]) : undefined;
}

export function sourceWord(s: ConfigSource) {
  if (s === ConfigSource.ENV) return "env";
  if (s === ConfigSource.YAML) return "yaml";
  return "default";
}
