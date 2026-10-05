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
  "context.window": "Fallback context window",
  "context.compact_at": "Compact at",
  "runs.max_duration": "Max run duration",
  debug: "Debug logging",
  "search.engine": "Engine",
  http_addr: "Listen address",
  public_url: "Public URL",
  cp_url: "Control plane URL",
  data_dir: "Data directory",
  docker_host: "Docker host",
  bot_image: "Bot image",
  mcp_stdio_image: "STDIO MCP image",
  "bootstrap.email": "Email",
  "bootstrap.password": "Password",
};

export const HINTS: Record<string, string> = {
  "context.window": "Tokens, used when the provider does not report one (OpenRouter does). Per-model overrides go in silo.yaml under context.windows.",
  "context.compact_at": "Fraction of the window (0.1–0.98) at which a run summarizes its history before the next model call.",
  "runs.max_duration": "How long one run may go on (120m, 2h). -1 is unlimited. A lead waiting on its subagents counts its sleep toward this.",
  embedding_model: "For long-term memories and indexed documents. OpenAI-compatible, 1536-wide; switching models makes old memories match poorly (documents are re-embedded on the next sync).",
  "knowledge.sync_interval": "A Go duration such as 15m or 1h (minimum 1m).",
  "knowledge.ocr": "Reads scanned PDF pages and image files (English and Polish) with tesseract in the Bot's box. A folder of photos makes syncing slow; turn it off for those.",
  "tunnels.host": "A tunnel is served at <name>.<this domain>. Needs a wildcard DNS record and TLS certificate for *.<this domain> pointing at the control plane, on a domain separate from the control plane's own. Unset, local development uses localhost and the control plane's port.",
  "tunnels.scheme": "http or https. Leave empty to follow the Public URL; set it when TLS ends at a proxy and the Public URL is an internal address.",
  transcribe_model: "Composer dictation and the transcribe tool. Any OpenAI-compatible /audio/transcriptions model (local/… for LocalAI, Speaches, vLLM, whisper.cpp). off disables voice.",
};

export const PLACEHOLDERS: Record<string, string> = {
  embedding_model: "openrouter/openai/text-embedding-3-small",
  transcribe_model: "openrouter/openai/whisper-1",
  "tunnels.host": "tunnels.example.com",
  "tunnels.scheme": "follows Public URL",
};

export const CONTEXT_NOTE = "When a conversation nears the model's context window, it is summarized into one turn. The thread keeps everything; the model sees the summary.";

export const MEMORY_NOTE =
  "Auto-recall puts up to 3 long-term memories close to the opening message into each run. Collecting reads each chat once it has been quiet for 10 minutes and saves the facts and lessons the Bot missed, one cheap call per chat. The embedding and memory save models are under Models.";

export const KNOWLEDGE_NOTE =
  "Folders the owner picks on a Bot's Knowledge page are indexed with the embedding model (under Models) and searched by the Bot's search_docs tool. Drive folders are re-checked four times less often. Turning this off stops syncing and hides the tool; what is already indexed stays. OCR is CPU work inside the Bot's box, one scan at a time per folder.";

export const TUNNELS_NOTE =
  "Tunnels give the owner addresses for services running on a Bot's machine, served by this control plane at <name>.<domain suffix>. Changing the suffix moves every existing tunnel at once; private tunnels ask their owner to sign in again.";

export const BOOTSTRAP_NOTE = "First admin only. Ignored after a user exists. Restart required.";

export function groupOf(key: string) {
  if (key === "model" || key === "model_title" || key === "model_approval" || key === "model_subagent" || key === "model_memory" || key === "embedding_model" || key === "transcribe_model") return "models";
  if (key.startsWith("memory.")) return "memory";
  if (key.startsWith("knowledge.")) return "knowledge";
  if (key.startsWith("tunnels.")) return "tunnels";
  if (key.startsWith("context.") || key.startsWith("runs.")) return "context";
  if (key.startsWith("providers.")) return "providers";
  if (key.startsWith("search.")) return "search";
  if (key.startsWith("bootstrap.")) return "bootstrap";
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

export function sourceWord(s: ConfigSource) {
  if (s === ConfigSource.ENV) return "env";
  if (s === ConfigSource.YAML) return "yaml";
  return "default";
}
