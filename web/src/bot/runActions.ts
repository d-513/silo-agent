import type { FormEvent } from "react";
import { ui } from "../api";
import type { Said } from "../composer/Composer";
import { fail } from "../errors";
import { UI, type SendResponse } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { reload, reloadBot } from "../query";
import { deviceZone } from "../zones";
import { putChat } from "./useChatList";
import type { ComposerDraft } from "./useComposerDraft";
import { quoteToProto, type Quote } from "../quote/model";

// The model and thinking level chosen for a chat that does not exist yet.
export type ChatPick = { model: string; thinking: string };

type Ctx = {
  id: string;
  chatId?: string;
  // On /run/new: the first send makes the chat, with what was picked there.
  fresh?: boolean;
  pick: ChatPick;
  setPick: (p: ChatPick) => void;
  draft: ComposerDraft;
  run: { markSent: () => void; setSending: (v: boolean) => void; resync: () => void };
  onError: (message: string) => void;
  // Open another chat of this Bot, or the empty New chat page.
  openChat: (chatId: string) => void;
  openNew: () => void;
  // What a slash command said back (null clears it).
  onSaid: (s: Said | null) => void;
  // Opens the slash menu (/help).
  showCommands: () => void;
};

// saidFor words what a command did in the reader's language from the
// command's name and value; the server's English notice is the fallback.
function saidFor(res: SendResponse): string {
  const v = res.value;
  switch (res.command) {
    case "clear":
      return v === "empty" ? t("The context is already empty.") : t("Context cleared. The Bot starts fresh from here; its memories stay.");
    case "wipe":
      return v === "0" ? t("There is nothing to wipe.") : t("Wiped {{count}} messages.", { count: Number(v) || 0 });
    case "model":
      return v ? t("This chat now uses {{model}}.", { model: v }) : res.notice;
    case "thinking":
      return v ? t("Thinking: {{level}}.", { level: v }) : res.notice;
    case "stop":
      return v === "idle" ? t("Nothing is running.") : t("Stopped.");
    case "rename":
      return t("Renamed to {{title}}.", { title: v });
    case "remember": {
      const [saved, updated, forgotten] = v.split("/").map(Number);
      if (!saved && !updated && !forgotten) return t("Nothing new to remember.");
      return t("Memories: {{saved}} saved, {{updated}} updated, {{forgotten}} forgotten.", { saved, updated, forgotten });
    }
    case "compact":
      return "";
  }
  return res.notice;
}

// The things a human does in a chat: send, edit, delete, diverge, pick a model
// or thinking level, compact, stop. Each clears the action error first and
// reports its own failure there.
export function runActions(c: Ctx) {
  const { id, chatId, draft, run, onError } = c;
  const refreshChats = () => reload(UI.method.listChats, { botId: id });

  // send sends the draft, or `typed` (a command picked from the menu).
  async function send(e?: FormEvent, typed?: string, confirmed = false) {
    e?.preventDefault();
    const { text, atts, quotes } = typed === undefined ? draft : { text: typed, atts: [], quotes: [] };
    if (!text.trim() && atts.length === 0 && quotes.length === 0) return;
    const msg = text.trim();
    if (typed === undefined) draft.setText("");
    const command = msg.startsWith("/") && !msg.startsWith("//") && atts.length === 0 && quotes.length === 0;
    if (!command) run.markSent();
    onError("");
    c.onSaid(null);
    try {
      const fresh = !chatId && !!c.fresh;
      const res = await ui.send({
        botId: id,
        chatId: chatId || "",
        newChat: fresh,
        model: fresh ? c.pick.model : "",
        thinking: fresh ? c.pick.thinking : "",
        text: msg,
        attachments: atts.map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
        quotes: quotes.map(quoteToProto),
        // Where the human is now: an unpinned Bot follows it.
        timeZone: deviceZone(),
        confirmed,
      });
      if (res.command) return afterCommand(res, msg);
      if (command) run.markSent();
      draft.setAtts([]);
      draft.setQuotes([]);
      reloadBot(id);
      // The new chat is in the list before its page opens, so the sidebar
      // lights it and the composer shows its model.
      if (fresh) {
        await refreshChats().catch(() => {});
        c.openChat(res.chatId);
      } else void refreshChats();
    } catch (ex) {
      run.setSending(false);
      if (!command) return onError(fail(ex));
      // A refused command is the answer to it: shown where it was typed,
      // with the text back to fix.
      if (typed === undefined) draft.setText(msg);
      c.onSaid({ text: fail(ex) });
    }
  }

  // afterCommand shows what a command did, asks what it asks, and follows it
  // where it leads.
  function afterCommand(res: SendResponse, typed: string) {
    if (res.confirm) {
      const n = Number(res.value) || 0;
      c.onSaid({
        text: t("Delete {{count}} messages for good? Tool output and files shown in them go too; memories stay.", { count: n }),
        confirm: { label: t("Wipe"), run: () => void send(undefined, typed, true) },
      });
      return;
    }
    if (res.command === "help") return c.showCommands();
    if (res.newChat) return c.openNew();
    void refreshChats();
    reloadBot(id);
    if (res.chatId && res.chatId !== chatId) return c.openChat(res.chatId);
    if (res.runId) run.markSent();
    if (res.command === "wipe") run.resync();
    const said = saidFor(res);
    if (said) c.onSaid({ text: said });
  }

  async function editMessage(eventId: string, text: string, attachments?: { name: string; path: string; size: number }[], quotes?: Quote[]) {
    if (!chatId) return;
    onError("");
    try {
      await ui.editMessage({
        botId: id,
        chatId,
        eventId,
        text,
        attachments: (attachments ?? []).map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
        quotes: (quotes ?? []).map(quoteToProto),
        timeZone: deviceZone(),
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
    if (!chatId && c.fresh) return c.setPick({ ...c.pick, model });
    if (!chatId) return;
    onError("");
    try {
      putChat(id, await ui.setChatModel({ botId: id, chatId, model }));
    } catch (ex) {
      onError(fail(ex));
    }
  }

  async function pickThinking(thinking: string) {
    if (!chatId && c.fresh) return c.setPick({ ...c.pick, thinking });
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

  return { send, sendCommand: (text: string) => void send(undefined, text), editMessage, deleteMessage, divergeChat, pickModel, pickThinking, compactChat, stopRun };
}

export type RunActions = ReturnType<typeof runActions>;
