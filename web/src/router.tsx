import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet, redirect } from "@tanstack/react-router";
import { getSession, onSession } from "./auth";
import { BotPage } from "./bot/BotPage";
import {
  AgentRoute,
  AutomationNewRoute,
  AutomationRoute,
  AutomationsLayout,
  AutomationsRoute,
  ChannelAddRoute,
  ChannelNewRoute,
  ChannelRoute,
  ChannelSetupRoute,
  ChannelsRoute,
  ChatLayout,
  ConnectorsRoute,
  ContainerRoute,
  DriveAddRoute,
  DriveNewRoute,
  DriveRoute,
  DrivesRoute,
  FeedRoute,
  FilesRoute,
  KnowledgeRoute,
  MemoriesRoute,
  RulesRoute,
  RunRoute,
  ScrollLayout,
  SecretsRoute,
  SettingsRoute,
  SkillsRoute,
  TunnelsRoute,
} from "./bot/routes";
import type { Tab } from "./bot/tabs";
import { BotsPage } from "./BotsPage";
import { Snag } from "./ErrorBoundary";
import { NewBotPage } from "./NewBotPage";
import { PaneFallback } from "./PaneFallback";
import { AccountRoute, Shell } from "./Shell";
import { SignIn } from "./SignIn";

// router.tsx is every URL the app answers, in one tree. A page is a route here
// or it does not exist: nothing parses a path by hand, and every Link and
// navigate() is checked against this file by the compiler.
//
//   /signin
//   _authed                        the rail; signed out goes to /signin
//     /  /new  /skills  /account
//     /admin/…                     admins only
//     /bots/$botId                 BotPage: header, tabs, what survives a tab change
//       _chat                      the chats sidebar and its side pages
//         run[/$chatId[/agent/$agentId]]  automations[/new|/$id]  memories  knowledge  feed
//       desktop console files drives/… connectors channels/… tunnels skills
//       secrets rules container settings
//
// `staticData.tab` is the tab a route lights (a sub-page lights its parent's).

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
  interface StaticDataRouteOption {
    tab?: Tab;
  }
}

const root = createRootRoute({ component: Outlet });

// The page a session ended on, as a redirect target. Only a plain path on this
// site is accepted; anything else goes home.
function backTo(from: string | undefined) {
  if (!from || !from.startsWith("/") || from.startsWith("//") || from.includes("\\") || from.startsWith("/signin")) return { to: "/" as const };
  const u = new URL(from, window.location.origin);
  return { to: u.pathname, search: Object.fromEntries(u.searchParams) } as unknown as { to: "/" };
}

const signin = createRoute({
  getParentRoute: () => root,
  path: "signin",
  // `from` is where to return after signing in; `next` is a private tunnel's
  // handoff (signinNext.ts), read by the page itself.
  validateSearch: (s: Record<string, unknown>): { from?: string; next?: string } => ({
    ...(typeof s.from === "string" ? { from: s.from } : {}),
    ...(typeof s.next === "string" ? { next: s.next } : {}),
  }),
  beforeLoad: ({ search }) => {
    if (getSession()) throw redirect(backTo(search.from));
  },
  component: SignIn,
});

const authed = createRoute({
  getParentRoute: () => root,
  id: "_authed",
  beforeLoad: ({ location }) => {
    if (!getSession()) throw redirect({ to: "/signin", search: location.href === "/" ? {} : { from: location.href } });
  },
  component: Shell,
});

const home = createRoute({ getParentRoute: () => authed, path: "/", component: BotsPage });
const newBot = createRoute({ getParentRoute: () => authed, path: "new", component: NewBotPage });
const skills = createRoute({ getParentRoute: () => authed, path: "skills", component: lazyRouteComponent(() => import("./Skills"), "SkillHub") });
const account = createRoute({ getParentRoute: () => authed, path: "account", component: AccountRoute });

// Everything under /admin is one chunk, loaded the first time an admin opens it.
const adminPage = (name: "AdminLayout" | "AdminSettings" | "AdminSkills" | "AdminDrives" | "AdminSearchExtract" | "AdminDebug" | "CatalogList" | "LibraryForm") =>
  lazyRouteComponent(() => import("./AdminApp"), name);

