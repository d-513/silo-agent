import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet, redirect } from "@tanstack/react-router";
import { FIRST_SECTION, isSection } from "./admin/sections";
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
import { CustomizeLayout, SettingsLayout } from "./bot/SectionTabs";
import type { Tab } from "./bot/tabs";
import { BotsPage } from "./BotsPage";
import { Snag } from "./ErrorBoundary";
import { InvitePage } from "./InvitePage";
import { NewBotPage } from "./NewBotPage";
import { PaneFallback } from "./PaneFallback";
import { AccountRoute, Shell } from "./Shell";
import { SignIn } from "./SignIn";

// router.tsx is every URL the app answers, in one tree. A page is a route here
// or it does not exist: nothing parses a path by hand, and every Link and
// navigate() is checked against this file by the compiler.
//
//   /signin
//   /invite/$token                 an admin's invite or password-reset link (signed out)
//   _authed                        the rail; signed out goes to /signin
//     /  /new  /skills  /account
//     /admin                       admins only: one tab per page
//       settings/$section          Settings and its categories
//       users[/invite|/$userId]
//       connectors[/new|/$id]  skills  drives  debug
//     /bots/$botId                 BotPage: sidebar, machine rail, what survives a page change
//       _chat                      the chat and the pages beside it
//         run[/$chatId[/agent/$agentId]]  automations[/new|/$id]  memories  knowledge  feed
//       _customize                 Customize and its tabs
//         connectors  skills  drives/…  channels/…
//       _settings                  Settings and its tabs
//         settings  rules  secrets  tunnels  container
//       files desktop console      a machine pane as a page of its own
//
// `staticData.tab` is the page a route lights (a sub-page lights its parent's).

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
  // handoff (signinNext.ts), read by the page itself; `error` is why an OIDC
  // round trip came back empty-handed (signinFlow.ts).
  validateSearch: (s: Record<string, unknown>): { from?: string; next?: string; error?: string } => ({
    ...(typeof s.from === "string" ? { from: s.from } : {}),
    ...(typeof s.next === "string" ? { next: s.next } : {}),
    ...(typeof s.error === "string" ? { error: s.error } : {}),
  }),
  beforeLoad: ({ search }) => {
    if (getSession()) throw redirect(backTo(search.from));
  },
  component: SignIn,
});

// Open to anyone holding the link, signed in or not: accepting it signs in as
// the invited account.
const invite = createRoute({ getParentRoute: () => root, path: "invite/$token", component: InvitePage });

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
const adminPage = (
  name: "AdminLayout" | "AdminSettings" | "SettingsSection" | "AdminSkills" | "AdminDrives" | "AdminDebug" | "CatalogList" | "LibraryForm" | "UsersList" | "UserPage" | "InviteUser",
) =>
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
// Settings is a layout: its categories are pages under it (admin/sections.ts).
const adminSettings = createRoute({ getParentRoute: () => admin, path: "settings", component: adminPage("AdminSettings") });
const toFirstSection = () => {
  throw redirect({ to: "/admin/settings/$section", params: { section: FIRST_SECTION }, replace: true });
};
const adminSettingsIndex = createRoute({ getParentRoute: () => adminSettings, path: "/", beforeLoad: toFirstSection });
const adminSettingsSection = createRoute({
  getParentRoute: () => adminSettings,
  path: "$section",
  beforeLoad: ({ params }) => {
    if (!isSection(params.section)) toFirstSection();
  },
  component: adminPage("SettingsSection"),
});
const adminUsers = createRoute({ getParentRoute: () => admin, path: "users", component: adminPage("UsersList") });
const adminUserInvite = createRoute({ getParentRoute: () => admin, path: "users/invite", component: adminPage("InviteUser") });
const adminUser = createRoute({ getParentRoute: () => admin, path: "users/$userId", component: adminPage("UserPage") });
const adminConnectors = createRoute({ getParentRoute: () => admin, path: "connectors", component: adminPage("CatalogList") });
const adminConnectorNew = createRoute({ getParentRoute: () => admin, path: "connectors/new", component: adminPage("LibraryForm") });
const adminConnector = createRoute({ getParentRoute: () => admin, path: "connectors/$connectorId", component: adminPage("LibraryForm") });
const adminSkills = createRoute({ getParentRoute: () => admin, path: "skills", component: adminPage("AdminSkills") });
// Search & Extract was a tab of its own; it is a Settings category now.
const adminSearch = createRoute({
  getParentRoute: () => admin,
  path: "search-extract",
  beforeLoad: () => {
    throw redirect({ to: "/admin/settings/$section", params: { section: "search" }, replace: true });
  },
});
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
// A page that does not exist opens the chat.
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

