export type Kind = "dir" | "image" | "pdf" | "video" | "audio" | "markdown" | "csv" | "json" | "code" | "docx" | "text" | "other";

const images = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "ico", "avif"]);
const videos = new Set(["mp4", "webm", "ogv", "mov"]);
const audios = new Set(["mp3", "wav", "ogg", "m4a", "flac", "aac"]);
const codes: Record<string, string> = {
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  py: "python",
  go: "go",
  rs: "rust",
  rb: "ruby",
  java: "java",
  kt: "kotlin",
  c: "c",
  h: "c",
  cpp: "cpp",
  cc: "cpp",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  css: "css",
  html: "xml",
  htm: "xml",
  xml: "xml",
  yml: "yaml",
  yaml: "yaml",
  toml: "toml",
  sql: "sql",
  json: "json",
};
const texts = new Set(["txt", "log", "env", "cfg", "ini", "conf", "gitignore", "dockerfile"]);

export function extOf(name: string) {
  const i = name.lastIndexOf(".");
  if (i <= 0 || i === name.length - 1) return name.toLowerCase() === "dockerfile" ? "dockerfile" : "";
  return name.slice(i + 1).toLowerCase();
}

export function kindOf(name: string, dir?: boolean): Kind {
  if (dir) return "dir";
  const e = extOf(name);
  if (images.has(e)) return "image";
  if (e === "pdf") return "pdf";
  if (videos.has(e)) return "video";
  if (audios.has(e)) return "audio";
  if (e === "md" || e === "markdown") return "markdown";
  if (e === "csv" || e === "tsv") return "csv";
  if (e === "json") return "json";
  if (e === "docx") return "docx";
  if (e in codes) return "code";
  if (texts.has(e) || e === "") return "text";
  return "other";
}

export function mimeOf(name: string): string {
  const e = extOf(name);
  const map: Record<string, string> = {
    png: "image/png",
    jpg: "image/jpeg",
    jpeg: "image/jpeg",
    gif: "image/gif",
    webp: "image/webp",
    bmp: "image/bmp",
    svg: "image/svg+xml",
    ico: "image/x-icon",
    avif: "image/avif",
    pdf: "application/pdf",
    mp4: "video/mp4",
    webm: "video/webm",
    mov: "video/quicktime",
    ogv: "video/ogg",
    mp3: "audio/mpeg",
    wav: "audio/wav",
    ogg: "audio/ogg",
    m4a: "audio/mp4",
    flac: "audio/flac",
    aac: "audio/aac",
    docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    html: "text/html",
    htm: "text/html",
    json: "application/json",
    csv: "text/csv",
    md: "text/markdown",
    txt: "text/plain",
  };
  return map[e] || (kindOf(name) === "text" || kindOf(name) === "code" ? "text/plain" : "application/octet-stream");
}

export function codeLang(name: string) {
  return codes[extOf(name)];
}
