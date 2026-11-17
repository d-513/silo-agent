import Observation
import SiloClient
import SwiftUI

/// The signed-in app, laid out as the web lays it out below 960: the page under the Bot's top bar
/// and its chat tabs, the sidebar (the Workspace and its Bots) as a sheet from the left, and the
/// Bot's rail (its pages, the machine, Settings) as a sheet from the right; each column keeps the
/// side it stands on. On an iPad the sidebar stays as a column. With no Bot open (a Workspace with
/// none of the user's), or with the sidebar's gear pressed, the page is the Workspace's.
struct RootView: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        VStack(spacing: 0) {
            let call = CallModel.shared
            if call.snap != nil && !call.expanded {
                CallBar(call: call)
            }
            shell.frame(maxHeight: .infinity)
        }
        .overlay {
            let call = CallModel.shared
            if call.snap != nil && call.expanded {
                CallStage(call: call).transition(.opacity.combined(with: .offset(y: 24)))
            }
        }
        .animation(Motion.quiet(0.32), value: CallModel.shared.expanded)
        .animation(Motion.state, value: CallModel.shared.snap == nil)
        .background(Theme.canvas.ignoresSafeArea())
        .task(id: model.selectedBotID) { await model.chooseBot(model.selectedBotID) }
        // The Workspaces, for whoever is signed in; listed again now and then for new ones.
        .task(id: model.user?.id) {
            spaces.reset()
            while !Task.isCancelled {
                await spaces.load()
                try? await Task.sleep(for: .seconds(30))
            }
        }
        .onChange(of: model.selectedBot?.spaceID, initial: true) { spaces.follow(model.selectedBot) }
        .overlay(alignment: .top) { ErrorBanner() }
        .siloOverlayHost()
        .toggleStyle(.silo)
        #if DEBUG
        .overlay { if model.showGallery { WidgetGallery() } }
        #endif
    }

    @ViewBuilder
    private var shell: some View {
        if sizeClass == .regular {
            HStack(spacing: 0) {
                Sidebar().frame(width: 300)
                Theme.line.frame(width: 1).ignoresSafeArea()
                EdgeSheets(left: false) { page }
            }
        } else {
            EdgeSheets(left: true) { page }
        }
    }

    @ViewBuilder
    private var page: some View {
        if model.selectedBot == nil || model.workspaceOpen {
            SpaceHome()
        } else {
            BotShell()
        }
    }
}

/// The two sheets at the screen's edges, over the page: the sidebar from the left (on a phone) and
/// the open Bot's rail from the right. Each follows the finger and settles open or shut past a third
/// of its width; a swipe in from its edge pulls it out. The left edge is the system's Back on a
/// pushed page, so there it only opens at the root.
private struct EdgeSheets<Content: View>: View {
    @Environment(AppModel.self) private var model
    let left: Bool
    @ViewBuilder let content: Content
    @State private var leftDrag: CGFloat = 0
    @State private var rightDrag: CGFloat = 0

    private var bot: Silo_V1_Bot? { model.workspaceOpen ? nil : model.selectedBot }
    private var atRoot: Bool { model.workspaceOpen || model.selectedBot == nil ? model.spacePath.isEmpty : model.path.isEmpty }

