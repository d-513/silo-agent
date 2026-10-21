import { closeThinking, runOk, runningCompaction } from "./find.ts";
import type { Handler } from "./text.ts";
import type { ArtifactPreview, ArtifactVersion } from "./types.ts";
import { t } from "../i18n.ts";

// Handlers for what happens to the run itself: compaction, its end, artifact
// cards, and errors.

// A /clear (the context starts over here) or a /wipe (messages were deleted
// for good; the body is how many).
export const boundary: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  out.push({ key, type: "boundary", kind: e.kind === "wiped" ? "wiped" : "cleared", count: Number(e.body) || 0, createdAt: e.createdAt });
};

export const compacting: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  out.push({ key, type: "compaction", text: "", reason: e.tool, running: true, runId: e.runId });
};

// The summary: it settles the running compaction, or stands alone.
export const compaction: Handler = (out, e, key) => {
  const c = runningCompaction(out, e.runId);
  if (c) {
    c.text = e.body;
    c.running = false;
  } else {
    out.push({ key, type: "compaction", text: e.body, reason: e.tool, runId: e.runId });
  }
};

// The run ended. Anything still running in it was cut off; a Stop leaves a
// receipt.
export const done: Handler = (out, e, key) => {
  const cut = e.body === "stopped" || e.body === "interrupted" || e.body === "error";
  for (let c = runningCompaction(out, e.runId); c; c = runningCompaction(out, e.runId)) c.running = false;
  for (const b of out) {
    if (b.type !== "tool" || !runOk(b, e.runId)) continue;
    if (b.running) {
      b.running = false;
      if (cut) b.outcome = "stopped";
    }
    b.waiting = false;
    for (const c of b.calls ?? []) {
      if (c.running) {
        c.running = false;
        if (cut) c.outcome = "stopped";
      }
      c.waiting = false;
    }
  }
  closeThinking(out, e.at);
  const last = out[out.length - 1];
  if (last?.type === "assistant" && last.streaming && e.runId) {
    last.streaming = false;
    last.bounds = undefined;
  }
  if (e.body === "stopped") out.push({ key, type: "receipt", decision: "stopped", title: t("this run"), target: "", runId: e.runId });
};

// An artifact card. Every emit of a path is a version of the same card
// (artifact_id): the card stays where it first appeared and gains the
// version, and a quiet row marks where the update happened. The same version
// again (Save skill) updates it in place.
export const artifact: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  let body: Record<string, unknown> = {};
  try {
    body = JSON.parse(e.body || "{}") as Record<string, unknown>;
  } catch {
    /* ignore */
  }
  const str = (v: unknown) => (typeof v === "string" ? v : "");
  const path = str(body.path);
  const name = str(body.name);
  if (!path && !name && !str(body.artifact_id)) return;
  const kind = body.type === "app" ? "app" : body.type === "skill" ? "skill" : "file";
  // A card from before versions: one version, keyed by its path.
  const artifactId = str(body.artifact_id) || `path:${path || name}`;
  const n = typeof body.version === "number" && body.version > 0 ? body.version : 1;
  const preview = body.preview && typeof body.preview === "object" ? (body.preview as ArtifactPreview) : undefined;
  const version: ArtifactVersion = {
    n,
    name,
    kind: str(body.kind) || (kind === "file" ? "file" : "tree"),
    size: typeof body.size === "number" ? body.size : undefined,
    pinned: body.pinned === true,
    note: str(body.note) || undefined,
    network: body.network === true,
    entry: str(body.entry) || undefined,
    files: typeof body.files === "number" ? body.files : undefined,
    preview,
    runId: e.runId,
  };
  const title = str(body.title) || name || path;
  const saved = body.status === "saved" ? n : undefined;
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type !== "artifact" || b.artifactId !== artifactId) continue;
    const at = b.versions.findIndex((v) => v.n === n);
    if (at >= 0) {
      b.versions[at] = { ...b.versions[at], ...version, preview: version.preview ?? b.versions[at].preview };
    } else {
      b.versions = [...b.versions, version].sort((x, y) => x.n - y.n);
      out.push({ key, type: "artifact_update", artifactId, title, version: n, runId: e.runId });
    }
    b.title = title;
    b.artifactType = kind;
    if (saved) b.saved = saved;
    return;
  }
  out.push({ key, type: "artifact", artifactId, artifactType: kind, title, path, versions: [version], saved, runId: e.runId });
};

export const error: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  const c = runningCompaction(out, e.runId);
  if (c) c.running = false;
  out.push({ key, type: "error", text: e.body });
};
