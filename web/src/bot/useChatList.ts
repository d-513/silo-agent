import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { chatLink } from "../links";
import { ui } from "../api";
import { fail } from "../errors";
import { UI, type Chat } from "../gen/silo/v1/ui_pb";
import { patch, recheck } from "../query";
import type { Tab } from "./tabs";

// Renaming a chat in place: the title being typed, and the one commit/cancel
// contract both chat lists (sidebar and narrow strip) share.
export type ChatRename = {
  editing: string;
  title: string;
  setTitle: (t: string) => void;
  begin: (c: Chat) => void;
  commit: (cid: string) => void;
  // Escape: leave without saving, and swallow the blur that follows.
  cancel: () => void;
  blur: (cid: string) => void;
};

/** Edit a Bot's cached chat list in place (a rename, a new chat, a new title). */
export function patchChats(botId: string, f: (xs: Chat[]) => Chat[]) {
  patch(UI.method.listChats, { botId }, (r) => ({ ...r, chats: f(r.chats) }));
}

/** Put one changed chat row back into the list. */
export function putChat(botId: string, row: Chat) {
  patchChats(botId, (xs) => xs.map((c) => (c.id === row.id ? row : c)));
}

// useChatList loads a Bot's chats and models while a chat-side page is open,
// opens the newest chat when none is picked, and owns create, rename and delete.
export function useChatList(id: string | undefined, route: { tab: Tab; chatSide: boolean; chatId?: string }, onError: (message: string) => void) {
  const { tab, chatSide, chatId } = route;
  const nav = useNavigate();
  const on = id && chatSide ? { botId: id } : skipToken;
  const chatsQ = useQuery(UI.method.listChats, on);
  const modelsQ = useQuery(UI.method.listModels, on);
  const chats = chatsQ.data?.chats ?? [];
  const [editingChat, setEditingChat] = useState("");
  const [editTitle, setEditTitle] = useState("");
  const renameCancel = useRef(false);

  // Moving between chats and side pages rereads the list: a channel or an
  // automation may have added to it.
  useEffect(() => {
    if (id && chatSide) void recheck(UI.method.listChats, { botId: id });
  }, [id, tab, chatSide, chatId]);

  const newest = chatsQ.data?.chats[0]?.id;
  useEffect(() => {
    if (id && tab === "run" && !chatId && newest) void nav({ ...chatLink(id, newest), replace: true });
  }, [id, tab, chatId, newest, nav]);

  async function renameChat(cid: string) {
    if (!id) return;
    const title = editTitle.trim();
    setEditingChat("");
    if (!title) return;
    const cur = chats.find((x) => x.id === cid);
    if (cur && cur.title === title) return;
    try {
      putChat(id, await ui.renameChat({ botId: id, id: cid, title }));
    } catch (e) {
      onError(fail(e));
    }
  }

  async function newChat() {
    if (!id) return;
    const c = await ui.createChat({ botId: id });
    patchChats(id, (xs) => [c, ...xs]);
    void nav(chatLink(id, c.id));
  }

  async function deleteChat(cid: string) {
    if (!id) return;
    await ui.deleteChat({ botId: id, id: cid });
    const next = chats.filter((x) => x.id !== cid);
    patchChats(id, () => next);
    if (cid === chatId) {
      if (next[0]) void nav(chatLink(id, next[0].id));
      else {
        const created = await ui.createChat({ botId: id });
        patchChats(id, () => [created]);
        void nav(chatLink(id, created.id));
      }
    }
  }

  const rename: ChatRename = {
    editing: editingChat,
    title: editTitle,
    setTitle: setEditTitle,
    begin: (c) => {
      setEditingChat(c.id);
      setEditTitle(c.title || "");
    },
    commit: (cid) => void renameChat(cid),
    cancel: () => {
      renameCancel.current = true;
      setEditingChat("");
    },
    blur: (cid) => {
      if (renameCancel.current) {
        renameCancel.current = false;
        return;
      }
      void renameChat(cid);
    },
  };

  return {
    chats,
    models: modelsQ.data?.models ?? [],
    defaultModel: modelsQ.data?.defaultModel ?? "",
    voice: modelsQ.data?.voiceEnabled ?? false,
    rename,
    newChat,
    deleteChat,
  };
}
