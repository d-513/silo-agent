import { Brain, Inbox, Library, Pencil, Power, SquarePen, Timer, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type FormEvent } from "react";
import { Link, Navigate, NavLink, useNavigate, useParams } from "react-router-dom";
import { ui } from "../api";
import { ApprovalSlip, ConnectorAuthSlip, SlipPresence } from "../Approval";
import { ArtifactOverlay, type Artifact } from "../Artifact";
import { useAuth } from "../auth";
import { AutomationsPane } from "../Automations";
import { BotChannels } from "../BotChannels";
import { BotConnectors, startConnectorAuth } from "../BotConnectors";
import { BotDrives } from "../BotDrives";
import { useBots } from "../bots";
import { Btn } from "../Btn";
import { Composer } from "../Composer";
import { Crest } from "../Crest";
import { fail, isGone } from "../errors";
import { ArmedButton, SaveButton, useSave } from "../Feedback";
import { FeedPane } from "../Feed";
import { Field, inputClass, Panel, SkeletonRows } from "../Field";
import { FilesPane } from "../Files";
import { chatWhen } from "../format";
import { joinPath } from "../fs";
import type { Approval, Bot, BotConnector, Chat, ModelOption, SecretMeta } from "../gen/silo/v1/ui_pb";
import { KnowledgePane } from "../Knowledge";
import { Lamp, StatusWord } from "../Lamp";
import { MemoriesPane } from "../Memories";
import { RulesPane } from "../Rules";
import { SettingsPane } from "../Settings";
import { BotSkills } from "../Skills";
import { SubagentPage } from "../SubagentPage";
import { SubagentTray } from "../SubagentTray";
import { Taskboard } from "../Taskboard";
import { Thread } from "../Thread";
import { useRunStream } from "../useRunStream";
import { useSubagents } from "../useSubagents";
import { BotTabs, machineBtnCompact, TabStrip } from "./BotTabs";
import { ContainersPane } from "./Containers";
import { FadeScroll } from "./FadeScroll";
import { MachinePane } from "./MachinePane";
import { SideChip, SideLink } from "./SideNav";
import { isNavTab, isSideTab, onChatSide, type Tab } from "./tabs";

