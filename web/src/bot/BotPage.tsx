import { Link, Outlet, useMatches, useNavigate, useParams } from "@tanstack/react-router";
import { X } from "lucide-react";
import { Suspense, useEffect, useState, type CSSProperties } from "react";
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
import { BotHeader } from "./BotHeader";
import { BotPageCtx } from "./context";
import { BotSlip } from "./BotSlip";
import { runActions } from "./runActions";
import { onChatSide } from "./tabs";
import { useBotLive } from "./useBotLive";
import { patchChats, useChatList } from "./useChatList";
import { useComposerDraft } from "./useComposerDraft";
import { useSecrets } from "./useSecrets";

const MachinePane = lazyNamed(() => import("./MachinePane"), "MachinePane");
const ArtifactOverlay = lazyNamed(() => import("../ArtifactOverlay"), "ArtifactOverlay");

function LoadingBot() {
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

// BotPage is the /bots/$botId layout: the Bot's header and tabs, with the open
// tab's page (bot/routes.tsx) in its <Outlet>. It owns what must survive moving
// between tabs (the live Bot row, the chat list, a half-written message, the
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
  const [keepDesk, setKeepDesk] = useState(tab === "desktop");
  const [keepCon, setKeepCon] = useState(tab === "console");
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

  // The desktop and console stay mounted (hidden) once opened, so their
  // connections survive a trip to another tab; a different Bot starts fresh.
  useEffect(() => {
    setKeepDesk(tab === "desktop");
    setKeepCon(tab === "console");
    setAuthPrompt(null);
  }, [id]);
  useEffect(() => {
    if (tab === "desktop") setKeepDesk(true);
    if (tab === "console") setKeepCon(true);
  }, [tab]);

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
        newChat: () => newChat().catch((e) => setActErr(fail(e))),
        deleteChat: (cid) => deleteChat(cid).catch((e) => setActErr(fail(e))),
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
        <BotHeader id={id} bot={bot} tab={tab} chatId={chatId} onStart={start} onStop={stop} />
        {actErr && (
          <div role="alert" className="rise flex items-center gap-2 bg-vermilion-pale px-4 py-2 text-[13px] text-vermilion">
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
            <span className="min-w-0 flex-1">{actErr}</span>
            <button type="button" title="Dismiss" className="rounded-xs p-1 hover:bg-white/60" onClick={() => setActErr("")}>
              <X size={13} />
            </button>
          </div>
        )}
        <div className="relative flex min-h-0 flex-1" style={{ "--wash-left": chatSide ? "248px" : "0px" } as CSSProperties}>
          <Outlet />
          {tab === "desktop" || keepDesk ? (
            <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "desktop" ? "flex" : "hidden"}`}>
              <Suspense fallback={null}>
                <MachinePane bot={bot} onStart={start} visible={tab === "desktop"} kind="desktop" />
              </Suspense>
              <p className="mt-2 text-[12.5px] text-ink-2">Same browser the Bot uses. You can type and click.</p>
            </section>
          ) : null}
          {tab === "console" || keepCon ? (
            <section className={`min-w-0 flex-1 flex-col p-3 ${tab === "console" ? "flex" : "hidden"}`}>
              <Suspense fallback={null}>
                <MachinePane bot={bot} onStart={start} visible={tab === "console"} kind="console" />
              </Suspense>
              <p className="mt-2 text-[12.5px] text-ink-2">A shell on this Bot, started in /workspace.</p>
            </section>
          ) : null}
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