const admin = createRoute({
  getParentRoute: () => authed,
  path: "admin",
  beforeLoad: () => {
    if (!getSession()?.admin) throw redirect({ to: "/" });
  },
  component: adminPage("AdminLayout"),
});
const adminIndex = createRoute({
  getParentRoute: () => admin,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/admin/settings", replace: true });
  },
});
const adminSettings = createRoute({ getParentRoute: () => admin, path: "settings", component: adminPage("AdminSettings") });
const adminConnectors = createRoute({ getParentRoute: () => admin, path: "connectors", component: adminPage("CatalogList") });
const adminConnectorNew = createRoute({ getParentRoute: () => admin, path: "connectors/new", component: adminPage("LibraryForm") });
const adminConnector = createRoute({ getParentRoute: () => admin, path: "connectors/$connectorId", component: adminPage("LibraryForm") });
const adminSkills = createRoute({ getParentRoute: () => admin, path: "skills", component: adminPage("AdminSkills") });
const adminSearch = createRoute({ getParentRoute: () => admin, path: "search-extract", component: adminPage("AdminSearchExtract") });
const adminDrives = createRoute({ getParentRoute: () => admin, path: "drives", component: adminPage("AdminDrives") });
const adminDebug = createRoute({ getParentRoute: () => admin, path: "debug", component: adminPage("AdminDebug") });
const adminElse = createRoute({
  getParentRoute: () => admin,
  path: "$",
  beforeLoad: () => {
    throw redirect({ to: "/" });
  },
});

const bot = createRoute({ getParentRoute: () => authed, path: "bots/$botId", component: BotPage });
const toChat = ({ params }: { params: { botId: string } }) => {
  throw redirect({ to: "/bots/$botId/run", params: { botId: params.botId }, replace: true });
};
const botIndex = createRoute({ getParentRoute: () => bot, path: "/", beforeLoad: toChat });
// A tab that does not exist opens the chat.
const botElse = createRoute({ getParentRoute: () => bot, path: "$", beforeLoad: toChat });

const chat = createRoute({ getParentRoute: () => bot, id: "_chat", component: ChatLayout });
const run = createRoute({ getParentRoute: () => chat, path: "run", staticData: { tab: "run" }, component: RunRoute });
const runChat = createRoute({ getParentRoute: () => chat, path: "run/$chatId", staticData: { tab: "run" }, component: RunRoute });
const runAgent = createRoute({ getParentRoute: () => chat, path: "run/$chatId/agent/$agentId", staticData: { tab: "run" }, component: AgentRoute });
const automations = createRoute({ getParentRoute: () => chat, path: "automations", staticData: { tab: "automations" }, component: AutomationsLayout });
const automationsIndex = createRoute({ getParentRoute: () => automations, path: "/", component: AutomationsRoute });
const automationNew = createRoute({ getParentRoute: () => automations, path: "new", component: AutomationNewRoute });
const automation = createRoute({ getParentRoute: () => automations, path: "$automationId", component: AutomationRoute });
const memories = createRoute({ getParentRoute: () => chat, path: "memories", staticData: { tab: "memories" }, component: MemoriesRoute });
const knowledge = createRoute({ getParentRoute: () => chat, path: "knowledge", staticData: { tab: "knowledge" }, component: KnowledgeRoute });
const feed = createRoute({ getParentRoute: () => chat, path: "feed", staticData: { tab: "feed" }, component: FeedRoute });

