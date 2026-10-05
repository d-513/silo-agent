import { X } from "lucide-react";
import { Suspense, useEffect, useState, type CSSProperties, type ReactNode } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { ui } from "../api";
import type { Artifact } from "../Artifact";
import { useAuth } from "../auth";
import { useBots } from "../bots";
import { fail } from "../errors";
import { SkeletonRows } from "../Field";
import { lazyNamed } from "../lazyNamed";
import { PaneFallback } from "../PaneFallback";
import type { BotConnector } from "../gen/silo/v1/ui_pb";
import { useRunStream } from "../useRunStream";
import { useSubagents } from "../useSubagents";
import { BotHeader } from "./BotHeader";
import { BotSlip } from "./BotSlip";
import { ChatSidebar } from "./ChatSidebar";
import { ChatStrip } from "./ChatStrip";
import { runActions } from "./runActions";
import { RunPane } from "./RunPane";
import { SecretsPane } from "./SecretsPane";
import { isNavTab, isSideTab, onChatSide, type Tab } from "./tabs";
import { useBotLive } from "./useBotLive";
import { useChatList } from "./useChatList";
import { useComposerDraft } from "./useComposerDraft";
import { useSecrets } from "./useSecrets";

// Everything but the chat is loaded the first time its tab opens.
const AutomationsPane = lazyNamed(() => import("../Automations"), "AutomationsPane");
const MemoriesPane = lazyNamed(() => import("../Memories"), "MemoriesPane");
const KnowledgePane = lazyNamed(() => import("../Knowledge"), "KnowledgePane");
const FeedPane = lazyNamed(() => import("../Feed"), "FeedPane");
const SubagentPage = lazyNamed(() => import("../SubagentPage"), "SubagentPage");
const MachinePane = lazyNamed(() => import("./MachinePane"), "MachinePane");
const FilesPane = lazyNamed(() => import("../Files"), "FilesPane");
const BotConnectors = lazyNamed(() => import("../connectors/BotConnectors"), "BotConnectors");
const BotDrives = lazyNamed(() => import("../drives/BotDrives"), "BotDrives");
const BotChannels = lazyNamed(() => import("../channels/BotChannels"), "BotChannels");
const TunnelsPane = lazyNamed(() => import("./Tunnels"), "TunnelsPane");
const BotSkills = lazyNamed(() => import("../Skills"), "BotSkills");
const ContainersPane = lazyNamed(() => import("./Containers"), "ContainersPane");
const SettingsPane = lazyNamed(() => import("../Settings"), "SettingsPane");
const RulesPane = lazyNamed(() => import("../rules/RulesPane"), "RulesPane");
const ArtifactOverlay = lazyNamed(() => import("../ArtifactOverlay"), "ArtifactOverlay");

// A tab page that scrolls inside the Bot page's body.
function ScrollPane({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-auto">
      <Suspense fallback={<PaneFallback />}>{children}</Suspense>
    </div>
  );
}

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

