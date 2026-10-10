import { ConfigSource, type Provider, type SearchEngine } from "../gen/silo/v1/ui_pb";

export const LABELS: Record<string, string> = {
  model: "Default model",
  model_title: "Chat title model",
  model_approval: "Auto-approval model",
  model_subagent: "Subagent model",
  model_memory: "Memory save model",
  embedding_model: "Embedding model",
  transcribe_model: "Speech-to-text model",
  "memory.auto_recall": "Auto-recall",
  "memory.collect": "Collect from idle chats",
  "knowledge.enabled": "Document search",
  "knowledge.sync_interval": "Re-check folders every",
  "knowledge.ocr": "Read scans and images (OCR)",
  "tunnels.enabled": "Tunnels",
  "tunnels.host": "Domain suffix",
  "tunnels.scheme": "Link scheme",
  "mail.enabled": "Mail",
  "mail.domain": "Mail domain",
  "mail.addr": "SMTP listen address",
  "mail.max_size_mb": "Largest message (MB)",
  "mail.tls_cert": "TLS certificate file",
  "mail.tls_key": "TLS key file",
  "context.window": "Fallback context window",
  "context.compact_at": "Compact at",
  "runs.max_duration": "Max run duration",
  "changes.enabled": "Track workspace changes",
  "changes.max_file_mb": "Largest file kept (MB)",
  "changes.keep_days": "Keep history for (days)",
  debug: "Debug logging",
  "search.engine": "Engine",
  http_addr: "Listen address",
  public_url: "Public URL",
  cp_url: "Control plane URL",
  data_dir: "Data directory",
  database_url: "Database URL",
  docker_host: "Docker host",
  bot_image: "Bot image",
  mcp_stdio_image: "STDIO MCP image",
  "drives.image": "Drive sidecar image",
  "drives.mount_root": "Drive mount root",
  "drives.cache_max_size": "Drive cache limit",
  "auth.password": "Password sign-in",
  "auth.trusted_proxies": "Trusted proxies",
  "oidc.issuer": "Issuer",
  "oidc.client_id": "Client ID",
  "oidc.client_secret": "Client secret",
  "oidc.scopes": "Scopes",
  "oidc.label": "Button label",
  "oidc.auto_create": "Create accounts on sign-in",
  "oidc.allowed_domains": "Allowed email domains",
  "oidc.groups_claim": "Groups claim",
  "oidc.admin_group": "Admin group",
  "bootstrap.email": "Email",
  "bootstrap.password": "Password",
};

export const HINTS: Record<string, string> = {
  "context.window": "Tokens, used when the provider does not report one (OpenRouter does). Per-model overrides go in silo.yaml under context.windows.",
  "context.compact_at": "Fraction of the window (0.1–0.98) at which a run summarizes its history before the next model call.",
  "changes.max_file_mb": "A bigger file is recorded by its size only, never its content. 1 to 100.",
  "changes.keep_days": "Older snapshots are forgotten. 1 to 365.",
  "runs.max_duration": "How long one run may go on (120m, 2h). -1 is unlimited. A lead waiting on its subagents counts its sleep toward this.",
  embedding_model: "For long-term memories and indexed documents. OpenAI-compatible, 1536-wide; switching models makes old memories match poorly (documents are re-embedded on the next sync).",
  "knowledge.sync_interval": "A Go duration such as 15m or 1h (minimum 1m).",
  "knowledge.ocr": "Reads scanned PDF pages and image files (English and Polish) with tesseract in the Bot's box. A folder of photos makes syncing slow; turn it off for those.",
  "tunnels.host": "A tunnel is served at <name>.<this domain>. Needs a wildcard DNS record and TLS certificate for *.<this domain> pointing at the control plane, on a domain separate from the control plane's own. Unset, local development uses localhost and the control plane's port.",
  "tunnels.scheme": "http or https. Leave empty to follow the Public URL; set it when TLS ends at a proxy and the Public URL is an internal address.",
  "mail.domain":
    "Every Bot's address is <name>@<this domain>. Point the domain's MX record at this server; until one does, nothing arrives. Changing it moves every address at once.",
  "mail.addr":
    "Where the control plane listens for SMTP, as host:port or :port. Other mail servers deliver to port 25, so publish or forward 25 to this port (or set :25 if the control plane may bind it). A reverse proxy for the web UI does not carry mail.",
  "mail.max_size_mb": "A message over this, attachments included, is refused. 1 to 50.",
  "mail.tls_cert": "A PEM certificate for STARTTLS, with the key below. Empty, a self-signed one is made at start, which is all that mail servers ask for.",
  "mail.tls_key": "The PEM private key that goes with the certificate.",
  "drives.mount_root": "Where drive mounts live, as a path the container engine sees.",
  "drives.cache_max_size": "The most one drive may cache on disk, such as 10G.",
  "auth.password": "Off, people sign in with OIDC only. It only takes effect while OIDC is configured, so it cannot lock everyone out.",
  "auth.trusted_proxies":
    "IPs or CIDRs of the reverse proxies in front of Silo, comma-separated (127.0.0.1, 10.0.0.0/8). X-Forwarded-For is believed only from them. Behind a proxy with this unset, every visitor looks like the proxy and shares one sign-in limit.",
  "oidc.issuer": "The provider's issuer URL, the one that serves /.well-known/openid-configuration.",
  "oidc.scopes": "Space-separated. openid is always asked for; add the scope that carries groups if the admin group is used.",
  "oidc.label": "The text of the button on the sign-in page.",
  "oidc.auto_create": "Makes an account the first time someone signs in. Off, only people who already have an account with the same verified email get in.",
  "oidc.allowed_domains": "Comma-separated. Limits creating accounts on sign-in to these email domains. Empty allows any.",
  "oidc.groups_claim": "The ID token claim that lists a person's groups.",
  "oidc.admin_group": "Members of this group are admins, checked at each sign-in. Empty leaves roles to Admin → Users.",
  transcribe_model: "Composer dictation and the transcribe tool. Any OpenAI-compatible /audio/transcriptions model (local/… for LocalAI, Speaches, vLLM, whisper.cpp). off disables voice.",
};

