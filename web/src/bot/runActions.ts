import type { FormEvent } from "react";
import type { NavigateFunction } from "react-router-dom";
import { ui } from "../api";
import { fail } from "../errors";
import type { Approval, Bot, Chat } from "../gen/silo/v1/ui_pb";
import type { ComposerDraft } from "./useComposerDraft";

type Ctx = {
  id: string;
  chatId?: string;
  draft: ComposerDraft;
  run: { markSent: () => void; setSending: (v: boolean) => void; resync: () => void };
  setBot: (b: Bot) => void;
  setChats: (f: (xs: Chat[]) => Chat[]) => void;
  setPending: (xs: Approval[]) => void;
  onError: (message: string) => void;
  nav: NavigateFunction;
};

// The things a human does in a chat: send, edit, delete, diverge, pick a model
// or thinking level, compact, stop. Each clears the action error first and
// reports its own failure there.
export function runActions(c: Ctx) {
  const { id, chatId, draft, run, onError } = c;
  const refreshChats = () => ui.listChats({ botId: id }).then((r) => c.setChats(() => r.chats)).catch(() => {});

  async function send(e?: FormEvent) {
    e?.preventDefault();
    const { text, atts } = draft;
    if (!text.trim() && atts.length === 0) return;
    const msg = text.trim();
    draft.setText("");
    run.markSent();
    onError("");
    try {
      await ui.send({
        botId: id,
        chatId: chatId || "",
        text: msg,
        attachments: atts.map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
      });
      draft.setAtts([]);
      ui.getBot({ id }).then(c.setBot);
      void refreshChats();
    } catch (ex) {
      run.setSending(false);
      onError(fail(ex));
    }
  }

  async function editMessage(eventId: string, text: string, attachments?: { name: string; path: string; size: number }[]) {
    if (!chatId) return;
    onError("");
    try {
      await ui.editMessage({
        botId: id,
        chatId,
        eventId,
        text,
        attachments: (attachments ?? []).map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
      });
      run.resync();
      ui.getBot({ id }).then(c.setBot);
      void refreshChats();
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function deleteMessage(eventId: string) {
    if (!chatId) return;
    onError("");
    try {
      await ui.deleteMessage({ botId: id, chatId, eventId });
      run.resync();
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function divergeChat(eventId: string) {
    if (!chatId) return;
    onError("");
    try {
      const res = await ui.divergeChat({ botId: id, chatId, eventId });
      if (!res.chat) return;
      void refreshChats();
      c.nav(`/bots/${id}/run/${res.chat.id}`);
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function pickModel(model: string) {
    if (!chatId) return;
    onError("");
    try {
      const row = await ui.setChatModel({ botId: id, chatId, model });
      c.setChats((xs) => xs.map((x) => (x.id === row.id ? row : x)));
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function pickThinking(thinking: string) {
    if (!chatId) return;
    onError("");
    try {
      const row = await ui.setChatThinking({ botId: id, chatId, thinking });
      c.setChats((xs) => xs.map((x) => (x.id === row.id ? row : x)));
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function compactChat() {
    if (!chatId) return;
    onError("");
    run.markSent();
    try {
      await ui.compactChat({ botId: id, chatId });
    } catch (ex) {
      run.setSending(false);
      onError(fail(ex));
    }
  }

  async function stopRun() {
    if (!chatId) return;
    onError("");
    try {
      await ui.stopRun({ botId: id, chatId });
      ui.listApprovals({ botId: id }).then((r) => c.setPending(r.approvals)).catch(() => {});
      ui.getBot({ id }).then(c.setBot).catch(() => {});
    } catch (ex) {
      onError(fail(ex));
    }
  }

  return { send, editMessage, deleteMessage, divergeChat, pickModel, pickThinking, compactChat, stopRun };
}

export type RunActions = ReturnType<typeof runActions>;
