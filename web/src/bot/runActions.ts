import type { FormEvent } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { UI } from "../gen/silo/v1/ui_pb";
import { reload, reloadBot } from "../query";
import { putChat } from "./useChatList";
import type { ComposerDraft } from "./useComposerDraft";

type Ctx = {
  id: string;
  chatId?: string;
  draft: ComposerDraft;
  run: { markSent: () => void; setSending: (v: boolean) => void; resync: () => void };
  onError: (message: string) => void;
  // Open another chat of this Bot.
  openChat: (chatId: string) => void;
};

// The things a human does in a chat: send, edit, delete, diverge, pick a model
// or thinking level, compact, stop. Each clears the action error first and
// reports its own failure there.
export function runActions(c: Ctx) {
  const { id, chatId, draft, run, onError } = c;
  const refreshChats = () => reload(UI.method.listChats, { botId: id });

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
      reloadBot(id);
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
      reloadBot(id);
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
      c.openChat(res.chat.id);
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function pickModel(model: string) {
    if (!chatId) return;
    onError("");
    try {
      putChat(id, await ui.setChatModel({ botId: id, chatId, model }));
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function pickThinking(thinking: string) {
    if (!chatId) return;
    onError("");
    try {
      putChat(id, await ui.setChatThinking({ botId: id, chatId, thinking }));
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
      void reload(UI.method.listApprovals, { botId: id });
      reloadBot(id);
    } catch (ex) {
      onError(fail(ex));
    }
  }

  return { send, editMessage, deleteMessage, divergeChat, pickModel, pickThinking, compactChat, stopRun };
}

export type RunActions = ReturnType<typeof runActions>;