// The machine panes render nothing here: BotPage shows the same pane docked
// beside the chat or, on these routes, as the page, and keeps it mounted
// (hidden) once opened, so its connection survives a trip elsewhere.
const desktop = createRoute({ getParentRoute: () => bot, path: "desktop", staticData: { tab: "desktop" } });
const consoleTab = createRoute({ getParentRoute: () => bot, path: "console", staticData: { tab: "console" } });
const files = createRoute({
  getParentRoute: () => bot,
  path: "files",
  staticData: { tab: "files" },
  // ?open=drives/work lands the tree in that folder (Drives → Open in Files).
  validateSearch: (s: Record<string, unknown>): { open?: string } => (typeof s.open === "string" ? { open: s.open } : {}),
});

const customize = createRoute({ getParentRoute: () => bot, id: "_customize", component: CustomizeLayout });
const settingsSection = createRoute({ getParentRoute: () => bot, id: "_settings", component: SettingsLayout });
const drives = createRoute({ getParentRoute: () => customize, path: "drives", staticData: { tab: "drives" }, component: ScrollLayout });
const drivesIndex = createRoute({ getParentRoute: () => drives, path: "/", component: DrivesRoute });
const driveNew = createRoute({ getParentRoute: () => drives, path: "new", component: DriveNewRoute });
const driveAdd = createRoute({ getParentRoute: () => drives, path: "new/$template", component: DriveAddRoute });
const drive = createRoute({ getParentRoute: () => drives, path: "$driveId", component: DriveRoute });
const connectors = createRoute({ getParentRoute: () => customize, path: "connectors", staticData: { tab: "connectors" }, component: ConnectorsRoute });
const channels = createRoute({ getParentRoute: () => customize, path: "channels", staticData: { tab: "channels" }, component: ScrollLayout });
const channelsIndex = createRoute({ getParentRoute: () => channels, path: "/", component: ChannelsRoute });
const channelNew = createRoute({ getParentRoute: () => channels, path: "new", component: ChannelNewRoute });
const channelAdd = createRoute({ getParentRoute: () => channels, path: "new/$adapter", component: ChannelAddRoute });
const channel = createRoute({ getParentRoute: () => channels, path: "$channelId", component: ChannelRoute });
const channelSetup = createRoute({ getParentRoute: () => channels, path: "$channelId/setup", component: ChannelSetupRoute });
const tunnels = createRoute({ getParentRoute: () => settingsSection, path: "tunnels", staticData: { tab: "tunnels" }, component: TunnelsRoute });
const botSkills = createRoute({ getParentRoute: () => customize, path: "skills", staticData: { tab: "skills" }, component: SkillsRoute });
const secrets = createRoute({ getParentRoute: () => settingsSection, path: "secrets", staticData: { tab: "secrets" }, component: SecretsRoute });
const rules = createRoute({ getParentRoute: () => settingsSection, path: "rules", staticData: { tab: "rules" }, component: RulesRoute });
const container = createRoute({ getParentRoute: () => settingsSection, path: "container", staticData: { tab: "container" }, component: ContainerRoute });
const settings = createRoute({ getParentRoute: () => settingsSection, path: "settings", staticData: { tab: "settings" }, component: SettingsRoute });

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
  invite,
  authed.addChildren([
    home,
    newBot,
    skills,
    account,
    admin.addChildren([adminIndex, adminSettings.addChildren([adminSettingsIndex, adminSettingsSection]), adminUsers, adminUserInvite, adminUser, adminConnectors, adminConnectorNew, adminConnector, adminSkills, adminSearch, adminDrives, adminDebug, adminElse]),
    bot.addChildren([
      botIndex,
      chat.addChildren([run, runChat, runAgent, automations.addChildren([automationsIndex, automationNew, automation]), memories, knowledge, feed]),
      customize.addChildren([
        connectors,
        botSkills,
        drives.addChildren([drivesIndex, driveNew, driveAdd, drive]),
        channels.addChildren([channelsIndex, channelNew, channelAdd, channel, channelSetup]),
      ]),
      settingsSection.addChildren([settings, rules, secrets, tunnels, container]),
      files,
      desktop,
      consoleTab,
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