export function BotPage() {
  const { admin } = useAuth();
  const { id, "*": splat } = useParams();
  const nav = useNavigate();
  const { refresh } = useBots();
  const parts = (splat ?? "").split("/").filter(Boolean);
  const tabParam = parts[0];
  const chatId = tabParam === "run" ? parts[1] : undefined;
  // /bots/:id/run/:chatId/agent/:agentId opens one of the chat's subagents.
  const agentId = chatId && parts[2] === "agent" ? parts[3] : undefined;
  const tab: Tab = chatId ? "run" : tabParam === "console" ? "console" : isNavTab(tabParam) || isSideTab(tabParam) ? tabParam : "run";
  const chatSide = onChatSide(tab);
  const [bot, setBot] = useState<Bot | null>(null);
  const [loadErr, setLoadErr] = useState("");
  const [text, setText] = useState("");
  const [pending, setPending] = useState<Approval[]>([]);
  const [authPrompt, setAuthPrompt] = useState<BotConnector | null>(null);
  const [secrets, setSecrets] = useState<SecretMeta[] | null>(null);
  const secretSaver = useSave();
  const [chats, setChats] = useState<Chat[]>([]);
  const [editingChat, setEditingChat] = useState("");
  const [editTitle, setEditTitle] = useState("");
  const renameCancel = useRef(false);
  const [secName, setSecName] = useState("");
  const [secVal, setSecVal] = useState("");
  const [actErr, setActErr] = useState("");
  const [atts, setAtts] = useState<{ name: string; path: string; size: number }[]>([]);
  const [attachErr, setAttachErr] = useState("");
  const [keepDesk, setKeepDesk] = useState(tab === "desktop");
  const [keepCon, setKeepCon] = useState(tab === "console");
  const [inspect, setInspect] = useState<Artifact | null>(null);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [defaultModel, setDefaultModel] = useState("");
  const [voice, setVoice] = useState(false);
  const subs = useSubagents(id, chatId);
  const { events, sending, setSending, usage, fresh, markSent, resync } = useRunStream(id, chatId, {
    onApproval: () => {
      if (id) ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {});
    },
    onTitle: (title) => setChats((xs) => xs.map((c) => (c.id === chatId ? { ...c, title } : c))),
    onDone: () => {
      if (id) ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
      subs.refresh();
    },
    onPing: () => subs.refresh(),
  });

  useEffect(() => {
    setKeepDesk(tab === "desktop");
    setKeepCon(tab === "console");
    setAuthPrompt(null);
    setLoadErr("");
  }, [id]);
  useEffect(() => {
    if (tab === "desktop") setKeepDesk(true);
    if (tab === "console") setKeepCon(true);
  }, [tab]);

  const settled = Boolean(bot && (bot.workerConnected || bot.status === "stopped"));
  useEffect(() => {
    if (!id) return;
    let dead = false;
    const tick = () => {
      ui.getBot({ id })
        .then((b) => {
          if (dead) return;
          setBot(b);
          setLoadErr("");
        })
        .catch((e) => {
          // Only a Bot that is gone (deleted, not ours) ends the page; a
          // blip keeps the last row and the next tick recovers.
          if (dead || !isGone(e)) return;
          setLoadErr(fail(e));
          clearInterval(t);
        });
      ui.listApprovals({ botId: id })
        .then((r) => {
          if (!dead) setPending(r.approvals);
        })
        .catch(() => {});
    };
    const t = setInterval(tick, settled ? 3000 : 500);
    tick();
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [id, settled]);

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

  useEffect(() => {
    if (!id || !chatId) return;
    ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {});
  }, [id, chatId]);

  useEffect(() => {
    if (!id || tab !== "secrets") return;
    ui.listSecrets({ botId: id }).then((r) => setSecrets(r.secrets)).catch((e) => {
      setSecrets([]);
      setActErr(fail(e));
    });
  }, [id, tab]);

  if (!id) return <Navigate to="/" />;
  if (!tabParam) return <Navigate to={`/bots/${id}/run`} replace />;
  if (!chatId && tabParam && tabParam !== "console" && !isNavTab(tabParam) && !isSideTab(tabParam)) return <Navigate to={`/bots/${id}/run`} replace />;
  if (loadErr) {
    return (
      <div className="p-7">
        <p className="mb-3 text-vermilion">{loadErr}</p>
        <Link to="/" className="text-cobalt">
          Back to Bots
        </Link>
      </div>
    );
  }
  if (!bot) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 items-center gap-3 px-4 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14">
          <div className="skeleton hidden h-7 w-7 rounded-sm wide:block" />
          <div className="skeleton hidden h-4 w-32 rounded-xs wide:block" />
        </div>
        <div className="flex min-h-0 flex-1">
          <div className="hidden w-[248px] shrink-0 bg-well p-2 pt-12 wide:block">
            <SkeletonRows rows={4} height={52} />
          </div>
          <div className="flex-1" />
        </div>
      </div>
    );
  }

  const chatRuns = new Set(events.map((e) => e.runId).filter(Boolean));
  const agentRuns = new Set(subs.agents.map((a) => a.runId).filter(Boolean));
  const waitingRuns = new Set(pending.map((p) => p.runId).filter((r) => r && agentRuns.has(r)));
  // A subagent's approval slip is this chat's too: the human is the same.
  const waiting = pending.some((p) => p.runId && (chatRuns.has(p.runId) || agentRuns.has(p.runId)));
  const agentsBusy = subs.agents.some((a) => a.running);

  function agentHref(name: string): string | undefined {
    const a = subs.agents.find((x) => x.name.toLowerCase() === name.toLowerCase());
    return a && id && chatId ? `/bots/${id}/run/${chatId}/agent/${a.id}` : undefined;
  }

  async function send(e?: FormEvent) {
    e?.preventDefault();
    if (!id || (!text.trim() && atts.length === 0)) return;
    const msg = text.trim();
    setText("");
    markSent();
    setActErr("");
    try {
      await ui.send({
        botId: id,
        chatId: chatId || "",
        text: msg,
        attachments: atts.map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
      });
      setAtts([]);
      ui.getBot({ id }).then(setBot);
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
    } catch (ex) {
      setSending(false);
      setActErr(fail(ex));
    }
  }

  async function editMessage(eventId: string, text: string, attachments?: { name: string; path: string; size: number }[]) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      await ui.editMessage({
        botId: id,
        chatId,
        eventId,
        text,
        attachments: (attachments ?? []).map((a) => ({ name: a.name, path: a.path, size: BigInt(a.size), mime: "" })),
      });
      resync();
      ui.getBot({ id }).then(setBot);
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function deleteMessage(eventId: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      await ui.deleteMessage({ botId: id, chatId, eventId });
      resync();
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function divergeChat(eventId: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      const res = await ui.divergeChat({ botId: id, chatId, eventId });
      if (!res.chat) return;
      ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
      nav(`/bots/${id}/run/${res.chat.id}`);
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function attach(list: FileList | null) {
    if (!list?.length || !id || !bot?.workerConnected) return;
    setAttachErr("");
    const added: { name: string; path: string; size: number }[] = [];
    for (const f of list) {
      if (f.size > 50 << 20) {
        setAttachErr(`${f.name} is larger than 50 MB`);
        continue;
      }
      const path = joinPath("tmp", f.name);
      if (!path) continue;
      try {
        const buf = new Uint8Array(await f.arrayBuffer());
        await ui.putFile({ botId: id, path, data: buf });
        added.push({ name: f.name, path, size: f.size });
      } catch (ex) {
        setAttachErr(fail(ex));
      }
    }
    if (added.length) {
      setAtts((xs) => {
        const seen = new Set(xs.map((x) => x.path));
        return [...xs, ...added.filter((a) => !seen.has(a.path))];
      });
    }
  }

  async function pickModel(model: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      const c = await ui.setChatModel({ botId: id, chatId, model });
      setChats((xs) => xs.map((x) => (x.id === c.id ? c : x)));
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function pickThinking(thinking: string) {
    if (!id || !chatId) return;
    setActErr("");
    try {
      const c = await ui.setChatThinking({ botId: id, chatId, thinking });
      setChats((xs) => xs.map((x) => (x.id === c.id ? c : x)));
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function compactChat() {
    if (!id || !chatId) return;
    setActErr("");
    markSent();
    try {
      await ui.compactChat({ botId: id, chatId });
    } catch (ex) {
      setSending(false);
      setActErr(fail(ex));
    }
  }

  async function stopRun() {
    if (!id || !chatId) return;
    setActErr("");
    try {
      await ui.stopRun({ botId: id, chatId });
      ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {});
      ui.getBot({ id }).then(setBot).catch(() => {});
    } catch (ex) {
      setActErr(fail(ex));
    }
  }

  async function start() {
    if (!id) return;
    setActErr("");
    try {
      setBot(await ui.startBot({ id }));
      refresh();
    } catch (e) {
      setActErr(fail(e));
    }
  }
  async function stop() {
    if (!id) return;
    setActErr("");
    try {
      setBot(await ui.stopBot({ id }));
      refresh();
    } catch (e) {
      setActErr(fail(e));
    }
  }

  async function saveSkill(a: Artifact) {
    if (!id) return;
    try {
      await ui.saveSkill({ botId: id, path: a.path, runId: a.runId ?? "" });
      setInspect((cur) => (cur && cur.path === a.path ? { ...cur, status: "saved", scope: "personal" } : cur));
    } catch (e) {
      setActErr(fail(e));
    }
  }

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
      setActErr(fail(e));
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

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="@container flex h-12 shrink-0 items-center gap-1 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:gap-3 wide:px-4">
        <span className="blink hidden wide:inline-flex">
          <Crest index={bot.crest} size={28} />
        </span>
        <h1 className="sr-only min-w-0 truncate text-[15px] leading-5 font-semibold tracking-[-0.01em] wide:not-sr-only wide:max-w-[12rem]">{bot.name}</h1>
        <span className="hidden shrink-0 items-center gap-2 wide:inline-flex">
          <Lamp status={bot.status} />
          <StatusWord status={bot.status} />
        </span>
        <TabStrip tab={tab} className="hidden min-h-0 flex-1 self-stretch wide:ml-2 wide:block" innerClass="flex h-full items-stretch gap-0.5">
          <BotTabs id={id} tab={tab} chatId={chatId} />
        </TabStrip>
        <TabStrip tab={tab} className="min-h-0 flex-1 self-stretch wide:hidden" innerClass="flex h-full items-stretch gap-0.5">
          <BotTabs id={id} tab={tab} chatId={chatId} splitMachine compact />
        </TabStrip>
        <div className="shrink-0">
          {bot.workerConnected ? (
            <Btn
              kind="ghost"
              title="Stop Bot"
              aria-label="Stop Bot"
              onClick={stop}
              className={machineBtnCompact}
              icon={<Power size={13} />}
            >
              <span className="@max-[1280px]:hidden">Stop Bot</span>
            </Btn>
          ) : (
            <Btn
              kind="ghost"
              title={bot.status === "starting" ? "Starting…" : "Start Bot"}
              aria-label={bot.status === "starting" ? "Starting" : "Start Bot"}
              onClick={start}
              disabled={bot.status === "starting"}
              className={machineBtnCompact}
              icon={<Power size={13} />}
            >
              <span className="@max-[1280px]:hidden">{bot.status === "starting" ? "Starting…" : "Start Bot"}</span>
            </Btn>
          )}
        </div>
      </header>
      {actErr && (
        <div role="alert" className="rise flex items-center gap-2 bg-vermilion-pale px-4 py-2 text-[13px] text-vermilion">
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
          <span className="min-w-0 flex-1">{actErr}</span>
          <button type="button" title="Dismiss" className="rounded-xs p-1 hover:bg-white/60" onClick={() => setActErr("")}>
            <X size={13} />
          </button>
        </div>
      )}
      <div
        className="relative flex min-h-0 flex-1"
        style={{ "--wash-left": chatSide ? "248px" : "0px" } as CSSProperties}
      >
        {chatSide && (
          <>
            <aside className="hidden w-[248px] shrink-0 flex-col bg-well wide:flex">
              <nav className="space-y-0.5 px-2 pt-2" aria-label="Conversation">
                <SideLink to={`/bots/${id}/automations`} on={tab === "automations"} icon={Timer} label="Automations" />
                <SideLink to={`/bots/${id}/memories`} on={tab === "memories"} icon={Brain} label="Memories" />
                <SideLink to={`/bots/${id}/knowledge`} on={tab === "knowledge"} icon={Library} label="Knowledge" />
                <SideLink to={`/bots/${id}/feed`} on={tab === "feed"} icon={Inbox} label="Feed" badge={tab === "feed" ? 0 : bot.feedUnread} />
              </nav>
              <div className="flex h-12 items-center justify-between pr-2 pl-4">
                <span className="text-[11px] leading-4 font-medium tracking-[0.08em] text-ink-3 uppercase">Chats</span>
                <Btn
                  kind="ghost"
                  size="sm"
                  iconOnly
                  title="New chat"
                  aria-label="New chat"
                  icon={<SquarePen size={15} />}
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                />
              </div>
              <div className="min-h-0 flex-1 space-y-0.5 overflow-auto px-2 pb-3">
                {chats.length === 0 && <p className="px-2 py-2 text-[12.5px] text-ink-3">No chats</p>}
                {chats.map((c) => {
                  const on = tab === "run" && c.id === chatId;
                  const live = on && (waiting || sending || agentsBusy);
                  return (
                    <div
                      key={c.id}
                      className={`group relative rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${ on ? "bg-surface shadow-card" : "hover:bg-pressed"
                      }`}
                    >
                      {editingChat === c.id ? (
                        <div className="px-1.5 py-1.5">
                          <input
                            autoFocus
                            className={`${inputClass} h-8 px-2 text-[13.5px]`}
                            value={editTitle}
                            onChange={(e) => setEditTitle(e.target.value)}
                            onBlur={() => {
                              if (renameCancel.current) {
                                renameCancel.current = false;
                                return;
                              }
                              void renameChat(c.id);
                            }}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault();
                                void renameChat(c.id);
                              }
                              if (e.key === "Escape") {
                                renameCancel.current = true;
                                setEditingChat("");
                              }
                            }}
                          />
                        </div>
                      ) : (
                        <NavLink
                          to={`/bots/${id}/run/${c.id}`}
                          onDoubleClick={(e) => {
                            e.preventDefault();
                            setEditingChat(c.id);
                            setEditTitle(c.title || "");
                          }}
                          className="block min-w-0 rounded-control px-3 py-2 pr-14"
                        >
                          <span className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-[13.5px] leading-5 font-medium text-ink">{c.title || "New chat"}</span>
                            {live ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
                          </span>
                          <span className={`block truncate text-[12.5px] leading-[18px] ${waiting && on ? "text-vermilion" : "text-ink-3"}`}>
                            {on && waiting ? "Waiting for you" : on && sending ? "Working…" : chatWhen(c.updatedAt)}
                          </span>
                        </NavLink>
                      )}
                      {editingChat !== c.id && (
                        <span className="absolute top-1.5 right-1.5 hidden items-center group-focus-within:flex group-hover:flex">
                          <button
                            type="button"
                            title="Rename chat"
                            className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
                            onClick={(e) => {
                              e.preventDefault();
                              setEditingChat(c.id);
                              setEditTitle(c.title || "");
                            }}
                          >
                            <Pencil size={13} />
                          </button>
                          <button
                            type="button"
                            title="Delete chat"
                            className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-vermilion-pale hover:text-vermilion"
                            onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                          >
                            <Trash2 size={13} />
                          </button>
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            </aside>
            <section className="flex min-w-0 flex-1 flex-col">
              <FadeScroll className="shrink-0 bg-well shadow-[inset_0_-1px_0_var(--color-line)] wide:hidden" innerClass="flex items-center gap-1 px-2 py-1.5" fade="from-well">
                <Btn
                  kind="ghost"
                  size="sm"
                  iconOnly
                  title="New chat"
                  aria-label="New chat"
                  className="h-10 w-10"
                  icon={<SquarePen size={15} />}
                  onClick={() => newChat().catch((e) => setActErr(fail(e)))}
                />
                <SideChip to={`/bots/${id}/automations`} on={tab === "automations"} icon={Timer} label="Automations" />
                <SideChip to={`/bots/${id}/memories`} on={tab === "memories"} icon={Brain} label="Memories" />
                <SideChip to={`/bots/${id}/knowledge`} on={tab === "knowledge"} icon={Library} label="Knowledge" />
                <SideChip to={`/bots/${id}/feed`} on={tab === "feed"} icon={Inbox} label="Feed" badge={bot.feedUnread} />
                <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-line-strong" />
                {chats.map((c) => {
                  const on = tab === "run" && c.id === chatId;
                  return (
                    <div
                      key={c.id}
                      className={`flex h-10 shrink-0 items-center rounded-control text-[13px] font-medium ${ on ? "bg-surface text-ink shadow-card" : "text-ink-2"
                      }`}
                    >
                      {editingChat === c.id ? (
                        <input
                          autoFocus
                          className={`${inputClass} h-8 w-40 px-2`}
                          value={editTitle}
                          onChange={(e) => setEditTitle(e.target.value)}
                          onBlur={() => {
                            if (renameCancel.current) {
                              renameCancel.current = false;
                              return;
                            }
                            void renameChat(c.id);
                          }}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              e.preventDefault();
                              void renameChat(c.id);
                            }
                            if (e.key === "Escape") {
                              renameCancel.current = true;
                              setEditingChat("");
                            }
                          }}
                        />
                      ) : (
                        <NavLink
                          to={`/bots/${id}/run/${c.id}`}
                          onDoubleClick={(e) => {
                            e.preventDefault();
                            setEditingChat(c.id);
                            setEditTitle(c.title || "");
                          }}
                          className={`flex h-10 max-w-[11rem] items-center gap-2 px-3 ${on ? "" : "rounded-control hover:bg-pressed hover:text-ink"}`}
                        >
                          <span className="truncate">{c.title || "New chat"}</span>
                          {on && (waiting || sending) ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
                        </NavLink>
                      )}
                      {on && editingChat !== c.id && (
                        <button
                          type="button"
                          title="Rename chat"
                          className="flex h-10 w-8 items-center justify-center text-ink-2 hover:text-ink"
                          onClick={(e) => {
                            e.preventDefault();
                            setEditingChat(c.id);
                            setEditTitle(c.title || "");
                          }}
                        >
                          <Pencil size={13} />
                        </button>
                      )}
                      {on && (
                        <button
                          type="button"
                          title="Delete chat"
                          className="flex h-10 w-8 items-center justify-center text-ink-2 hover:text-vermilion"
                          onClick={() => deleteChat(c.id).catch((e) => setActErr(fail(e)))}
                        >
                          <Trash2 size={13} />
                        </button>
                      )}
                    </div>
                  );
                })}
              </FadeScroll>
              {tab === "automations" && id && (
                <AutomationsPane
                  bot={bot}
                  sub={parts.slice(1)}
                  onError={setActErr}
                  onApprovals={() => ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {})}
                  onInspectArtifact={setInspect}
                  onSaveSkill={(a) => void saveSkill(a)}
                />
              )}
              {tab === "memories" && (
                <div className="min-h-0 min-w-0 flex-1 overflow-auto">
                  <MemoriesPane bot={bot} onSaved={setBot} onError={setActErr} />
                </div>
              )}
              {tab === "knowledge" && (
                <div className="min-h-0 min-w-0 flex-1 overflow-auto">
                  <KnowledgePane bot={bot} onError={setActErr} onStart={start} />
                </div>
              )}
              {tab === "feed" && (
                <div className="min-h-0 min-w-0 flex-1 overflow-auto">
                  <FeedPane
                    bot={bot}
                    onError={setActErr}
                    onQuoted={(c) => {
                      setChats((xs) => [c, ...xs.filter((x) => x.id !== c.id)]);
                      nav(`/bots/${id}/run/${c.id}`);
                    }}
                  />
                </div>
              )}
              {tab === "run" && agentId && chatId && (
                <SubagentPage
                  key={agentId}
                  botId={id!}
                  botName={bot.name}
                  botCrest={bot.crest}
                  chatId={chatId}
                  agentId={agentId}
                  onError={setActErr}
                  onApprovals={() => ui.listApprovals({ botId: id! }).then((r) => setPending(r.approvals)).catch(() => {})}
                  onInspectArtifact={setInspect}
                  onSaveSkill={(a) => void saveSkill(a)}
                />
              )}
              {tab === "run" && !agentId && (
              <>
              <Taskboard
                items={subs.board}
                agentHref={agentHref}
                onClear={() => {
                  if (!id || !chatId) return;
                  ui.clearTaskboard({ botId: id, chatId })
                    .then((b) => subs.setBoard(b.items))
                    .catch((e) => setActErr(fail(e)));
                }}
              />
              <Thread
                botId={id!}
                botName={bot.name}
                botCrest={bot.crest}
                chatId={chatId}
                events={events}
                sending={sending}
                fresh={fresh}
                onInspectArtifact={setInspect}
                onSaveSkill={(a) => void saveSkill(a)}
                onSelectPrompt={(p) => setText(p)}
                onEditMessage={(eid, t, a) => void editMessage(eid, t, a)}
                onDeleteMessage={(eid) => void deleteMessage(eid)}
                onDivergeChat={(eid) => void divergeChat(eid)}
                agentHref={agentHref}
              />
              {chatId ? (
                <SubagentTray
                  botId={id!}
                  chatId={chatId}
                  agents={subs.agents}
                  waitingRuns={waitingRuns}
                  onStop={(sid) => {
                    ui.stopSubagent({ botId: id!, id: sid }).then(subs.refresh).catch((e) => setActErr(fail(e)));
                  }}
                  onStopAll={() => {
                    for (const a of subs.agents.filter((x) => x.running)) {
                      ui.stopSubagent({ botId: id!, id: a.id }).catch((e) => setActErr(fail(e)));
                    }
                    setTimeout(subs.refresh, 300);
                  }}
                />
              ) : null}
              <Composer
                text={text}
                setText={setText}
                atts={atts}
                onRemoveAtt={(path) => setAtts((xs) => xs.filter((x) => x.path !== path))}
                attachErr={attachErr}
                onAttach={attach}
                onSend={() => void send()}
                onStop={() => void stopRun()}
                sending={sending}
                chatId={chatId}
                workerConnected={bot.workerConnected}
                botName={bot.name}
                models={models}
                model={chats.find((c) => c.id === chatId)?.model || defaultModel}
                onModel={(m) => void pickModel(m)}
                thinking={chats.find((c) => c.id === chatId)?.thinking ?? ""}
                onThinking={(l) => void pickThinking(l)}
                usage={usage}
                onCompact={() => void compactChat()}
                voice={voice}
                onTranscribe={(audio, mime) => ui.transcribe({ botId: id, audio, mime }).then((r) => r.text)}
                onCollect={() => ui.collectMemories({ botId: id!, chatId: chatId! })}
              />
              </>
              )}
            </section>
          </>
        )}
        {tab === "desktop" || keepDesk ? (
          <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "desktop" ? "flex" : "hidden"}`}>
            <MachinePane bot={bot} onStart={start} visible={tab === "desktop"} kind="desktop" />
            <p className="mt-2 text-[12.5px] text-ink-2">Same browser the Bot uses. You can type and click.</p>
          </section>
        ) : null}
        {tab === "console" || keepCon ? (
          <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "console" ? "flex" : "hidden"}`}>
            <MachinePane bot={bot} onStart={start} visible={tab === "console"} kind="console" />
            <p className="mt-2 text-[12.5px] text-ink-2">A shell on this Bot, started in /workspace.</p>
          </section>
        ) : null}
        {tab === "files" && (
          <section className="flex min-h-0 min-w-0 flex-1 flex-col">
            <FilesPane bot={bot} onStart={start} />
          </section>
        )}
        {tab === "connectors" && id && (
          <section className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotConnectors botId={id} onNeedAuth={setAuthPrompt} />
          </section>
        )}
        {tab === "drives" && id && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotDrives botId={id} sub={parts.slice(1)} admin={admin} />
          </div>
        )}
        {tab === "channels" && id && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotChannels botId={id} sub={parts.slice(1)} />
          </div>
        )}
        {tab === "skills" && id && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <BotSkills botId={id} />
          </div>
        )}
        {tab === "secrets" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
          <div className="silo-page">
            <h2 className="text-title">Secrets</h2>
            <p className="mb-6 text-ink-2">Handed to the Bot only after you allow it. Masked before the model sees output.</p>
            <Panel title="Stored secrets" padded={false}>
              {secrets === null ? (
                <SkeletonRows rows={2} height={52} className="p-3" />
              ) : secrets.length === 0 ? (
                <p className="px-5 py-4 text-[13px] text-ink-3">No secrets on this Bot yet.</p>
              ) : (
                secrets.map((s) => (
                  <div key={s.id} className="flex items-center justify-between gap-3 px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
                    <div className="min-w-0">
                      <div className="truncate font-mono text-[13px] font-medium">{s.name}</div>
                      <div className="font-mono text-[12px] text-ink-3">•••••••• · {s.lastUsedAt || "never used"}</div>
                    </div>
                    <ArmedButton
                      kind="ghost"
                      size="sm"
                      onConfirm={async () => {
                        await ui.deleteSecret({ botId: id, id: s.id });
                        setSecrets((xs) => (xs ?? []).filter((x) => x.id !== s.id));
                      }}
                    >
                      Delete
                    </ArmedButton>
                  </div>
                ))
              )}
            </Panel>
            <Panel title="Add a secret" className="mt-4">
              <form
                className="flex flex-col gap-3 wide:flex-row wide:items-end"
                onSubmit={async (e) => {
                  e.preventDefault();
                  try {
                    await secretSaver.run(() => ui.addSecret({ botId: id, name: secName, value: secVal }));
                    setSecName("");
                    setSecVal("");
                    setSecrets((await ui.listSecrets({ botId: id })).secrets);
                  } catch (ex) {
                    setActErr(fail(ex));
                  }
                }}
              >
                <Field label="Name" className="flex-1">
                  <input className={`${inputClass} font-mono`} placeholder="vendor_password" value={secName} onChange={(e) => setSecName(e.target.value)} />
                </Field>
                <Field label="Value" className="flex-1">
                  <input type="password" className={`${inputClass} font-mono`} placeholder="••••••••" value={secVal} onChange={(e) => setSecVal(e.target.value)} />
                </Field>
                <SaveButton type="submit" state={secretSaver.state} savedLabel="Added" disabled={!secName.trim() || !secVal}>
                  Add
                </SaveButton>
              </form>
            </Panel>
          </div>
          </div>
        )}
        {tab === "container" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <ContainersPane
              bot={bot}
              onStart={start}
              onStop={stop}
              onChanged={(next) => {
                setBot(next);
                refresh();
              }}
            />
          </div>
        )}
        {tab === "settings" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <SettingsPane
              bot={bot}
              onSaved={(next) => {
                setBot(next);
                refresh();
              }}
              onError={setActErr}
              onRefresh={refresh}
            />
          </div>
        )}
        {tab === "rules" && (
          <div className="min-h-0 min-w-0 flex-1 overflow-auto">
            <RulesPane botId={id} />
          </div>
        )}
        <SlipPresence show={!!(pending[0] || authPrompt?.connector)}>
          {pending[0] ? (
            <ApprovalSlip
              key={pending[0].id}
              bot={bot}
              approval={pending[0]}
              onDecide={async (decision) => {
                await ui.decideApproval({ id: pending[0].id, decision });
                setPending((xs) => xs.slice(1));
              }}
              onAutoApprove={async () => {
                try {
                  await ui.setRule({
                    botId: bot.id,
                    connector: pending[0].connector,
                    action: pending[0].action,
                    decision: "auto",
                  });
                  await ui.decideApproval({ id: pending[0].id, decision: "allow_once" });
                  setPending((xs) => xs.slice(1));
                } catch (e) {
                  setActErr(fail(e));
                }
              }}
            />
          ) : authPrompt?.connector ? (
            <ConnectorAuthSlip
              bot={bot}
              name={authPrompt.connector.name}
              onAuthorize={async () => {
                try {
                  await startConnectorAuth(bot.id, authPrompt.id);
                  setAuthPrompt(null);
                } catch (e) {
                  setActErr(fail(e));
                }
              }}
              onLater={() => setAuthPrompt(null)}
            />
          ) : null}
        </SlipPresence>
        {inspect && id ? (
          <ArtifactOverlay botId={id} artifact={inspect} onSave={(a) => void saveSkill(a)} onClose={() => setInspect(null)} />
        ) : null}
      </div>
    </div>
  );
}