// BotPage is /bots/:id/*: the Bot's header and tabs, and the page for the open
// tab. It owns what must survive moving between tabs (the live Bot row, the
// chat list, a half-written message, the run stream) and hands the rest to the
// pieces in this folder.
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
  const [actErr, setActErr] = useState("");
  const { bot, setBot, loadErr, pending, setPending, reloadApprovals } = useBotLive(id, chatId);
  const { chats, setChats, models, defaultModel, voice, rename, newChat, deleteChat } = useChatList(id, { tab, chatSide, chatId }, setActErr);
  const draft = useComposerDraft(id ?? "", bot?.workerConnected);
  const secrets = useSecrets(id, tab === "secrets", setActErr);
  const [authPrompt, setAuthPrompt] = useState<BotConnector | null>(null);
  const [keepDesk, setKeepDesk] = useState(tab === "desktop");
  const [keepCon, setKeepCon] = useState(tab === "console");
  const [inspect, setInspect] = useState<Artifact | null>(null);
  const subs = useSubagents(id, chatId);
  const run = useRunStream(id, chatId, {
    onApproval: reloadApprovals,
    onTitle: (title) => setChats((xs) => xs.map((c) => (c.id === chatId ? { ...c, title } : c))),
    onDone: () => {
      if (id) ui.listChats({ botId: id }).then((r) => setChats(r.chats)).catch(() => {});
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
  if (!bot) return <LoadingBot />;

  const chatRuns = new Set(run.events.map((e) => e.runId).filter(Boolean));
  const agentRuns = new Set(subs.agents.map((a) => a.runId).filter(Boolean));
  const waitingRuns = new Set(pending.map((p) => p.runId).filter((r) => r && agentRuns.has(r)));
  // A subagent's approval slip is this chat's too: the human is the same.
  const waiting = pending.some((p) => p.runId && (chatRuns.has(p.runId) || agentRuns.has(p.runId)));
  const agentsBusy = subs.agents.some((a) => a.running);

  function agentHref(name: string): string | undefined {
    const a = subs.agents.find((x) => x.name.toLowerCase() === name.toLowerCase());
    return a && id && chatId ? `/bots/${id}/run/${chatId}/agent/${a.id}` : undefined;
  }

  const actions = runActions({ id, chatId, draft, run, setBot, setChats, setPending, onError: setActErr, nav });

  async function start() {
    setActErr("");
    try {
      setBot(await ui.startBot({ id: id! }));
      refresh();
    } catch (e) {
      setActErr(fail(e));
    }
  }
  async function stop() {
    setActErr("");
    try {
      setBot(await ui.stopBot({ id: id! }));
      refresh();
    } catch (e) {
      setActErr(fail(e));
    }
  }

  async function saveSkill(a: Artifact) {
    try {
      await ui.saveSkill({ botId: id!, path: a.path, runId: a.runId ?? "" });
      setInspect((cur) => (cur && cur.path === a.path ? { ...cur, status: "saved", scope: "personal" } : cur));
    } catch (e) {
      setActErr(fail(e));
    }
  }
  const onSaveSkill = (a: Artifact) => void saveSkill(a);
  const onNewChat = () => newChat().catch((e) => setActErr(fail(e)));
  const onDeleteChat = (cid: string) => deleteChat(cid).catch((e) => setActErr(fail(e)));
  const savedBot = (next: typeof bot) => {
    setBot(next);
    refresh();
  };

  return (
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
        {chatSide && (
          <>
            <ChatSidebar
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
            <section className="flex min-w-0 flex-1 flex-col">
              <ChatStrip
                id={id}
                bot={bot}
                tab={tab}
                chatId={chatId}
                chats={chats}
                rename={rename}
                busy={{ waiting, sending: run.sending }}
                onNewChat={onNewChat}
                onDeleteChat={onDeleteChat}
              />
              {tab === "automations" && (
                <Suspense fallback={<PaneFallback />}>
                  <AutomationsPane
                    bot={bot}
                    sub={parts.slice(1)}
                    onError={setActErr}
                    onApprovals={reloadApprovals}
                    onInspectArtifact={setInspect}
                    onSaveSkill={onSaveSkill}
                  />
                </Suspense>
              )}
              {tab === "memories" && (
                <ScrollPane>
                  <MemoriesPane bot={bot} onSaved={setBot} onError={setActErr} />
                </ScrollPane>
              )}
              {tab === "knowledge" && (
                <ScrollPane>
                  <KnowledgePane bot={bot} onError={setActErr} onStart={start} />
                </ScrollPane>
              )}
              {tab === "feed" && (
                <ScrollPane>
                  <FeedPane
                    bot={bot}
                    onError={setActErr}
                    onQuoted={(c) => {
                      setChats((xs) => [c, ...xs.filter((x) => x.id !== c.id)]);
                      nav(`/bots/${id}/run/${c.id}`);
                    }}
                  />
                </ScrollPane>
              )}
              {tab === "run" && agentId && chatId && (
                <Suspense fallback={<PaneFallback />}>
                  <SubagentPage
                    key={agentId}
                    botId={id}
                    botName={bot.name}
                    botCrest={bot.crest}
                    chatId={chatId}
                    agentId={agentId}
                    onError={setActErr}
                    onApprovals={reloadApprovals}
                    onInspectArtifact={setInspect}
                    onSaveSkill={onSaveSkill}
                  />
                </Suspense>
              )}
              {tab === "run" && !agentId && (
                <RunPane
                  id={id}
                  bot={bot}
                  chatId={chatId}
                  chats={chats}
                  models={models}
                  defaultModel={defaultModel}
                  voice={voice}
                  run={run}
                  subs={subs}
                  draft={draft}
                  actions={actions}
                  waitingRuns={waitingRuns}
                  agentHref={agentHref}
                  onInspectArtifact={setInspect}
                  onSaveSkill={onSaveSkill}
                  onError={setActErr}
                />
              )}
            </section>
          </>
        )}
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
        {tab === "files" && (
          <section className="flex min-h-0 min-w-0 flex-1 flex-col">
            <Suspense fallback={<PaneFallback />}>
              <FilesPane bot={bot} onStart={start} />
            </Suspense>
          </section>
        )}
        {tab === "connectors" && (
          <section className="min-h-0 min-w-0 flex-1 overflow-auto">
            <Suspense fallback={<PaneFallback />}>
              <BotConnectors botId={id} onNeedAuth={setAuthPrompt} />
            </Suspense>
          </section>
        )}
        {tab === "drives" && (
          <ScrollPane>
            <BotDrives botId={id} sub={parts.slice(1)} admin={admin} />
          </ScrollPane>
        )}
        {tab === "channels" && (
          <ScrollPane>
            <BotChannels botId={id} sub={parts.slice(1)} />
          </ScrollPane>
        )}
        {tab === "tunnels" && (
          <ScrollPane>
            <TunnelsPane botId={id} />
          </ScrollPane>
        )}
        {tab === "skills" && (
          <ScrollPane>
            <BotSkills botId={id} />
          </ScrollPane>
        )}
        {tab === "secrets" && (
          <ScrollPane>
            <SecretsPane botId={id} state={secrets} onError={setActErr} />
          </ScrollPane>
        )}
        {tab === "container" && (
          <ScrollPane>
            <ContainersPane bot={bot} onStart={start} onStop={stop} onChanged={savedBot} />
          </ScrollPane>
        )}
        {tab === "settings" && (
          <ScrollPane>
            <SettingsPane bot={bot} onSaved={savedBot} onError={setActErr} onRefresh={refresh} />
          </ScrollPane>
        )}
        {tab === "rules" && (
          <ScrollPane>
            <RulesPane botId={id} />
          </ScrollPane>
        )}
        <BotSlip bot={bot} pending={pending} setPending={setPending} authPrompt={authPrompt} setAuthPrompt={setAuthPrompt} onError={setActErr} />
        {inspect ? (
          <Suspense fallback={null}>
            <ArtifactOverlay botId={id} artifact={inspect} onSave={onSaveSkill} onClose={() => setInspect(null)} />
          </Suspense>
        ) : null}
      </div>
    </div>
  );
}
