import type { Said } from "../composer/Composer";
import { Link, Outlet, useLocation, useMatches, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { X } from "lucide-react";
import { Suspense, useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";
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
import { BotSlip } from "./BotSlip";
import { ChatTabs } from "./ChatTabs";
import { BotTopBar, RailSheet } from "./BotDrawer";
import { Sheet } from "../nav/Sheet";
import { MachineRail } from "./MachineRail";
import { available, barKeys, chatOf, isChat, isPageKey, isPane, moveBefore, neighbour, pinnable, railKeys, routeKey, splitAfterMove, splittable, type Avail, type TabKey } from "./layout";
import { closeTab, openTab, pin, setOrder, setSplit, unpin, useLayout } from "./layoutStore";
import { runActions, type ChatPick } from "./runActions";
import { SplitPage } from "./routes";
import { Panes } from "./SidePane";
import type { TabControl } from "./tabControl";
import { keyLink } from "./tabMeta";
import { dropped, carriesTab, TabMenuHost, useDragging } from "./TabMenu";
import { chatTabs } from "./tabOrder";
import { onChatSide } from "./tabs";
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
      <div className="flex h-[calc(3rem+env(safe-area-inset-top))] shrink-0 items-center gap-3 bg-well pt-[env(safe-area-inset-top)] pr-3 pl-12 wide:hidden">
        <div className="skeleton h-8 w-8 rounded-sm" />
        <div className="skeleton h-4 w-32 rounded-xs" />
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-11 shrink-0 items-center gap-2 bg-well px-3 shadow-[inset_0_-1px_0_var(--color-line)] max-wide:h-10">
          <div className="skeleton h-5 w-24 rounded-xs" />
          <div className="skeleton h-5 w-24 rounded-xs" />
        </div>
        <div className="flex-1" />
      </div>
      <div className="hidden w-12 shrink-0 bg-well wide:block" />
    </div>
  );
}

