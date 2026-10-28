import type { Said } from "../composer/Composer";
import { Link, Outlet, useLocation, useMatches, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { X } from "lucide-react";
import { Suspense, useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { ui } from "../api";
import type { Artifact } from "../Artifact";
import { fail } from "../errors";
import { SkeletonRows } from "../Field";
import { UI } from "../gen/silo/v1/ui_pb";
import { lazyNamed } from "../lazyNamed";
import { agentLink, chatLink, newChatLink } from "../links";
import { reload, setBot } from "../query";
import { useRunStream } from "../useRunStream";
import { useSubagents } from "../useSubagents";
import { useWide } from "../useWide";
import { BotPageCtx } from "./context";
import { BotSidebar } from "./BotSidebar";
import { BotSlip } from "./BotSlip";
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
  changes: "/bots/$botId/changes",
  mail: "/bots/$botId/mail",
} as const;

// BotPage is the /bots/$botId layout: the sidebar on the left, the open page
// (bot/routes.tsx) in its <Outlet>, and the machine on the right — the rail,
// and the pane it docks beside the page. It owns what must survive moving
// between pages (the live Bot row, the chat list, a half-written message, the
// run stream) and hands it to those pages through useBotPage().
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
  const { chats, models, defaultModel, voice, rename, newChat, deleteChat } = useChatList(id, { tab, chatSide, chatId, fresh }, setActErr);
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
    void navigate(lastChat.current === "" ? newChatLink(id) : chatLink(id, lastChat.current));
  }

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
        waiting,
        waitingRuns,
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
              attention={pending.length ? "needs" : bot.dashboardAsks + bot.feedUnread + bot.auditUnseen ? "new" : ""}
              open={drawer}
              onOpen={() => setDrawer(true)}
              onNewChat={onNewChat}
            />
            <BotDrawer open={drawer} botId={id} label={bot.name} onClose={closeDrawer}>
              <BotSidebar
                id={id}
                bot={bot}
                tab={tab}
                chatId={chatId}
                chats={chats}
                rename={rename}
                pendingCount={pending.length}
                live={{ waiting, sending: run.sending, agentsBusy }}
                onNewChat={onNewChat}
                onDeleteChat={onDeleteChat}
                drawer
                machine={<MachineTiles bot={bot} tab={tab} onStart={start} onStop={stop} />}
              />
            </BotDrawer>
          </>
        )}
        {actErr && (
          <div role="alert" className="rise flex items-center gap-2 bg-vermilion-pale px-4 py-2 text-[13px] text-vermilion">
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
            <span className="min-w-0 flex-1">{actErr}</span>
            <button type="button" title={t("Dismiss")} className="rounded-xs p-1 hover:bg-surface/60" onClick={() => setActErr("")}>
              <X size={13} />
            </button>
          </div>
        )}
        <div className="relative flex min-h-0 flex-1" style={{ "--wash-left": max || !wide ? "0px" : "248px" } as CSSProperties}>
          {max || !wide ? null : (
            <BotSidebar
              id={id}
              bot={bot}
              tab={tab}
              chatId={chatId}
              chats={chats}
              rename={rename}
              pendingCount={pending.length}
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
            onError={setActErr}
          />
          <MachineRail bot={bot} open={pane} onPick={pickPane} onStart={start} onStop={stop} />
          <BotSlip bot={bot} pending={tab === "dashboard" ? [] : pending} onError={setActErr} />
          {inspect ? (
            <Suspense fallback={null}>
              <ArtifactOverlay botId={id} artifact={inspect} onSave={onSaveSkill} onClose={() => setInspect(null)} />
            </Suspense>
          ) : null}
        </div>
      </div>
    </BotPageCtx.Provider>
    </QuoteProvider>
  );
}