export const PLACEHOLDERS: Record<string, string> = {
  embedding_model: "openrouter/openai/text-embedding-3-small",
  transcribe_model: "openrouter/openai/whisper-1",
  "tunnels.host": "tunnels.example.com",
  "tunnels.scheme": "follows Public URL",
  "mail.domain": "bots.example.com",
  "mail.addr": ":2525",
  "mail.tls_cert": "self-signed",
  "mail.tls_key": "self-signed",
  "oidc.issuer": "https://id.example.com",
  "oidc.scopes": "openid email profile",
  "oidc.label": "Single sign-on",
  "oidc.groups_claim": "groups",
  "oidc.allowed_domains": "example.com",
};

export const CONTEXT_NOTE = "When a conversation nears the model's context window, it is summarized into one turn. The thread keeps everything; the model sees the summary.";

export const RUNS_NOTE = "A run is one reply: every model call and tool call from a message to the answer.";

export const CHANGES_NOTE =
  "Each Bot's machine snapshots /workspace before and after every run, and the Changes pane shows the difference as diffs. The history stays on the Bot's own disk, beside the workspace. Drives, tmp/, bot/ and dependency folders are never read, and a Bot keeps at most 300 snapshots and 1 GB. Drives get a journal instead: what was written, deleted or renamed on them, without any content. Turning this off stops both; what was recorded stays.";

export const CONTAINERS_NOTE = "The container engine and the images a Bot's machine and its sidecars start from. A running Bot keeps its old image until its container is reset.";

export const MEMORY_NOTE =
  "Auto-recall puts up to 3 long-term memories close to the opening message into each run. Collecting reads each chat once it has been quiet for 10 minutes and saves the facts and lessons the Bot missed, one cheap call per chat. The embedding and memory save models are under Models.";

export const KNOWLEDGE_NOTE =
  "Folders the owner picks on a Bot's Knowledge page are indexed with the embedding model (under Models) and searched by the Bot's search_docs tool. Drive folders are re-checked four times less often. Turning this off stops syncing and hides the tool; what is already indexed stays. OCR is CPU work inside the Bot's box, one scan at a time per folder.";

export const TUNNELS_NOTE =
  "Tunnels give the owner addresses for services running on a Bot's machine, served by this control plane at <name>.<domain suffix>. Changing the suffix moves every existing tunnel at once; private tunnels ask their owner to sign in again.";

export const MAIL_NOTE =
  "Gives every Bot a receive-only address. The control plane runs its own small SMTP listener and files what arrives in the Bot's inbox, where the Bot reads it with list_mail and read_mail. Nothing is ever sent, so there is no relay or sender reputation to look after. On as soon as a domain is set; changes here apply within a few seconds, without a restart.";

export const AUTH_NOTE =
  "Wrong passwords, two-factor codes and invite links are limited per account and address: five tries, then a wait that doubles from 30 seconds to 15 minutes.";

export const OIDC_NOTE =
  "One OpenID Connect provider (Authentik, Keycloak, Google, Entra and the like). On when the issuer and client ID are set. An account is matched by the email the provider has verified.";

export const BOOTSTRAP_NOTE = "First admin only. Ignored after a user exists. Restart required.";

export function groupOf(key: string) {
  if (key === "model" || key === "model_title" || key === "model_approval" || key === "model_subagent" || key === "model_memory" || key === "embedding_model" || key === "transcribe_model") return "models";
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
  if (key.startsWith("auth.")) return "auth";
  if (key.startsWith("oidc.")) return "oidc";
  return "server";
}

export function labelOf(key: string, engines: SearchEngine[], providers: Provider[]) {
  if (LABELS[key]) return LABELS[key];
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
  if (HINTS[key]) return HINTS[key];
  const p = /^providers\.(.+)\.(.+)$/.exec(key);
  if (p) return providers.find((x) => x.id === p[1])?.fields.find((x) => x.key === p[2])?.description || undefined;
  const m = /^search\.(.+)\.(.+)$/.exec(key);
  if (m) return engines.find((e) => e.id === m[1])?.fields.find((x) => x.key === m[2])?.description || undefined;
  return undefined;
}

export function sourceWord(s: ConfigSource) {
  if (s === ConfigSource.ENV) return "env";
  if (s === ConfigSource.YAML) return "yaml";
  return "default";
}
