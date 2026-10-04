import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ui } from "../api";
import { fail } from "../errors";
import type { Chat, ModelOption } from "../gen/silo/v1/ui_pb";
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

// useChatList loads a Bot's chats and models while a chat-side page is open,
// opens the newest chat when none is picked, and owns create, rename and delete.
export function useChatList(id: string | undefined, route: { tab: Tab; chatSide: boolean; chatId?: string }, onError: (message: string) => void) {
  const { tab, chatSide, chatId } = route;
  const nav = useNavigate();
  const [chats, setChats] = useState<Chat[]>([]);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [defaultModel, setDefaultModel] = useState("");
  const [voice, setVoice] = useState(false);
  const [editingChat, setEditingChat] = useState("");
  const [editTitle, setEditTitle] = useState("");
  const renameCancel = useRef(false);

  useEffect(() => {
    if (!id || !chatSide) return;
    let dead = false;
    ui.listChats({ botId: id })
      .then((r) => {
        if (dead) return;
        setChats(r.chats);
        if (tab === "run" && !chatId && r.chats[0]) nav(`/bots/${id}/run/${r.chats[0].id}`, { replace: true });
      })
      .catch(() => {});
    ui.listModels({ botId: id })
      .then((r) => {
        if (!dead) {
          setModels(r.models);
          setDefaultModel(r.defaultModel);
          setVoice(r.voiceEnabled);
        }
      })
      .catch(() => {});
    return () => {
      dead = true;
    };
  }, [id, tab, chatSide, chatId, nav]);

  async function renameChat(cid: string) {
    if (!id) return;
    const title = editTitle.trim();
    setEditingChat("");
    if (!title) return;
    const cur = chats.find((x) => x.id === cid);
    if (cur && cur.title === title) return;
    try {
      const row = await ui.renameChat({ botId: id, id: cid, title });
      setChats((xs) => xs.map((c) => (c.id === cid ? row : c)));
    } catch (e) {
      onError(fail(e));
    }
  }

  async function newChat() {
    if (!id) return;
    const c = await ui.createChat({ botId: id });
    setChats((xs) => [c, ...xs]);
    nav(`/bots/${id}/run/${c.id}`);
  }

  async function deleteChat(cid: string) {
    if (!id) return;
    await ui.deleteChat({ botId: id, id: cid });
    const next = chats.filter((x) => x.id !== cid);
    setChats(next);
    if (cid === chatId) {
      if (next[0]) nav(`/bots/${id}/run/${next[0].id}`);
      else {
        const created = await ui.createChat({ botId: id });
        setChats([created]);
        nav(`/bots/${id}/run/${created.id}`);
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

  return { chats, setChats, models, defaultModel, voice, rename, newChat, deleteChat };
}
