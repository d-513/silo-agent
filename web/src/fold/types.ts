import type { DashboardCard, DashboardWidget, SetupOffer } from "../gen/silo/v1/ui_pb";

export type Attachment = { name: string; path: string; size: number };

// What a chat opened from (a feed_quote event): a Feed post, a Dashboard
// widget as it stood, or a Dashboard ask. Absent on quotes from before it
// was recorded.
export type Quoted = { from: "feed" | "widget" | "ask" | "asked" | ""; title: string; widget?: DashboardWidget };

export function quotedFrom(s: string): Quoted["from"] {
  return s === "feed" || s === "widget" || s === "ask" || s === "asked" ? s : "";
}

// A quote the human put in a message. source is "message" (ref "reply" for
// the Bot's words, "own" for the human's), "file" (ref the workspace path) or
// "artifact" (ref artifact_id@version). locator is "lines 4–9", or x,y,w,h
// fractions with image the workspace path of the crop.
export type Quote = { source: "message" | "file" | "artifact"; ref: string; label: string; text: string; locator: string; image?: string };

// `at` is the client arrival time (ms); replayed history arrives in a burst.
// `createdAt` is the server's record time (RFC 3339) when it sent one.
export type Ev = {
  id?: string;
  kind: string;
  body: string;
  tool: string;
  runId?: string;
  attachments?: Attachment[];
  quotes?: Quote[];
  quoted?: Quoted;
  // A card (and a quote of one, and a card_state update): the card as it
  // stands.
  card?: DashboardCard;
  // A setup offer (and a setup_state update): the offer as it stands.
  setupOffer?: SetupOffer;
  at?: number;
  createdAt?: string;
};

// An artifact card's look at one version, made when it was pinned.
export type ArtifactPreview = { text?: string; image?: string; more?: boolean };

// One emit of an artifact. pinned: its bytes are kept as they were; not
// pinned (over the size caps), the card shows the live file and note says why.
export type ArtifactVersion = {
  n: number;
  name: string;
  kind: string;
  size?: number;
  pinned: boolean;
  note?: string;
  network?: boolean;
  entry?: string;
  files?: number;
  preview?: ArtifactPreview;
  runId?: string;
};

export type ArtifactType = "file" | "skill" | "app";

// A card: every version the Bot handed over at one path in this chat, oldest
// first. saved is the skill version the human saved.
export type ArtifactBlock = {
  key: string;
  type: "artifact";
  artifactId: string;
  artifactType: ArtifactType;
  title: string;
  path: string;
  versions: ArtifactVersion[];
  saved?: number;
  runId?: string;
};

// Why a tool stopped without a clean result: the human denied it, or the run
// was stopped/interrupted around it.
export type Outcome = "denied" | "stopped";

// What the owner's output rule did to a result before the Bot read it:
// passed on flagged or unchecked, or withheld (`original` is then what the
// Bot was not given, kept for the owner).
export type Filtered = { verdict: "flagged" | "withheld" | "unchecked"; mode: string; original?: string };

export type NestedCall = { key: string; title: string; name: string; result?: string; running?: boolean; waiting?: boolean; outcome?: Outcome; filter?: Filtered };

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
  filter?: Filtered;
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
  | { key: string; type: "user"; id?: string; runId?: string; text: string; attachments?: Attachment[]; quotes?: Quote[]; createdAt?: string; from?: string }
  // `bounds` are source offsets where each streamed delta began.
  | { key: string; type: "assistant"; text: string; streaming?: boolean; bounds?: number[] }
  | { key: string; type: "thinking"; text: string; streaming?: boolean; startAt?: number; ms?: number }
  | ToolBlock
  | ReceiptBlock
  | { key: string; type: "error"; text: string }
  // Subagents finished and woke the lead: `agents` are their names and
  // statuses, `text` the report the lead was given.
  | ReportBlock
  // What a chat opened from (see Quoted): `source` is where the Bot made it.
  | { key: string; type: "quote"; text: string; source: string; quoted?: Quoted; createdAt?: string }
  // The history before this point was summarized to fit the context window.
  // `running` while the summary is being written; empty text once settled
  // means it failed or was stopped.
  | CompactionBlock
  // An ask shown in the chat: the Bot's question as a card answered in
  // place. `asked` when it came from elsewhere (an automation) into this chat.
  | { key: string; type: "card"; card: DashboardCard; asked?: boolean; createdAt?: string }
  // Something the Bot offers to add (a connector, channel or drive).
  | { key: string; type: "offer"; offer: SetupOffer }
  // A slash command's mark in the thread: the context started over here
  // (/clear), or `count` messages were deleted for good (/wipe).
  | { key: string; type: "boundary"; kind: "cleared" | "wiped"; count: number; createdAt?: string }
  | ArtifactBlock
  // A later emit of a card further up: the card shows it, this row says so.
  | { key: string; type: "artifact_update"; artifactId: string; title: string; version: number; runId?: string };
