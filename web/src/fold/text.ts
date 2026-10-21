import { closeThinking, lastThinking } from "./find.ts";
import { reportAgents, type Block, type Ev } from "./types.ts";

// Handlers for what is said: the user's turn, the model's thinking and reply,
// quotes a chat opened from and subagent reports. Each folds one event into `out`.
export type Handler = (out: Block[], e: Ev, key: string) => void;

export const user: Handler = (out, e, key) => {
  out.push({ key, type: "user", id: e.id, runId: e.runId, text: e.body, attachments: e.attachments, quotes: e.quotes, createdAt: e.createdAt, from: e.tool || undefined });
};

export const subagentReport: Handler = (out, e, key) => {
  out.push({ key, type: "report", text: e.body, agents: reportAgents(e.tool), runId: e.runId, createdAt: e.createdAt });
};

export const feedQuote: Handler = (out, e, key) => {
  // A question the Bot asked from elsewhere is drawn as its card, live.
  if (e.card) {
    out.push({ key, type: "card", card: e.card, asked: true, createdAt: e.createdAt });
    return;
  }
  out.push({ key, type: "quote", text: e.body, source: e.tool, quoted: e.quoted, createdAt: e.createdAt });
};

// card is an ask the Bot showed in this chat.
export const card: Handler = (out, e, key) => {
  if (e.card) out.push({ key, type: "card", card: e.card, createdAt: e.createdAt });
};

// offer is a setup the Bot offered in this chat; offerState is the owner
// answering it.
export const offer: Handler = (out, e, key) => {
  if (e.setupOffer) out.push({ key, type: "offer", offer: e.setupOffer });
};

export const offerState: Handler = (out, e) => {
  const o = e.setupOffer;
  if (!o) return;
  for (const b of out) if (b.type === "offer" && b.offer.id === o.id) b.offer = o;
};

// cardState is a card in the chat changing (answered, put away, a setup
// done): every block that shows it takes the new state.
export const cardState: Handler = (out, e) => {
  const c = e.card;
  if (!c) return;
  for (const b of out) if (b.type === "card" && b.card.id === c.id) b.card = c;
};

export const thinkingChunk: Handler = (out, e, key) => {
  const last = out[out.length - 1];
  if (last?.type === "thinking") {
    last.text += e.body;
    last.streaming = true;
  } else {
    out.push({ key, type: "thinking", text: e.body, streaming: true, startAt: e.at });
  }
};

export const thinking: Handler = (out, e, key) => {
  const t = lastThinking(out);
  if (t) {
    t.text = e.body || t.text;
    if (t.streaming && t.startAt && e.at) t.ms = e.at - t.startAt;
    t.streaming = false;
  } else {
    out.push({ key, type: "thinking", text: e.body, streaming: false });
  }
};

// A streamed piece of the reply: it extends the open assistant block, noting
// where each delta began.
export const chunk: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  const last = out[out.length - 1];
  if (last?.type === "assistant") {
    (last.bounds ??= [0]).push(last.text.length);
    last.text += e.body;
    last.streaming = true;
  } else {
    out.push({ key, type: "assistant", text: e.body, streaming: true, bounds: [0] });
  }
};

// The finished reply (or a delivered section): it settles a streaming block, or
// is a block of its own when it has text.
export const assistant: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  const last = out[out.length - 1];
  if (last?.type === "assistant" && last.streaming) {
    last.text = e.body || last.text;
    last.streaming = false;
    last.bounds = undefined;
  } else if (e.body.trim()) {
    out.push({ key, type: "assistant", text: e.body });
  }
};
