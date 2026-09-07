import { ui } from "./api";
import type { FileEntry } from "./gen/silo/v1/ui_pb";

export type FsEntry = {
  name: string;
  path: string;
  dir: boolean;
  size: bigint;
  modified: string;
};

export type FsFile = {
  name: string;
  content: string;
  data?: Uint8Array;
  binary: boolean;
  truncated: boolean;
  size: bigint;
};

export type FsSource = {
  rootLabel: string;
  list(path: string): Promise<FsEntry[]>;
  read(path: string): Promise<FsFile>;
};

export function fmtSize(n: bigint | number) {
  const v = typeof n === "bigint" ? Number(n) : n;
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`;
  return `${(v / (1024 * 1024)).toFixed(1)} MB`;
}

export function crumbs(path: string, rootLabel = "workspace") {
  if (!path) return [{ label: rootLabel, path: "" }];
  const parts = path.split("/").filter(Boolean);
  const out = [{ label: rootLabel, path: "" }];
  let acc = "";
  for (const p of parts) {
    acc = acc ? `${acc}/${p}` : p;
    out.push({ label: p, path: acc });
  }
  return out;
}

export function joinPath(dir: string, name: string) {
  const n = name
    .replaceAll("\\", "/")
    .split("/")
    .filter((p) => p && p !== "." && p !== "..")
    .join("/");
  if (!n) return "";
  return dir ? `${dir}/${n}` : n;
}

export function parentPath(path: string) {
  const i = path.lastIndexOf("/");
  return i <= 0 ? "" : path.slice(0, i);
}

function asEntry(e: FileEntry, path = e.path): FsEntry {
  return { name: e.name, path, dir: e.dir, size: e.size, modified: e.modified };
}

function asFile(r: { name: string; content: string; data: Uint8Array; binary: boolean; truncated: boolean; size: bigint }): FsFile {
  return { name: r.name, content: r.content, data: r.data, binary: r.binary, truncated: r.truncated, size: r.size };
}

export function botSource(botId: string): FsSource {
  return {
    rootLabel: "workspace",
    async list(path) {
      const r = await ui.listFiles({ botId, path });
      return r.entries.map((e) => asEntry(e));
    },
    async read(path) {
      return asFile(await ui.readFile({ botId, path }));
    },
  };
}

export function skillSource(scope: string, name: string): FsSource {
  return {
    rootLabel: name,
    async list(path) {
      const r = await ui.listSkillFiles({ scope, name, path });
      return r.entries.map((e) => asEntry(e));
    },
    async read(path) {
      return asFile(await ui.readSkillFile({ scope, name, path }));
    },
  };
}

export function workspaceDirSource(botId: string, prefix: string): FsSource {
  const root = prefix.replace(/^\/+|\/+$/g, "");
  const abs = (p: string) => (p ? joinPath(root, p) : root);
  const rel = (p: string) => {
    if (!root) return p;
    if (p === root) return "";
    if (p.startsWith(`${root}/`)) return p.slice(root.length + 1);
    return p;
  };
  return {
    rootLabel: root.split("/").pop() || root || "workspace",
    async list(path) {
      const r = await ui.listFiles({ botId, path: abs(path) });
      return r.entries.map((e) => asEntry(e, rel(e.path)));
    },
    async read(path) {
      return asFile(await ui.readFile({ botId, path: abs(path) }));
    },
  };
}