    var body: some View {
        GeometryReader { geometry in
            let width = min(330, geometry.size.width * 0.86)
            let leftOpen = left && model.sidebarOpen
            let rightOpen = bot != nil && model.railOpen
            let leftOffset = leftOpen ? min(0, leftDrag) : -width + max(0, leftDrag)
            let rightOffset = rightOpen ? max(0, rightDrag) : width + min(0, rightDrag)
            let shown = max(left ? (leftOffset + width) / width : 0, bot != nil ? (width - rightOffset) / width : 0)

            ZStack {
                content

                Theme.canvas
                    .opacity(0.55 * shown)
                    .ignoresSafeArea()
                    .allowsHitTesting(leftOpen || rightOpen)
                    .onTapGesture {
                        model.sidebarOpen = false
                        model.railOpen = false
                    }

                if left {
                    Sidebar()
                        .frame(width: width)
                        .shadow(color: Theme.shade.opacity(0.08 * shown), radius: 18, x: 6)
                        .offset(x: leftOffset)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .gesture(leftOpen ? drag($leftDrag, width: width, sign: -1) { if $0 { model.sidebarOpen = false } } : nil)
                        .accessibilityHidden(!leftOpen)
                }
                if let bot {
                    RailSheet(bot: bot)
                        .frame(width: width)
                        .shadow(color: Theme.shade.opacity(0.08 * shown), radius: 18, x: -6)
                        .offset(x: rightOffset)
                        .frame(maxWidth: .infinity, alignment: .trailing)
                        .gesture(rightOpen ? drag($rightDrag, width: width, sign: 1) { if $0 { model.railOpen = false } } : nil)
                        .accessibilityHidden(!rightOpen)
                }

                // The edges that pull a sheet out, clear of the top bars and the composer.
                HStack(spacing: 0) {
                    if left && !leftOpen && !rightOpen && atRoot {
                        edge.gesture(drag($leftDrag, width: width, sign: 1) { if $0 { model.sidebarOpen = true } })
                    }
                    Spacer(minLength: 0)
                    if bot != nil && !leftOpen && !rightOpen {
                        edge.gesture(drag($rightDrag, width: width, sign: -1) { if $0 { model.railOpen = true } })
                    }
                }
                .padding(.top, 100)
                .padding(.bottom, 130)
                .ignoresSafeArea(edges: .horizontal)
            }
            .animation(Motion.quiet(0.32), value: model.sidebarOpen)
            .animation(Motion.quiet(0.32), value: model.railOpen)
        }
    }

    private var edge: some View {
        Color.clear.frame(width: 16).contentShape(Rectangle())
    }

    /// A drag along the x axis in `sign`'s direction; past a third of the width (or flung past half)
    /// it ends with true.
    private func drag(_ value: Binding<CGFloat>, width: CGFloat, sign: CGFloat, ended: @escaping (Bool) -> Void) -> some Gesture {
        DragGesture(minimumDistance: 8)
            .onChanged { g in
                let x = g.translation.width * sign
                value.wrappedValue = max(0, min(width, x)) * sign
            }
            .onEnded { g in
                let x = g.translation.width * sign
                let p = g.predictedEndTranslation.width * sign
                value.wrappedValue = 0
                ended(x > width / 3 || p > width / 2)
            }
    }
}

/// A Bot: its top bar, its chat tabs, and under them the chat or the page the rail opened, with
/// whatever that page pushed.
private struct BotShell: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            VStack(spacing: 0) {
                BotTopBar(bot: bot)
                ChatTabsStrip(bot: bot)
                BotStack()
            }
            .background(Theme.canvas.ignoresSafeArea())
        }
    }
}

/// The chat or the open page at the root, and the pages it pushes over it.
struct BotStack: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        NavigationStack(path: $model.path) {
            root
                .navigationDestination(for: BotRoute.self) { route in
                    switch route {
                    case .page(let tab): BotPage(tab: tab)
                    case .settings: SectionPage(title: "Settings", tabs: BotTab.settingsTabs)
                    case .workspace: SiloPage(title: "Workspace") { WorkspacePage() }
                    case .space(let tab): SpaceTabPage(tab: tab)
                    }
                }
        }
        .id(model.selectedBotID)
    }

    @ViewBuilder
    private var root: some View {
        switch model.page {
        case .page(let tab)?: BotPage(tab: tab, back: false)
        case .settings?: SectionPage(title: "Settings", tabs: BotTab.settingsTabs, back: false)
        default: ChatScreen()
        }
    }
}

