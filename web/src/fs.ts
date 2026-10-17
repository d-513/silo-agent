import { ui } from "./api";
import { isTextKind } from "./fileKind";
import type { FileEntry } from "./gen/silo/v1/ui_pb";
import type { QuoteTarget } from "./quote/QuoteContext";

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
  // What quoting a file of this source points at; none, it cannot be quoted.
  quote?(path: string): QuoteTarget | undefined;
};

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
    quote: (path) => ({ source: "file", ref: path, label: path }),
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
    quote: (path) => ({ source: "file", ref: abs(path), label: abs(path) }),
  };
}

// artifactSource is one folder version of an artifact (a skill or an app), as
// it was pinned: its file list comes once, each file on demand.
export function artifactSource(botId: string, artifactId: string, version: number, root: string): FsSource {
  let entries: Promise<{ path: string; size: bigint }[]> | null = null;
  const all = () => (entries ??= ui.getArtifact({ botId, artifactId, version }).then((r) => r.entries.map((e) => ({ path: e.path, size: e.size }))));
  return {
    rootLabel: root.split("/").filter(Boolean).pop() || root || "artifact",
    async list(dir) {
      const prefix = dir ? `${dir}/` : "";
      const seen = new Map<string, FsEntry>();
      for (const e of await all()) {
        if (!e.path.startsWith(prefix)) continue;
        const rest = e.path.slice(prefix.length);
        const [head, ...more] = rest.split("/");
        const path = prefix + head;
        if (more.length) seen.set(path, seen.get(path) ?? { name: head, path, dir: true, size: 0n, modified: "" });
        else seen.set(path, { name: head, path, dir: false, size: e.size, modified: "" });
      }
      return [...seen.values()].sort((a, b) => (a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1));
    },
    async read(path) {
      const r = await ui.readArtifactFile({ botId, artifactId, version, path });
      const text = isTextKind(r.name || path);
      return { name: r.name, content: text ? new TextDecoder().decode(r.data) : "", data: r.data, binary: !text, truncated: false, size: r.size };
    },
    quote: (path) => ({ source: "artifact", ref: `${artifactId}@${version}/${path}`, label: `${root}/${path} · v${version}` }),
  };
}
