import { Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { Suspense, type ReactNode } from "react";
import { lazyNamed } from "../lazyNamed";
import { chatLink } from "../links";
import { PaneFallback } from "../PaneFallback";
import { useBotPage } from "./context";
import { chatOf, type TabKey } from "./layout";
import { RunPane } from "./RunPane";
import { SecretsPane } from "./SecretsPane";
import { Act, Grip, slotBox } from "./SidePane";
import { SplitChat } from "./SplitChat";
import { tabMenu, type TabControl } from "./tabControl";
import { keyIcon, keyLabel } from "./tabMeta";
import { onMenu } from "./TabMenu";
import { patchChats } from "./useChatList";
import { t } from "../i18n.ts";
import { Maximize2, X } from "lucide-react";

// The pages under /bots/$botId (router.tsx). Each is a thin adapter: it takes
// what its pane needs from BotPage and renders the pane. Everything but the
// chat is loaded the first time its page opens. Files, Desktop, Console,
// Changes and Mail have no adapter: BotPage renders them itself, as the page
// or in the split. SplitPage shows any other tab in the split.
const AutomationsList = lazyNamed(() => import("../Automations"), "AutomationsList");
const AutomationNew = lazyNamed(() => import("../Automations"), "AutomationNew");
const AutomationLog = lazyNamed(() => import("../Automations"), "AutomationLog");
const ArtifactsPane = lazyNamed(() => import("../Artifacts"), "ArtifactsPane");
const MemoriesPane = lazyNamed(() => import("../Memories"), "MemoriesPane");
const RunsPane = lazyNamed(() => import("../Runs"), "RunsPane");
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
        bot={p.bot}
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

export function RunsRoute() {
  const p = useBotPage();
  return (
    <ScrollPane>
      <RunsPane bot={p.bot} onError={p.onError} />
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

// The split's own view of a tab that is a page, not a pane: the same adapter
// the route renders (a page with sub-pages shows its first; its links lead
// the page there), or a chat with a stream of its own.
function SplitBody({ k, mainId }: { k: TabKey; mainId?: string }) {
  if (k === "main") return mainId ? <SplitChat key={mainId} chatId={mainId} /> : null;
  const cid = chatOf(k);
  if (cid) return <SplitChat key={cid} chatId={cid} />;
  switch (k) {
    case "dashboard":
      return <DashboardRoute />;
    case "automations":
      return (
        <ScrollPane>
          <AutomationsRoute />
        </ScrollPane>
      );
    case "artifacts":
      return <ArtifactsRoute />;
    case "memories":
      return <MemoriesRoute />;
    case "knowledge":
      return <KnowledgeRoute />;
    case "feed":
      return <FeedRoute />;
    case "channels":
      return (
        <ScrollPane>
          <ChannelsRoute />
        </ScrollPane>
      );
    case "settings":
      return <SettingsRoute />;
    case "runs":
      return <RunsRoute />;
  }
  return null;
}

// SplitPage is a tab in the split beside the page: a head with its name (a
// right-click is the tab's menu), the way to make it the page or to close
// the split, and the tab under it. The edge on its left resizes it.
export function SplitPage({ k, ctl, share }: { k: TabKey; ctl: TabControl; share: number }) {
  const box = slotBox("side", share);
  const label = keyLabel(k, ctl.chats);
  const Icon = keyIcon(k);
  return (
    <section aria-label={t("Split view")} className={`${box.className} bg-canvas`} style={box.style}>
      <Grip />
      <header
        onContextMenu={onMenu(label, () => tabMenu(k, ctl, "split"))}
        className="flex h-9 shrink-0 items-center gap-2 pr-1.5 pl-3.5 shadow-[inset_0_-1px_0_var(--color-line)]"
      >
        <Icon size={14} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate text-small font-medium text-ink">{label}</span>
        <Act label={t("Open as the page")} icon={Maximize2} onClick={() => ctl.open(k)} />
        <Act label={t("Close split view")} icon={X} onClick={ctl.unsplit} />
      </header>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <Suspense fallback={<PaneFallback />}>
          <SplitBody k={k} mainId={ctl.mainId} />
        </Suspense>
      </div>
    </section>
  );
}