// Desktop and Console render nothing here: BotPage keeps them mounted (hidden)
// once opened, so their connections survive a trip to another tab.
const desktop = createRoute({ getParentRoute: () => bot, path: "desktop", staticData: { tab: "desktop" } });
const consoleTab = createRoute({ getParentRoute: () => bot, path: "console", staticData: { tab: "console" } });
const files = createRoute({
  getParentRoute: () => bot,
  path: "files",
  staticData: { tab: "files" },
  // ?open=drives/work lands the tree in that folder (Drives → Open in Files).
  validateSearch: (s: Record<string, unknown>): { open?: string } => (typeof s.open === "string" ? { open: s.open } : {}),
  component: FilesRoute,
});
const drives = createRoute({ getParentRoute: () => bot, path: "drives", staticData: { tab: "drives" }, component: ScrollLayout });
const drivesIndex = createRoute({ getParentRoute: () => drives, path: "/", component: DrivesRoute });
const driveNew = createRoute({ getParentRoute: () => drives, path: "new", component: DriveNewRoute });
const driveAdd = createRoute({ getParentRoute: () => drives, path: "new/$template", component: DriveAddRoute });
const drive = createRoute({ getParentRoute: () => drives, path: "$driveId", component: DriveRoute });
const connectors = createRoute({ getParentRoute: () => bot, path: "connectors", staticData: { tab: "connectors" }, component: ConnectorsRoute });
const channels = createRoute({ getParentRoute: () => bot, path: "channels", staticData: { tab: "channels" }, component: ScrollLayout });
const channelsIndex = createRoute({ getParentRoute: () => channels, path: "/", component: ChannelsRoute });
const channelNew = createRoute({ getParentRoute: () => channels, path: "new", component: ChannelNewRoute });
const channelAdd = createRoute({ getParentRoute: () => channels, path: "new/$adapter", component: ChannelAddRoute });
const channel = createRoute({ getParentRoute: () => channels, path: "$channelId", component: ChannelRoute });
const channelSetup = createRoute({ getParentRoute: () => channels, path: "$channelId/setup", component: ChannelSetupRoute });
const tunnels = createRoute({ getParentRoute: () => bot, path: "tunnels", staticData: { tab: "tunnels" }, component: TunnelsRoute });
const botSkills = createRoute({ getParentRoute: () => bot, path: "skills", staticData: { tab: "skills" }, component: SkillsRoute });
const secrets = createRoute({ getParentRoute: () => bot, path: "secrets", staticData: { tab: "secrets" }, component: SecretsRoute });
const rules = createRoute({ getParentRoute: () => bot, path: "rules", staticData: { tab: "rules" }, component: RulesRoute });
const container = createRoute({ getParentRoute: () => bot, path: "container", staticData: { tab: "container" }, component: ContainerRoute });
const settings = createRoute({ getParentRoute: () => bot, path: "settings", staticData: { tab: "settings" }, component: SettingsRoute });

// Any other address goes home (and from there to Sign in when signed out).
const elsewhere = createRoute({
  getParentRoute: () => root,
  path: "$",
  beforeLoad: () => {
    throw redirect({ to: "/" });
  },
});

const routeTree = root.addChildren([
  signin,
  authed.addChildren([
    home,
    newBot,
    skills,
    account,
    admin.addChildren([adminIndex, adminSettings, adminConnectors, adminConnectorNew, adminConnector, adminSkills, adminSearch, adminDrives, adminDebug, adminElse]),
    bot.addChildren([
      botIndex,
      chat.addChildren([run, runChat, runAgent, automations.addChildren([automationsIndex, automationNew, automation]), memories, knowledge, feed]),
      desktop,
      consoleTab,
      files,
      drives.addChildren([drivesIndex, driveNew, driveAdd, drive]),
      connectors,
      channels.addChildren([channelsIndex, channelNew, channelAdd, channel, channelSetup]),
      tunnels,
      botSkills,
      secrets,
      rules,
      container,
      settings,
      botElse,
    ]),
  ]),
  elsewhere,
]);

export const router = createRouter({
  routeTree,
  // A render crash stays inside the page that threw; the rail and every other
  // page stay usable, and navigating away clears it.
  defaultErrorComponent: ({ error, reset }) => <Snag error={error} onRetry={reset} />,
  defaultPendingComponent: PaneFallback,
});

// Signing in or out re-runs the guards above: out lands on /signin, in lands
// back on the page the session ended on.
onSession(() => void router.invalidate());
