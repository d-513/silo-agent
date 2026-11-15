import type { Said } from "../composer/Composer";
import { Link, Outlet, useLocation, useMatches, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { X } from "lucide-react";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { ui } from "../api";
import type { Artifact } from "../Artifact";
import { fail } from "../errors";
import { UI } from "../gen/silo/v1/ui_pb";
import { lazyNamed } from "../lazyNamed";
import { agentLink, chatLink, newChatLink } from "../links";
import { CallBar } from "../call/CallBar";
import { CallStage } from "../call/CallStage";
import { callIsHere, callPlace } from "../call/place";
import { hush, setOnAir, useRinging } from "../call/ring";
import { useCall } from "../call/useCall";
import { reload, setBot } from "../query";
import { useRunStream } from "../useRunStream";
import { useSubagents } from "../useSubagents";
import { useWide } from "../useWide";
import { BotPageCtx } from "./context";
import { BotSidebar } from "./BotSidebar";
import { BotSlip } from "./BotSlip";
import { ChatTabs } from "./ChatTabs";
import { BotDrawer, BotTopBar, MachineTiles } from "./BotDrawer";
import { MachineRail } from "./MachineRail";
import { setDocked, useDocked } from "./paneStore";
import { runActions, type ChatPick } from "./runActions";
import { SidePane } from "./SidePane";
import { onChatSide, paneOf, type PaneKind } from "./tabs";
import { useBotLive } from "./useBotLive";
import { patchChats, useChatList } from "./useChatList";
import { useComposerDraft } from "./useComposerDraft";
import { QuoteProvider, type QuoteSink } from "../quote/QuoteContext";
import { useSecrets } from "./useSecrets";
import { t } from "../i18n.ts";

const ArtifactOverlay = lazyNamed(() => import("../ArtifactOverlay"), "ArtifactOverlay");

function LoadingBot() {
  return (
    <div className="flex h-full max-wide:flex-col">
      <div className="flex h-[calc(3rem+env(safe-area-inset-top))] shrink-0 items-center gap-3 bg-well px-3 pt-[env(safe-area-inset-top)] shadow-[inset_0_-1px_0_var(--color-line)] wide:hidden">
        <div className="skeleton h-7 w-7 rounded-sm" />
        <div className="skeleton h-4 w-32 rounded-xs" />
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="hidden h-11 shrink-0 items-center gap-2 bg-well px-3 shadow-[inset_0_-1px_0_var(--color-line)] wide:flex">
          <div className="skeleton h-5 w-24 rounded-xs" />
          <div className="skeleton h-5 w-24 rounded-xs" />
        </div>
        <div className="flex-1" />
      </div>
      <div className="hidden w-12 shrink-0 bg-well wide:block" />
    </div>
  );
}

// Where each machine pane lives as a page of its own.
const paneTo = {
  files: "/bots/$botId/files",
  desktop: "/bots/$botId/desktop",
  console: "/bots/$botId/console",
  changes: "/bots/$botId/changes",
  mail: "/bots/$botId/mail",
} as const;

// BotPage is the /bots/$botId layout: the conversations' tab bar across the
// top, the open page (bot/routes.tsx) in its <Outlet>, and on the right the
// rail (the Bot's other pages, then its machine) and the pane it docks beside
// the page. Below 960 a top bar and its drawer stand in for the tab bar and
// the rail. It owns what must survive moving between pages (the live Bot row,
// the chat list, a half-written message, the run stream, the call) and hands
// it to those pages through useBotPage().
export function BotPage() {
  const { botId: id } = useParams({ from: "/_authed/bots/$botId" });
  const { chatId, agentId } = useParams({ strict: false });
  // The tab is whatever the matched route says it lights (router.tsx).
  const tab = useMatches({ select: (ms) => ms.map((m) => m.staticData.tab).filter(Boolean).pop() }) ?? "run";
  const fresh = useMatches({ select: (ms) => ms.some((m) => m.staticData.fresh) });
  const chatSide = onChatSide(tab);
  const navigate = useNavigate();
  const [actErr, setActErr] = useState("");
  const [said, setSaid] = useState<Said | null>(null);
  useEffect(() => setSaid(null), [id, chatId]);
  const { bot, loadErr, pending, reloadApprovals } = useBotLive(id, chatId);
  const { chats, models, defaultModel, voice, talk, rename, newChat, deleteChat, hideChat } = useChatList(id, { tab, chatSide, chatId, fresh }, setActErr);
  // A call survives moving between the Bot's pages; it is held in one chat.
  const call = useCall(id);
  const startCall = useCallback(() => call.start(chatId ?? (fresh ? "new" : "")), [call, chatId, fresh]);
  // The big Call button rings the Bot in your main chat, on the call screen.
  // While a call is up it goes back to it instead.
  const mainId = chats.find((c) => c.main && c.mine)?.id;
  const callMain = useCallback(() => {
    if (call.live && call.snap) {
      call.setExpanded(true);
      if (call.snap.chatId && call.snap.chatId !== "new") void navigate(chatLink(id, call.snap.chatId));
      return;
    }
    if (!mainId) return;
    call.start(mainId, true);
    void navigate(chatLink(id, mainId));
  }, [call, mainId, id, navigate]);
  // Call on this Bot's row in the sidebar rings here: the page answers once
  // it knows the main chat. A ring for another Bot is that Bot's page's.
  const ringing = useRinging();
  useEffect(() => {
    if (ringing !== id || (!mainId && !call.live)) return;
    hush(id);
    callMain();
  }, [ringing, id, mainId, call.live, callMain]);
  // The row shows this page's call: its clock, and the way back to it.
  const onAir = call.live && call.snap ? call.snap : null;
  const airConnecting = onAir?.state === "connecting";
  const airSince = onAir?.startedAt ?? 0;
  useEffect(() => {
    setOnAir(onAir ? { botId: id, connecting: airConnecting, since: airSince } : null);
    return () => setOnAir(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, !!onAir, airConnecting, airSince]);
  // A call from the empty chat makes its own: go there once it has an id,
  // if the empty chat is still the page. Either way that call is settled, so
  // a later New chat never jumps to it.
  const callChat = call.snap?.chatId ?? "";
  const callFromNew = useRef(false);
  useEffect(() => {
    if (callChat === "new") {
      callFromNew.current = true;
      return;
    }
    if (!callFromNew.current) return;
    if (!callChat) {
      // Ended (and gone) before it had an id.
      callFromNew.current = false;
      return;
    }
    callFromNew.current = false;
    if (fresh) void navigate(chatLink(id, callChat));
  }, [callChat, fresh, id, navigate]);
  const draft = useComposerDraft(id, bot?.workerConnected);
  // Each visit to the empty chat starts from the defaults.
  const [pick, setPick] = useState<ChatPick>({ model: "", thinking: "" });
  useEffect(() => setPick({ model: "", thinking: "" }), [id, fresh]);
  const secrets = useSecrets(id, tab === "secrets", setActErr);
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
  // The narrow layout's drawer; any move to another page puts it away.
  const [drawer, setDrawer] = useState(false);
  const closeDrawer = useCallback(() => setDrawer(false), []);
  const { pathname } = useLocation();
  useEffect(() => setDrawer(false), [pathname]);
  useEffect(() => {
    if (wide) setDrawer(false);
  }, [wide]);
  const subs = useSubagents(id, chatId);
  const run = useRunStream(id, chatId, {
    onApproval: reloadApprovals,
    onTitle: (title) => patchChats(id, (xs) => xs.map((c) => (c.id === chatId ? { ...c, title } : c))),
    onDone: () => {
      void reload(UI.method.listChats, { botId: id });
      void reload(UI.method.listArtifacts, { botId: id });
      subs.refresh();
    },
    onPing: () => subs.refresh(),
  });

  useEffect(() => {
    lastChat.current = undefined;
  }, [id]);
  // Docking a pane back goes to the chat it was opened from.
  useEffect(() => {
    if (chatId) lastChat.current = chatId;
    else if (fresh) lastChat.current = "";
  }, [chatId, fresh]);
  // Files keeps the folder a link opened it in after it is docked.
  useEffect(() => {
    if (open) setOpenAt(open);
  }, [open]);

  if (loadErr) {
    return (
      <div className="p-7">
        <p className="mb-3 text-vermilion">{loadErr}</p>
        <Link to="/" className="text-cobalt">
          {t("Back to Bots")}
        </Link>
      </div>
    );
  }
  if (!bot) return <LoadingBot />;

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

  // Save skill installs the version the card shows; the card then reads saved
  // from the event the server emits for it.
  async function saveSkill(a: Artifact) {
    const v = a.versions.find((x) => x.n === a.version);
    try {
      await ui.saveSkill({ botId: id, artifactId: a.artifactId, version: a.version, path: a.path, runId: v?.runId ?? a.runId ?? "" });
      setInspect((cur) => (cur && cur.artifactId === a.artifactId ? { ...cur, saved: a.version } : cur));
      void reload(UI.method.listArtifacts, { botId: id });
    } catch (e) {
      setActErr(fail(e));
    }
  }
  const onSaveSkill = (a: Artifact) => void saveSkill(a);
  const onNewChat = newChat;
  const onDeleteChat = (cid: string) => void deleteChat(cid).catch((e) => setActErr(fail(e)));

  // The rail's machine panes: on a pane's own page it moves between those
  // pages; beside the chat it docks the pane, and a second press puts it away;
  // from Channels or Settings it goes back to the chat with the pane docked.
  function pickPane(kind: PaneKind) {
    if (max) void navigate({ to: paneTo[kind], params: { botId: id } });
    else if (chatSide) setDocked(docked.kind === kind ? "" : kind);
    else dockPane(kind);
  }
  function dockPane(kind: PaneKind | "") {
    if (kind) setDocked(kind);
    void navigate(lastChat.current === "" ? newChatLink(id) : chatLink(id, lastChat.current));
  }

  // Off its chat's page the call is drawn here, so it never leaves the screen.
  const place = callPlace({
    call: !!call.snap,
    here: tab === "run" && !agentId && callIsHere(callChat, chatId, fresh),
    expanded: call.expanded,
    wide,
    docked: !!pane && !max,
  });
  const callRow = chats.find((c) => c.id === callChat);

  const quotes: QuoteSink = { add: draft.quote, upload: draft.upload, canUpload: !!bot.workerConnected };

  return (
    <QuoteProvider value={quotes}>
    <BotPageCtx.Provider
      value={{
        id,
        bot,
        tab,
        chatId,
        fresh,
        pick,
        agentId,
        start,
        stop,
        onError: setActErr,
        reloadApprovals,
        inspectArtifact: setInspect,
        saveSkill: onSaveSkill,
        secrets,
        chats,
        models,
        defaultModel,
        voice,
        talk,
        call,
        startCall,
        rename,
        newChat: onNewChat,
        deleteChat: onDeleteChat,
        run,
        subs,
        draft,
        actions: runActions({
          id,
          chatId,
          fresh,
          pick,
          setPick,
          draft,
          run,
          onError: setActErr,
          openChat: (cid) => void navigate(chatLink(id, cid)),
          openNew: () => void navigate(newChatLink(id)),
          onSaid: setSaid,
          showCommands: () => draft.setText("/"),
        }),
        said,
        setSaid,
        agentsBusy,
        agentHref,
      }}
    >
      <div className="flex h-full min-h-0 flex-col">
        {wide ? null : (
          <>
            <BotTopBar
              bot={bot}
              tab={tab}
              chat={chats.find((c) => c.id === chatId)}
              fresh={fresh}
              attention={pending.length + bot.dashboardUrgent ? "needs" : bot.dashboardAsks + bot.feedUnread + bot.auditUnseen ? "new" : ""}
              open={drawer}
              onOpen={() => setDrawer(true)}
              onNewChat={onNewChat}
              call={talk ? call : undefined}
              onCall={callMain}
            />
            <BotDrawer open={drawer} botId={id} label={bot.name} onClose={closeDrawer}>
              <BotSidebar
                id={id}
                bot={bot}
                tab={tab}
                chatId={chatId}
                chats={chats}
                rename={rename}
                pendingCount={pending.length + bot.dashboardUrgent}
                sending={run.sending}
                onNewChat={onNewChat}
                onDeleteChat={onDeleteChat}
                call={talk ? call : undefined}
                onCall={callMain}
                machine={<MachineTiles bot={bot} tab={tab} onStart={start} onStop={stop} />}
              />
            </BotDrawer>
          </>
        )}
        {actErr && (
          <div role="alert" className="rise flex items-center gap-2 bg-vermilion-pale px-4 py-2 text-small text-vermilion">
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
            <span className="min-w-0 flex-1">{actErr}</span>
            <button type="button" title={t("Dismiss")} className="rounded-xs p-1 hover:bg-surface/60" onClick={() => setActErr("")}>
              <X size={13} />
            </button>
          </div>
        )}
        <div className="relative flex min-h-0 flex-1">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            {wide ? (
              <ChatTabs
                id={id}
                bot={bot}
                tab={tab}
                chatId={chatId}
                fresh={fresh}
                chats={chats}
                rename={rename}
                pendingCount={pending.length + bot.dashboardUrgent}
                sending={run.sending}
                callChat={call.live ? callChat : ""}
                onNewChat={onNewChat}
                onDeleteChat={onDeleteChat}
                onHideChat={(cid, hidden) => void hideChat(cid, hidden)}
              />
            ) : null}
            <div className="flex min-h-0 flex-1">
              <div className={`min-h-0 min-w-0 flex-1 ${max ? "hidden" : "flex"}`}>
                <Outlet />
              </div>
              {place === "aside" ? (
                <aside className="flex min-h-0 w-[340px] shrink-0 flex-col bg-canvas shadow-[inset_1px_0_0_var(--color-line)]">
                  <CallStage
                    call={call}
                    where={callRow?.main ? t("Main chat") : callRow?.title || t("New chat")}
                    bot={bot}
                    narrow
                    onMinimize={() => {
                      call.setExpanded(false);
                      if (callChat !== "new") void navigate(chatLink(id, callChat));
                    }}
                  />
                </aside>
              ) : null}
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
                onError={setActErr}
              />
            </div>
          </div>
          <MachineRail bot={bot} tab={tab} open={pane} onPick={pickPane} onStart={start} onStop={stop} />
          <BotSlip bot={bot} pending={tab === "dashboard" ? [] : pending} />
          {inspect ? (
            <Suspense fallback={null}>
              <ArtifactOverlay botId={id} artifact={inspect} onSave={onSaveSkill} onClose={() => setInspect(null)} />
            </Suspense>
          ) : null}
        </div>
        {place === "bar" ? (
          <div className="shrink-0 bg-well pt-2 pb-[env(safe-area-inset-bottom)] shadow-[inset_0_1px_0_var(--color-line)]">
            <CallBar call={call} bot={bot} onExpand={callMain} />
          </div>
        ) : null}
      </div>
    </BotPageCtx.Provider>
    </QuoteProvider>
  );
}
