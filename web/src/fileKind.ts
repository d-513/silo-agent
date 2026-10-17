import { t } from "./i18n.ts";

export type Kind =
  | "dir"
  | "image"
  | "pdf"
  | "video"
  | "audio"
  | "markdown"
  | "csv"
  | "json"
  | "code"
  | "docx"
  | "text"
  | "sheet"
  | "slides"
  | "chart"
  | "mermaid"
  | "notebook"
  | "geojson"
  | "model"
  | "html"
  | "other";

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

// isArchive reports whether a file extension (from extOf) is a compressed bundle.
export function isArchive(ext: string) {
  return ext === "zip" || ext === "gz" || ext === "tgz" || ext === "tar" || ext === "7z";
}

// A Vega or Vega-Lite spec is JSON with a second extension: chart.vl.json.
const chartSuffix = /\.(vl|vg|vega|vegalite|vega-lite)\.json$/i;

export function kindOf(name: string, dir?: boolean): Kind {
  if (dir) return "dir";
  const e = extOf(name);
  if (chartSuffix.test(name)) return "chart";
  if (e === "xlsx" || e === "xlsm" || e === "xls" || e === "ods") return "sheet";
  if (e === "pptx") return "slides";
  if (e === "mmd" || e === "mermaid") return "mermaid";
  if (e === "ipynb") return "notebook";
  if (e === "geojson") return "geojson";
  if (e === "glb") return "model";
  if (e === "html" || e === "htm") return "html";
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
    xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    glb: "model/gltf-binary",
    geojson: "application/geo+json",
    ipynb: "application/x-ipynb+json",
    html: "text/html",
    htm: "text/html",
    json: "application/json",
    csv: "text/csv",
    md: "text/markdown",
    txt: "text/plain",
  };
  return map[e] || (kindOf(name) === "text" || kindOf(name) === "code" ? "text/plain" : "application/octet-stream");
}

// Names of what no code extension covers but a highlighter reads.
const extraLangs: Record<string, string> = { svg: "xml", md: "markdown", markdown: "markdown", diff: "diff", patch: "diff" };

// codeLang is the highlight.js language for a file name, if it has one.
export function codeLang(name: string) {
  const e = extOf(name);
  return codes[e] ?? extraLangs[e];
}

export function kindLabel(name: string): string {
  switch (kindOf(name)) {
    case "image":
      return t("Image");
    case "pdf":
      return "PDF";
    case "video":
      return t("Video");
    case "audio":
      return t("Audio");
    case "markdown":
      return t("Markdown");
    case "csv":
      return t("Spreadsheet");
    case "json":
      return "JSON";
    case "docx":
      return t("Document");
    case "code":
      return extOf(name).toUpperCase() || t("Code");
    case "text":
      return t("Text");
    case "sheet":
      return t("Workbook");
    case "slides":
      return t("Slides");
    case "chart":
      return t("Chart");
    case "mermaid":
      return t("Diagram");
    case "notebook":
      return t("Notebook");
    case "geojson":
      return t("Map");
    case "model":
      return t("3D model");
    case "html":
      return "HTML";
    default:
      break;
  }
  const e = extOf(name);
  if (isArchive(e)) return t("Archive");
  return e ? e.toUpperCase() : t("File");
}

export function isTextKind(name: string): boolean {
  const k = kindOf(name);
  return (
    k === "markdown" || k === "csv" || k === "json" || k === "code" || k === "text" || k === "chart" ||
    k === "mermaid" || k === "notebook" || k === "geojson" || k === "html"
  );
}
