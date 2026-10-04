export type Attachment = { name: string; path: string; size: number };

// `at` is the client arrival time (ms); replayed history arrives in a burst.
// `createdAt` is the server's record time (RFC 3339) when it sent one.
export type Ev = { id?: string; kind: string; body: string; tool: string; runId?: string; attachments?: Attachment[]; at?: number; createdAt?: string };

// Why a tool stopped without a clean result: the human denied it, or the run
// was stopped/interrupted around it.
export type Outcome = "denied" | "stopped";

export type NestedCall = { key: string; title: string; name: string; result?: string; running?: boolean; waiting?: boolean; outcome?: Outcome };

export type ToolBlock = {
  key: string;
  type: "tool";
  name: string;
  args: string;
  result?: string;
  running?: boolean;
  runId?: string;
  calls?: NestedCall[];
  // Paused on an approval slip.
  waiting?: boolean;
  approvalId?: string;
  outcome?: Outcome;
};

export type CompactionBlock = { key: string; type: "compaction"; text: string; reason: string; running?: boolean; runId?: string };

export type ReportBlock = { key: string; type: "report"; text: string; agents: { name: string; status: string }[]; runId?: string; createdAt?: string };

// reportAgents parses a subagent_report's "name:status,…" label.
export function reportAgents(label: string): { name: string; status: string }[] {
  return label
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean)
    .map((x) => {
      const i = x.lastIndexOf(":");
      return i < 0 ? { name: x, status: "" } : { name: x.slice(0, i), status: x.slice(i + 1) };
    });
}

export type Decision = "allow_once" | "always" | "deny" | "stopped";

export type ReceiptBlock = { key: string; type: "receipt"; decision: Decision; title: string; target: string; runId?: string };

export type Block =
  // `from` names a non-human sender ("lead" for a subagent's brief and the
  // lead's messages to it).
  | { key: string; type: "user"; id?: string; runId?: string; text: string; attachments?: Attachment[]; createdAt?: string; from?: string }
  // `bounds` are source offsets where each streamed delta began.
  | { key: string; type: "assistant"; text: string; streaming?: boolean; bounds?: number[] }
  | { key: string; type: "thinking"; text: string; streaming?: boolean; startAt?: number; ms?: number }
  | ToolBlock
  | ReceiptBlock
  | { key: string; type: "error"; text: string }
  // Subagents finished and woke the lead: `agents` are their names and
  // statuses, `text` the report the lead was given.
  | ReportBlock
  // A Feed post quoted into a new chat: `source` is where it was posted from.
  | { key: string; type: "quote"; text: string; source: string; createdAt?: string }
  // The history before this point was summarized to fit the context window.
  // `running` while the summary is being written; empty text once settled
  // means it failed or was stopped.
  | CompactionBlock
  | {
      key: string;
      type: "artifact";
      artifactType: string;
      name: string;
      title: string;
      path: string;
      scope: string;
      approvalId: string;
      status: string;
      size?: number;
      runId?: string;
    };