/// The current Workspace's page, in the Bot's place: its head, its tabs, and what they push. A new
/// Workspace, or one with none of the user's Bots, lands here; the sidebar's gear opens it too.
private struct SpaceHome: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        NavigationStack(path: $model.spacePath) {
            SiloPage(title: "Workspace", back: false, menu: true) {
                WorkspacePage()
            }
            .navigationDestination(for: BotRoute.self) { route in
                if case .space(let tab) = route { SpaceTabPage(tab: tab) } else { SiloPage(title: "Workspace") { WorkspacePage() } }
            }
        }
    }
}

/// The Workspace switcher: the user's Workspaces (the current one checked, with their role), then
/// New workspace. Picking one opens its first Bot, or its page when it has none.
struct SpaceSwitcher<Content: View>: View {
    @Environment(SpaceModel.self) private var spaces
    let newSpace: () -> Void
    @ViewBuilder let label: () -> Content

    var body: some View {
        SiloMenu {
            MenuHeading(text: "Workspaces")
            ForEach(spaces.spaces) { space in
                MenuRow(title: space.name, detail: spaceRoleWord(space.role), checked: space.id == spaces.currentID) {
                    spaces.open(space.id)
                }
            }
            MenuDivider()
            MenuRow(title: "New workspace", symbol: "plus", action: newSpace)
        } label: {
            label()
        }
        .accessibilityLabel(spaces.current.map { "Workspace: \($0.name)" } ?? "Workspaces")
    }
}

/// A failed action, shown under the top bar; tap to dismiss.
struct ErrorBanner: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let message = model.errorMessage {
            Button {
                model.errorMessage = nil
            } label: {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Image(systemName: "exclamationmark.triangle")
                    Text(message).multilineTextAlignment(.leading)
                    Spacer(minLength: 0)
                    Image(systemName: "xmark").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
                }
                .font(.silo(13))
                .foregroundStyle(Theme.vermilion)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.line))
                .shadow(color: .black.opacity(0.06), radius: 10, y: 4)
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 12)
            .padding(.top, 52)
            .transition(.move(edge: .top).combined(with: .opacity))
        }
    }
}

/// The Settings list: one row per page, as the web's section tabs.
struct SectionPage: View {
    @Environment(AppModel.self) private var model
    let title: String
    let tabs: [BotTab]
    var back = true

    var body: some View {
        SiloPage(title: title, back: back) {
            PageScroll {
                Panel {
                    ForEach(tabs) { tab in
                        PanelRow(symbol: tab.symbol, title: tab == .settings ? "General" : tab.title, chevron: true) {
                            model.path.append(.page(tab))
                        }
                    }
                }
                Text("Desktop and Console stay on the web client. Connectors, skills and drives are the workspace's: they are on the Workspace page.")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .padding(.horizontal, 4)
            }
        }
    }
}

/// A pushed Bot page in the page frame; each page puts its own actions in the head with `.pageActions`.
struct BotPage: View {
    let tab: BotTab
    /// False when the rail opened it in the chat's place, with nothing under it to go back to.
    var back = true

    var body: some View {
        SiloPage(title: tab == .settings ? "General" : tab.title, back: back) { content }
    }

    @ViewBuilder
    private var content: some View {
        switch tab {
        case .dashboard: DashboardPage()
        case .automations: AutomationsPage()
        case .artifacts: ArtifactsPage()
        case .knowledge: KnowledgePage()
        case .changes: ChangesPage()
        case .mail: MailPage()
        case .tunnels: TunnelsPage()
        case .audit: AuditPage()
        case .memories: MemoriesPage()
        case .feed: FeedPage()
        case .files: FilesPage()
        case .channels: ChannelsPage()
        case .people: BotPeoplePage()
        case .workspace: MoveBotPage()
        case .secrets: SecretsPage()
        case .rules: RulesPage()
        case .container: ContainersPage()
        case .settings: BotSettingsPage()
        case .chat, .desktop: PlaceholderPane(tab: tab)
        }
    }
}
