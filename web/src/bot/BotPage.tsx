import { Link, Outlet, useMatches, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { X } from "lucide-react";
import { Suspense, useEffect, useRef, useState, type CSSProperties } from "react";
import { ui } from "../api";
import type { Artifact } from "../Artifact";
import { useAuth } from "../auth";
import { fail } from "../errors";
import { SkeletonRows } from "../Field";
import { UI, type BotConnector } from "../gen/silo/v1/ui_pb";
import { lazyNamed } from "../lazyNamed";
import { agentLink, chatLink } from "../links";
import { reload, setBot } from "../query";
import { useRunStream } from "../useRunStream";
import { useSubagents } from "../useSubagents";
import { useWide } from "../useWide";
import { BotPageCtx } from "./context";
import { BotSidebar } from "./BotSidebar";
import { BotSlip } from "./BotSlip";
import { BotStrip } from "./BotStrip";
import { MachineRail } from "./MachineRail";
import { setDocked, useDocked } from "./paneStore";
import { runActions } from "./runActions";
import { SidePane } from "./SidePane";
import { onChatSide, paneOf, type PaneKind } from "./tabs";
import { useBotLive } from "./useBotLive";
import { patchChats, useChatList } from "./useChatList";
import { useComposerDraft } from "./useComposerDraft";
import { useSecrets } from "./useSecrets";

const ArtifactOverlay = lazyNamed(() => import("../ArtifactOverlay"), "ArtifactOverlay");

function LoadingBot() {
  return (
    <div className="flex h-full">
      <div className="hidden w-[248px] shrink-0 bg-well px-2 wide:block">
        <div className="flex h-14 items-center px-3">
          <div className="skeleton h-4 w-32 rounded-xs" />
        </div>
        <div className="skeleton mb-3 h-10 rounded-control" />
        <SkeletonRows rows={5} height={36} />
      </div>
      <div className="flex-1" />
      <div className="hidden w-12 shrink-0 bg-well wide:block" />
    </div>
  );
}

// Where each machine pane lives as a page of its own.
const paneTo = {
  files: "/bots/$botId/files",
  desktop: "/bots/$botId/desktop",
  console: "/bots/$botId/console",
} as const;

// BotPage is the /bots/$botId layout: the sidebar on the left, the open page
// (bot/routes.tsx) in its <Outlet>, and the machine on the right — the rail,
// and the pane it docks beside the page. It owns what must survive moving
// between pages (the live Bot row, the chat list, a half-written message, the
// run stream) and hands it to those pages through useBotPage().
export function BotPage() {
  const { admin } = useAuth();
  const { botId: id } = useParams({ from: "/_authed/bots/$botId" });
  const { chatId, agentId } = useParams({ strict: false });
  // The tab is whatever the matched route says it lights (router.tsx).
  const tab = useMatches({ select: (ms) => ms.map((m) => m.staticData.tab).filter(Boolean).pop() }) ?? "run";
  const chatSide = onChatSide(tab);
  const navigate = useNavigate();
  const [actErr, setActErr] = useState("");
  const { bot, loadErr, pending, reloadApprovals } = useBotLive(id, chatId);
  const { chats, models, defaultModel, voice, rename, newChat, deleteChat } = useChatList(id, { tab, chatSide, chatId }, setActErr);
  const draft = useComposerDraft(id, bot?.workerConnected);
  const secrets = useSecrets(id, tab === "secrets", setActErr);
  const [authPrompt, setAuthPrompt] = useState<BotConnector | null>(null);
  // A machine pane is its own page on its route (`max`); on the chat side it
  // is the one docked beside the page, where there is room for that. Customize
  // and Settings keep their full width: a docked pane waits (still connected)
  // until the chat is back.
  const wide = useWide();
  const docked = useDocked();
  const max = paneOf(tab) !== "";
  const pane = paneOf(tab) || (wide && chatSide ? docked.kind : "");
  const { open } = useSearch({ strict: false });
  const [openAt, setOpenAt] = useState(open ?? "");
  const lastChat = useRef<string | undefined>(undefined);
  const [inspect, setInspect] = useState<Artifact | null>(null);
  const subs = useSubagents(id, chatId);
  const run = useRunStream(id, chatId, {
    onApproval: reloadApprovals,
    onTitle: (title) => patchChats(id, (xs) => xs.map((c) => (c.id === chatId ? { ...c, title } : c))),
    onDone: () => {
      void reload(UI.method.listChats, { botId: id });
      subs.refresh();
    },
    onPing: () => subs.refresh(),
  });

  useEffect(() => {
    setAuthPrompt(null);
    lastChat.current = undefined;
  }, [id]);
  // Docking a pane back goes to the chat it was opened from.
  useEffect(() => {
    if (chatId) lastChat.current = chatId;
  }, [chatId]);
  // Files keeps the folder a link opened it in after it is docked.
  useEffect(() => {
    if (open) setOpenAt(open);
  }, [open]);

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
  if (!bot) return <LoadingBot />;

  const chatRuns = new Set(run.events.map((e) => e.runId).filter(Boolean));
  const agentRuns = new Set(subs.agents.map((a) => a.runId).filter(Boolean));
  const waitingRuns = new Set(pending.map((p) => p.runId).filter((r) => r && agentRuns.has(r)));
  // A subagent's approval slip is this chat's too: the human is the same.
  const waiting = pending.some((p) => p.runId && (chatRuns.has(p.runId) || agentRuns.has(p.runId)));
  const agentsBusy = subs.agents.some((a) => a.running);

  function agentHref(name: string) {
    const a = subs.agents.find((x) => x.name.toLowerCase() === name.toLowerCase());
    return a && chatId ? agentLink(id, chatId, a.id) : undefined;
  }

  async function start() {
    setActErr("");
    try {
      setBot(await ui.startBot({ id }));
    } catch (e) {
      setActErr(fail(e));
    }
  }
  async function stop() {
    setActErr("");
    try {
      setBot(await ui.stopBot({ id }));
    } catch (e) {
      setActErr(fail(e));
    }
  }

  async function saveSkill(a: Artifact) {
    try {
      await ui.saveSkill({ botId: id, path: a.path, runId: a.runId ?? "" });
      setInspect((cur) => (cur && cur.path === a.path ? { ...cur, status: "saved", scope: "personal" } : cur));
    } catch (e) {
      setActErr(fail(e));
    }
  }
  const onSaveSkill = (a: Artifact) => void saveSkill(a);
  const onNewChat = () => void newChat().catch((e) => setActErr(fail(e)));
  const onDeleteChat = (cid: string) => void deleteChat(cid).catch((e) => setActErr(fail(e)));

  // The rail: on a pane's own page it moves between those pages; beside the
  // chat it docks the pane, and a second press puts it away; from Customize or
  // Settings it goes back to the chat with the pane docked.
  function pickPane(kind: PaneKind) {
    if (max) void navigate({ to: paneTo[kind], params: { botId: id } });
    else if (chatSide) setDocked(docked.kind === kind ? "" : kind);
    else dockPane(kind);
  }
  function dockPane(kind: PaneKind | "") {
    if (kind) setDocked(kind);
    void navigate(chatLink(id, lastChat.current));
  }

  return (
    <BotPageCtx.Provider
      value={{
        id,
        bot,
        admin,
        tab,
        chatId,
        agentId,
        start,
        stop,
        onError: setActErr,
        reloadApprovals,
        inspectArtifact: setInspect,
        saveSkill: onSaveSkill,
        needAuth: setAuthPrompt,
        secrets,
        chats,
        models,
        defaultModel,
        voice,
        rename,
        newChat: onNewChat,
        deleteChat: onDeleteChat,
        run,
        subs,
        draft,
        actions: runActions({ id, chatId, draft, run, onError: setActErr, openChat: (cid) => void navigate(chatLink(id, cid)) }),
        waiting,
        waitingRuns,
        agentsBusy,
        agentHref,
      }}
    >
      <div className="flex h-full min-h-0 flex-col">
        {actErr && (
          <div role="alert" className="rise flex items-center gap-2 bg-vermilion-pale px-4 py-2 text-[13px] text-vermilion">
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
            <span className="min-w-0 flex-1">{actErr}</span>
            <button type="button" title="Dismiss" className="rounded-xs p-1 hover:bg-surface/60" onClick={() => setActErr("")}>
              <X size={13} />
            </button>
          </div>
        )}
        <BotStrip
          id={id}
          bot={bot}
          tab={tab}
          chatId={chatId}
          chats={chats}
          rename={rename}
          busy={{ waiting, sending: run.sending }}
          onNewChat={onNewChat}
          onDeleteChat={onDeleteChat}
          onStart={start}
          onStop={stop}
        />
        <div className="relative flex min-h-0 flex-1" style={{ "--wash-left": max ? "0px" : "248px" } as CSSProperties}>
          {max ? null : (
            <BotSidebar
              id={id}
              bot={bot}
              tab={tab}
              chatId={chatId}
              chats={chats}
              rename={rename}
              live={{ waiting, sending: run.sending, agentsBusy }}
              onNewChat={onNewChat}
              onDeleteChat={onDeleteChat}
            />
          )}
          <div className={`min-h-0 min-w-0 flex-1 ${max ? "hidden" : "flex"}`}>
            <Outlet />
          </div>
          <SidePane
            key={id}
            bot={bot}
            kind={pane}
            max={max}
            share={docked.share}
            openAt={openAt}
            canDock={wide}
            onStart={start}
            onMax={() => pane && void navigate({ to: paneTo[pane], params: { botId: id } })}
            onDock={() => dockPane(pane)}
            onClose={() => setDocked("")}
          />
          <MachineRail bot={bot} open={pane} onPick={pickPane} onStart={start} onStop={stop} />
          <BotSlip bot={bot} pending={pending} authPrompt={authPrompt} setAuthPrompt={setAuthPrompt} onError={setActErr} />
          {inspect ? (
            <Suspense fallback={null}>
              <ArtifactOverlay botId={id} artifact={inspect} onSave={onSaveSkill} onClose={() => setInspect(null)} />
            </Suspense>
          ) : null}
        </div>
      </div>
    </BotPageCtx.Provider>
  );
}
