// What a tool call's arguments say, read without a schema: streaming args are
// partial JSON, so fields are pulled out by hand until the object closes.

function unescapeJsonTail(s: string): string {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '"') break;
    if (c === "\\" && i + 1 < s.length) {
      const n = s[++i];
      if (n === "n") out += "\n";
      else if (n === "t") out += "\t";
      else if (n === "r") out += "\r";
      else if (n === '"' || n === "\\" || n === "/") out += n;
      else if (n === "u" && i + 4 < s.length) {
        out += String.fromCharCode(parseInt(s.slice(i + 1, i + 5), 16));
        i += 4;
      } else out += n;
      continue;
    }
    out += c;
  }
  return out;
}

function extractStringField(raw: string, key: string): string | undefined {
  for (const needle of [`"${key}": "`, `"${key}":"`]) {
    const i = raw.indexOf(needle);
    if (i >= 0) return unescapeJsonTail(raw.slice(i + needle.length));
  }
}

export function parseToolArgs(raw: string): Record<string, unknown> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw);
    if (v && typeof v === "object" && !Array.isArray(v)) return v as Record<string, unknown>;
  } catch {
    /* stream */
  }
  const out: Record<string, unknown> = {};
  for (const key of ["code", "command", "content", "path", "pattern", "old_text", "new_text", "include", "append", "text", "query", "channel", "to", "chat", "name"]) {
    const v = extractStringField(raw, key);
    if (v !== undefined) out[key] = v;
  }
  for (const key of ["offset", "limit", "max_hits"]) {
    const m = raw.match(new RegExp(`"${key}":\\s*(-?\\d+)`));
    if (m) out[key] = Number(m[1]);
  }
  return out;
}

export function asStr(v: unknown): string {
  return typeof v === "string" ? v : v == null ? "" : String(v);
}

export function prettyJson(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

export function toolMeta(name: string): { app: string } {
  switch (name) {
    case "exec_python":
      return { app: "Python" };
    case "terminal":
      return { app: "Terminal" };
    case "read":
    case "write":
    case "patch":
    case "grep":
    case "delete":
    case "present":
      return { app: "Files" };
    case "look":
    case "click":
    case "type":
    case "key":
    case "scroll":
      return { app: "Desktop" };
    case "soul":
      return { app: "SOUL" };
    case "memory":
    case "core_memory":
      return { app: "CORE MEMORY" };
    case "skill":
      return { app: "Skills" };
    case "artifact":
      return { app: "Artifact" };
    case "web_search":
      return { app: "Web search" };
    case "channel":
      return { app: "Channels" };
    case "chats":
      return { app: "Chats" };
    case "feed":
      return { app: "Feed" };
    case "list_models":
    case "switch_model":
      return { app: "Model" };
    case "spawn_agent":
    case "agent_status":
    case "message_agent":
    case "stop_agent":
      return { app: "Subagents" };
    case "sleep":
      return { app: "Sleep" };
    case "task_add":
    case "task_list":
    case "task_done":
    case "task_reset":
      return { app: "Taskboard" };
    default:
      return { app: name };
  }
}

function firstLine(s: string) {
  return s.split("\n").find((l) => l.trim())?.trim() ?? "";
}

export function toolAction(name: string, raw: string): string {
  const a = parseToolArgs(raw);
  const path = asStr(a.path);
  switch (name) {
    case "exec_python":
      return firstLine(asStr(a.code));
    case "terminal": {
      const c = firstLine(asStr(a.command));
      return c ? `$ ${c}` : "";
    }
    case "read":
    case "write":
    case "patch":
    case "delete":
      return `${name} ${path}`.trim();
    case "grep":
      return `grep ${asStr(a.pattern)}`.trim();
    case "present":
      return path;
    case "click":
      return a.x != null ? `click ${asStr(a.x)},${asStr(a.y)}` : "click";
    case "scroll":
      return a.dy != null ? `scroll ${asStr(a.dy)}` : "scroll";
    case "type":
      return "type";
    case "key":
      return `key ${asStr(a.name)}`.trim();
    case "web_search":
      return asStr(a.query);
    case "skill":
      return asStr(a.name) || path;
    case "channel":
      return asStr(a.channel) || asStr(a.to);
    case "list_models":
      return "list";
    case "switch_model":
      return asStr(a.model);
    case "spawn_agent":
      return `start ${asStr(a.name)}`.trim();
    case "agent_status":
      return asStr(a.name) ? `check ${asStr(a.name)}` : "check all";
    case "message_agent":
      return `message ${asStr(a.name)}`.trim();
    case "stop_agent":
      return `stop ${asStr(a.name)}`.trim();
    case "sleep":
      return a.seconds != null ? `${asStr(a.seconds)}s` : "";
    case "task_add":
      return Array.isArray(a.tasks) ? `add ${a.tasks.length}` : "add";
    case "task_done":
      return Array.isArray(a.ids) ? `done ${a.ids.map((n) => `#${asStr(n)}`).join(" ")}` : "done";
    case "task_list":
      return "read";
    case "task_reset":
      return "reset";
    default:
      return "";
  }
}