// DropZones is where a dragged tab may land on the page: its left part opens
// it as the page, its right part shows it in the split. It stands over the
// page only while a tab is dragged, over the desktop's canvas too.
function DropZones({ onMain, onSide }: { onMain: (k: TabKey) => void; onSide: (k: TabKey) => void }) {
  const [hot, setHot] = useState<"main" | "side" | "">("");
  const zone = (which: "main" | "side", label: string, take: (k: TabKey) => void, className: string) => (
    <div
      className={`relative flex items-center justify-center p-3 ${className}`}
      onDragOver={(e: DragEvent) => {
        if (!carriesTab(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        if (hot !== which) setHot(which);
      }}
      onDragLeave={() => setHot((h) => (h === which ? "" : h))}
      onDrop={(e: DragEvent) => {
        e.preventDefault();
        setHot("");
        const k = dropped(e);
        if (k) take(k);
      }}
    >
      <span
        className={`pointer-events-none absolute inset-2 flex items-center justify-center rounded-card text-small font-medium transition-[background-color,box-shadow,color] duration-[160ms] ease-quiet ${
          hot === which ? "bg-cobalt-pale/95 text-cobalt shadow-[inset_0_0_0_2px_var(--color-cobalt)] backdrop-blur-sm" : "bg-canvas/40 text-transparent shadow-[inset_0_0_0_1px_var(--color-line-strong)]"
        }`}
      >
        {label}
      </span>
    </div>
  );
  return (
    <div className="absolute inset-0 z-30 flex">
      {zone("main", t("Open here"), onMain, "flex-1")}
      {zone("side", t("Open in split view"), onSide, "w-[38%] min-w-[240px]")}
    </div>
  );
}

// BotPage is the /bots/$botId layout: the tab bar across the top (every page
// of the Bot can be a tab: bot/layout.ts), the open page (bot/routes.tsx) in
// its <Outlet>, the split beside it when a tab is shown there, and on the
// right the rail (every page that is not pinned to the bar). Below 960 a top
// bar stands over the tab bar, the rail is a sheet from the right and there
// is no split. It owns what must survive moving between pages (the live Bot
// row, the chat list, a half-written message, the run stream, the call, the
// machine panes) and hands it to those pages through useBotPage().
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
  const layout = useLayout(id);
  const { chats, models, defaultModel, voice, talk, rename, newChat, deleteChat, hideChat } = useChatList(id, { tab, chatSide: chatSide || isChat(layout.split), chatId, fresh }, setActErr);
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
  // The tabs: what the page is, what is pinned, open and in the split
  // (layout.ts), and what waits in the rail. The split needs room, so it is
  // the wide layout's alone.
  const wide = useWide();
  const current = routeKey(tab, chatId, fresh, mainId);
  const avail: Avail = useMemo(
    () => ({ main: !!mainId, mail: !!bot?.mailAddress || current === "mail", runs: bot?.role === "manage", chats: new Set(chats.map((c) => c.id)) }),
    [mainId, bot?.mailAddress, bot?.role, current, chats],
  );
  const openChats = useMemo(() => chatTabs(chats, tab === "run" ? chatId : undefined).map((c) => c.id), [chats, tab, chatId]);
  const keys = useMemo(() => barKeys({ pinned: layout.pinned, order: layout.order, openChats, current, avail }), [layout.pinned, layout.order, openChats, current, avail]);
  const railed = useMemo(() => railKeys(layout.pinned, avail), [layout.pinned, avail]);
  const split: TabKey | "" = wide && layout.split && layout.split !== current && splittable(layout.split) && available(layout.split, avail) ? layout.split : "";
  const mainPane = isPane(current) ? current : "";
  const sidePane = isPane(split) ? split : "";
  const dragging = useDragging();
  // A page opened by any way (the rail, a link, the address bar) is a tab
  // until it is closed.
  useEffect(() => {
    if (current && isPageKey(current) && !layout.pinned.includes(current)) openTab(id, current);
  }, [id, current, layout.pinned]);
  // Opening the tab shown in the split swaps the two; a tab just closed is
  // not put there.
  const prevKey = useRef<TabKey | "">("");
  const closing = useRef<TabKey | "">("");
  useEffect(() => {
    prevKey.current = "";
  }, [id]);
  useEffect(() => {
    const from = closing.current === prevKey.current ? "" : prevKey.current;
    closing.current = "";
    const next = splitAfterMove(layout.split, from, current);
    if (next !== layout.split) setSplit(id, next);
    prevKey.current = current;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, current]);
  const { open } = useSearch({ strict: false });
  const [openAt, setOpenAt] = useState(open ?? "");
  const [inspect, setInspect] = useState<Artifact | null>(null);
  // The narrow layout's rail, a sheet from the right; any move to another
  // page puts it away.
  const [rail, setRail] = useState(false);
  const closeRail = useCallback(() => setRail(false), []);
  const openRail = useCallback(() => setRail(true), []);
  const { pathname } = useLocation();
  useEffect(() => setRail(false), [pathname]);
  useEffect(() => {
    if (wide) setRail(false);
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

  // What every place a tab stands may do with it (tabControl.ts).
  const go = (k: TabKey) => void navigate(keyLink(k, id, mainId));
  const chatRow = (k: TabKey) => chats.find((c) => c.id === chatOf(k));
  const unhide = (k: TabKey) => {
    const c = chatRow(k);
    if (c?.mine && c.hidden) void hideChat(c.id, false);
  };
  const ctl: TabControl = {
    botId: id,
    mainId,
    chats,
    current,
    split,
    pinned: layout.pinned,
    wide,
    open: (k) => {
      if (isPageKey(k) && !layout.pinned.includes(k)) openTab(id, k);
      unhide(k);
      go(k);
    },
    close: (k) => {
      const c = chatRow(k);
      if (c?.mine && !c.hidden && !c.main) void hideChat(c.id, true);
      closeTab(id, k);
      if (k !== current) return;
      // The open tab closes onto its neighbour, else the last pinned one,
      // else the main chat.
      const next = neighbour(keys.strip, k) || keys.pinned[keys.pinned.length - 1] || (k === "main" ? "dashboard" : "main");
      closing.current = k;
      go(next);
    },
    pin: (k, before = "") => {
      if (!pinnable(k)) return;
      pin(k, before);
      unhide(k);
    },
    unpin: (k) => {
      unpin(k);
      // Unpinned while it is the page or in the split, it stays as a tab.
      if (isPageKey(k) && (k === current || k === split)) openTab(id, k);
    },
    toSplit: (k) => {
      if (!wide || !splittable(k)) return;
      unhide(k);
      if (k !== current) {
        setSplit(id, k);
        return;
      }
      // The page moves to the split; the page becomes what was in the split,
      // else the tab beside it.
      const rest = [...keys.pinned, ...keys.strip].filter((x) => x !== k && splittable(x));
      const all = [...keys.pinned, ...keys.strip];
      const near = neighbour(all, k);
      const fb = (split && split !== k ? split : "") || (near && splittable(near) ? near : "") || rest[0] || (k !== "main" && mainId ? "main" : "");
      if (!fb) return;
      setSplit(id, k);
      go(fb);
    },
    unsplit: () => setSplit(id, ""),
    drop: (k, group, before) => {
      if (group === "pinned") {
        ctl.pin(k, before);
        return;
      }
      if (layout.pinned.includes(k)) unpin(k);
      setOrder(id, moveBefore(keys.strip, k, before));
      unhide(k);
    },
    rename: rename.begin,
  };

  // Off its chat's page (and the split) the call is drawn here, so it never
  // leaves the screen.
  const splitChat = split === "main" ? mainId : chatOf(split);
  const place = callPlace({
    call: !!call.snap,
    here: (tab === "run" && !agentId && callIsHere(callChat, chatId, fresh)) || (!!splitChat && callChat === splitChat),
    expanded: call.expanded,
    wide,
    docked: !!split,
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
            <BotTopBar bot={bot} tab={tab} railOpen={rail} onRail={openRail} call={talk ? call : undefined} onCall={callMain} />
            <Sheet side="right" open={rail} edge label={bot.name} onOpen={openRail} onClose={closeRail}>
              <RailSheet bot={bot} ctl={ctl} rail={railed} onStart={start} onStop={stop} />
            </Sheet>
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
            <ChatTabs
              bot={bot}
              ctl={ctl}
              keys={keys}
              rename={rename}
              pendingCount={pending.length + bot.dashboardUrgent}
              sending={run.sending}
              callChat={call.live ? callChat : ""}
              onNewChat={onNewChat}
              onDeleteChat={onDeleteChat}
              onHideChat={(cid, hidden) => void hideChat(cid, hidden)}
            />
            <div className="relative flex min-h-0 flex-1">
              <div className={`order-1 min-h-0 min-w-0 flex-1 ${mainPane ? "hidden" : "flex"}`}>
                <Outlet />
              </div>
              <Panes
                key={id}
                bot={bot}
                main={mainPane}
                side={sidePane}
                share={layout.share}
                openAt={openAt}
                wide={wide}
                onStart={start}
                onOpen={ctl.open}
                onSplit={ctl.toSplit}
                onUnsplit={ctl.unsplit}
                onError={setActErr}
              />
              {split && !sidePane ? <SplitPage key={split} k={split} ctl={ctl} share={layout.share} /> : null}
              {place === "aside" ? (
                <aside className="order-4 flex min-h-0 w-[340px] shrink-0 flex-col bg-canvas shadow-[inset_1px_0_0_var(--color-line)]">
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
              {dragging && wide ? <DropZones onMain={ctl.open} onSide={ctl.toSplit} /> : null}
            </div>
          </div>
          <MachineRail bot={bot} ctl={ctl} rail={railed} pendingCount={pending.length + bot.dashboardUrgent} onStart={start} onStop={stop} />
          <BotSlip bot={bot} pending={tab === "dashboard" ? [] : pending} />
          <TabMenuHost />
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
