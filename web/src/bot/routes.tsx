import { Outlet, useMatches, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Suspense, type ReactNode } from "react";
import { lazyNamed } from "../lazyNamed";
import { chatLink } from "../links";
import { PaneFallback } from "../PaneFallback";
import { useBotPage, type SubPage } from "./context";
import { ChatSidebar } from "./ChatSidebar";
import { ChatStrip } from "./ChatStrip";
import { RunPane } from "./RunPane";
import { SecretsPane } from "./SecretsPane";
import { patchChats } from "./useChatList";

// The pages under /bots/$botId (router.tsx). Each is a thin adapter: it takes
// what its pane needs from BotPage and renders the pane. Everything but the
// chat is loaded the first time its tab opens.
const AutomationsPane = lazyNamed(() => import("../Automations"), "AutomationsPane");
const MemoriesPane = lazyNamed(() => import("../Memories"), "MemoriesPane");
const KnowledgePane = lazyNamed(() => import("../Knowledge"), "KnowledgePane");
const FeedPane = lazyNamed(() => import("../Feed"), "FeedPane");
const SubagentPage = lazyNamed(() => import("../SubagentPage"), "SubagentPage");
const FilesPane = lazyNamed(() => import("../Files"), "FilesPane");
const BotConnectors = lazyNamed(() => import("../connectors/BotConnectors"), "BotConnectors");
const BotDrives = lazyNamed(() => import("../drives/BotDrives"), "BotDrives");
const BotChannels = lazyNamed(() => import("../channels/BotChannels"), "BotChannels");
const TunnelsPane = lazyNamed(() => import("./Tunnels"), "TunnelsPane");
const BotSkills = lazyNamed(() => import("../Skills"), "BotSkills");
const ContainersPane = lazyNamed(() => import("./Containers"), "ContainersPane");
const SettingsPane = lazyNamed(() => import("../Settings"), "SettingsPane");
const RulesPane = lazyNamed(() => import("../rules/RulesPane"), "RulesPane");

// A tab page that scrolls inside the Bot page's body.
function ScrollPane({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-auto">
      <Suspense fallback={<PaneFallback />}>{children}</Suspense>
    </div>
  );
}

// Which sub-page of a pane the URL names.
function useSubPage(): SubPage {
  const view = useMatches({ select: (ms) => ms[ms.length - 1]?.staticData.view });
  const p = useParams({ strict: false });
  return { view, id: p.automationId ?? p.channelId ?? p.adapter ?? p.driveId ?? p.template };
}

// The chat side: the chats column (a strip when narrow) beside whichever
// conversation page is open.
export function ChatLayout() {
  const { id, bot, tab, chatId, chats, rename, waiting, run, agentsBusy, newChat, deleteChat } = useBotPage();
  return (
    <>
      <ChatSidebar
        id={id}
        bot={bot}
        tab={tab}
        chatId={chatId}
        chats={chats}
        rename={rename}
        live={{ waiting, sending: run.sending, agentsBusy }}
        onNewChat={newChat}
        onDeleteChat={deleteChat}
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
          onNewChat={newChat}
          onDeleteChat={deleteChat}
        />
        <Outlet />
      </section>
    </>
  );
}

export function RunRoute() {
  const p = useBotPage();
  return (
    <RunPane
      id={p.id}
      bot={p.bot}
      chatId={p.chatId}
      chats={p.chats}
      models={p.models}
      defaultModel={p.defaultModel}
      voice={p.voice}
      run={p.run}
      subs={p.subs}
      draft={p.draft}
      actions={p.actions}
      waitingRuns={p.waitingRuns}
      agentHref={p.agentHref}
      onInspectArtifact={p.inspectArtifact}
      onSaveSkill={p.saveSkill}
      onError={p.onError}
    />
  );
}

export function AgentRoute() {
  const p = useBotPage();
  if (!p.chatId || !p.agentId) return null;
  return (
    <Suspense fallback={<PaneFallback />}>
      <SubagentPage
        key={p.agentId}
        botId={p.id}
        botName={p.bot.name}
        botCrest={p.bot.crest}
        chatId={p.chatId}
        agentId={p.agentId}
        onError={p.onError}
        onApprovals={p.reloadApprovals}
        onInspectArtifact={p.inspectArtifact}
        onSaveSkill={p.saveSkill}
      />
    </Suspense>
  );
}

export function AutomationsRoute() {
  const p = useBotPage();
  return (
    <Suspense fallback={<PaneFallback />}>
      <AutomationsPane bot={p.bot} at={useSubPage()} onError={p.onError} onApprovals={p.reloadApprovals} onInspectArtifact={p.inspectArtifact} onSaveSkill={p.saveSkill} />
    </Suspense>
  );
}

export function MemoriesRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <MemoriesPane bot={p.bot} onError={p.onError} />
    </ScrollPane>
  );
}

export function KnowledgeRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <KnowledgePane bot={p.bot} onError={p.onError} onStart={p.start} />
    </ScrollPane>
  );
}

export function FeedRoute() {
  const p = useBotPage();
  const navigate = useNavigate();
  return (
    <ScrollPane>
      <FeedPane
        bot={p.bot}
        onError={p.onError}
        onQuoted={(c) => {
          patchChats(p.id, (xs) => [c, ...xs.filter((x) => x.id !== c.id)]);
          void navigate(chatLink(p.id, c.id));
        }}
      />
    </ScrollPane>
  );
}

export function FilesRoute() {
  const p = useBotPage();
  const { open } = useSearch({ from: "/_authed/bots/$botId/files" });
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <Suspense fallback={<PaneFallback />}>
        <FilesPane bot={p.bot} onStart={p.start} openAt={open ?? ""} />
      </Suspense>
    </section>
  );
}

export function ConnectorsRoute() {
  const p = useBotPage();
  return (
    <section className="min-h-0 min-w-0 flex-1 overflow-auto">
      <Suspense fallback={<PaneFallback />}>
        <BotConnectors botId={p.id} onNeedAuth={p.needAuth} />
      </Suspense>
    </section>
  );
}

export function DrivesRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <BotDrives botId={p.id} at={useSubPage()} admin={p.admin} />
    </ScrollPane>
  );
}

export function ChannelsRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <BotChannels botId={p.id} at={useSubPage()} />
    </ScrollPane>
  );
}

export function TunnelsRoute() {
  return (
    <ScrollPane>
      <TunnelsPane botId={useBotPage().id} />
    </ScrollPane>
  );
}

export function SkillsRoute() {
  return (
    <ScrollPane>
      <BotSkills botId={useBotPage().id} />
    </ScrollPane>
  );
}

export function SecretsRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <SecretsPane botId={p.id} state={p.secrets} onError={p.onError} />
    </ScrollPane>
  );
}

export function ContainerRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <ContainersPane bot={p.bot} onStart={p.start} onStop={p.stop} />
    </ScrollPane>
  );
}

export function SettingsRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <SettingsPane bot={p.bot} onError={p.onError} />
    </ScrollPane>
  );
}

export function RulesRoute() {
  return (
    <ScrollPane>
      <RulesPane botId={useBotPage().id} />
    </ScrollPane>
  );
}
