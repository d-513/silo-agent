import { Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { Suspense, type ReactNode } from "react";
import { lazyNamed } from "../lazyNamed";
import { chatLink } from "../links";
import { PaneFallback } from "../PaneFallback";
import { useBotPage } from "./context";
import { RunPane } from "./RunPane";
import { SecretsPane } from "./SecretsPane";
import { patchChats } from "./useChatList";

// The pages under /bots/$botId (router.tsx). Each is a thin adapter: it takes
// what its pane needs from BotPage and renders the pane. Everything but the
// chat is loaded the first time its page opens. Files, Desktop, Console and
// Mail have no adapter: BotPage renders them itself, docked or as the page.
const AutomationsList = lazyNamed(() => import("../Automations"), "AutomationsList");
const AutomationNew = lazyNamed(() => import("../Automations"), "AutomationNew");
const AutomationLog = lazyNamed(() => import("../Automations"), "AutomationLog");
const ArtifactsPane = lazyNamed(() => import("../Artifacts"), "ArtifactsPane");
const MemoriesPane = lazyNamed(() => import("../Memories"), "MemoriesPane");
const KnowledgePane = lazyNamed(() => import("../Knowledge"), "KnowledgePane");
const DashboardPane = lazyNamed(() => import("../dashboard/Dashboard"), "DashboardPane");
const FeedPane = lazyNamed(() => import("../Feed"), "FeedPane");
const SubagentPage = lazyNamed(() => import("../SubagentPage"), "SubagentPage");
const BotPeople = lazyNamed(() => import("../space/BotPeople"), "BotPeople");
const ChannelsList = lazyNamed(() => import("../channels/BotChannels"), "ChannelsList");
const ChannelNew = lazyNamed(() => import("../channels/BotChannels"), "ChannelNew");
const ChannelAdd = lazyNamed(() => import("../channels/BotChannels"), "ChannelAdd");
const ChannelEdit = lazyNamed(() => import("../channels/BotChannels"), "ChannelEdit");
const ChannelSetupPage = lazyNamed(() => import("../channels/BotChannels"), "ChannelSetupPage");
const TunnelsPane = lazyNamed(() => import("./Tunnels"), "TunnelsPane");
const ContainersPane = lazyNamed(() => import("./Containers"), "ContainersPane");
const SettingsPane = lazyNamed(() => import("../Settings"), "SettingsPane");
const RulesPane = lazyNamed(() => import("../rules/RulesPane"), "RulesPane");
const AuditPane = lazyNamed(() => import("../AuditPane"), "AuditPane");

// A tab page that scrolls inside the Bot page's body.
function ScrollPane({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-auto">
      <Suspense fallback={<PaneFallback />}>{children}</Suspense>
    </div>
  );
}

// The frame of a tab with sub-pages: they scroll in it, one at a time.
export function ScrollLayout() {
  return (
    <ScrollPane>
      <Outlet />
    </ScrollPane>
  );
}

// The chat side: the conversation, or one of the pages beside it.
export function ChatLayout() {
  return (
    <section className="flex min-w-0 flex-1 flex-col">
      <Outlet />
    </section>
  );
}

export function RunRoute() {
  const p = useBotPage();
  return (
    <RunPane
      id={p.id}
      bot={p.bot}
      chatId={p.chatId}
      fresh={p.fresh}
      pick={p.pick}
      chats={p.chats}
      models={p.models}
      defaultModel={p.defaultModel}
      voice={p.voice}
      talk={p.talk}
      call={p.call}
      onTalk={p.startCall}
      run={p.run}
      subs={p.subs}
      draft={p.draft}
      actions={p.actions}
      said={p.said}
      setSaid={p.setSaid}
      waiting={p.waiting}
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

// Automations lay themselves out (the log fills the height), so their frame
// only waits for the code.
export function AutomationsLayout() {
  return (
    <Suspense fallback={<PaneFallback />}>
      <Outlet />
    </Suspense>
  );
}

export function AutomationsRoute() {
  const p = useBotPage();
  return <AutomationsList bot={p.bot} onError={p.onError} />;
}

export function AutomationNewRoute() {
  const p = useBotPage();
  return <AutomationNew bot={p.bot} onError={p.onError} />;
}

export function AutomationRoute() {
  const p = useBotPage();
  const { automationId } = useParams({ from: "/_authed/bots/$botId/_chat/automations/$automationId" });
  return <AutomationLog key={automationId} bot={p.bot} automationId={automationId} onError={p.onError} onApprovals={p.reloadApprovals} onInspectArtifact={p.inspectArtifact} onSaveSkill={p.saveSkill} />;
}

export function ArtifactsRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <ArtifactsPane botId={p.id} onInspectArtifact={p.inspectArtifact} onSaveSkill={p.saveSkill} onError={p.onError} />
    </ScrollPane>
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

export function DashboardRoute() {
  const p = useBotPage();
  const navigate = useNavigate();
  return (
    <ScrollPane>
      <DashboardPane
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

export function ChannelsRoute() {
  return <ChannelsList botId={useBotPage().id} />;
}

export function ChannelNewRoute() {
  return <ChannelNew botId={useBotPage().id} />;
}

export function ChannelAddRoute() {
  const { adapter } = useParams({ from: "/_authed/bots/$botId/channels/new/$adapter" });
  return <ChannelAdd botId={useBotPage().id} adapter={adapter} />;
}

export function ChannelRoute() {
  const { channelId } = useParams({ from: "/_authed/bots/$botId/channels/$channelId" });
  return <ChannelEdit botId={useBotPage().id} channelId={channelId} />;
}

export function ChannelSetupRoute() {
  const { channelId } = useParams({ from: "/_authed/bots/$botId/channels/$channelId/setup" });
  return <ChannelSetupPage botId={useBotPage().id} channelId={channelId} />;
}

export function TunnelsRoute() {
  return (
    <ScrollPane>
      <TunnelsPane botId={useBotPage().id} />
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

export function PeopleRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <BotPeople bot={p.bot} />
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

export function AuditRoute() {
  return (
    <ScrollPane>
      <AuditPane botId={useBotPage().id} />
    </ScrollPane>
  );
}
