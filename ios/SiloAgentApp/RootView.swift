import Observation
import SiloClient
import SwiftUI

/// The signed-in app. It opens on a chat, as the web does: the Bot's sidebar (the Workspace's
/// crests, its pages, chats, Channels, the Workspace and Settings) slides in from the left edge on
/// a phone and stays as a column on an iPad; the machine (Files, Changes, Mail, Start/Stop) is a
/// sheet off the status lamp. With no Bot open (a Workspace with none of the user's) it is the
/// Workspace page.
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
        Group {
            if model.selectedBot == nil {
                SpaceHome()
            } else if sizeClass == .regular {
                HStack(spacing: 0) {
                    BotSidebar().frame(width: 300)
                    Theme.line.frame(width: 1).ignoresSafeArea()
                    BotStack()
                }
            } else {
                Drawer()
            }
        }
    }
}

/// The phone shell: the chat stack, and the sidebar over it. A swipe from the left edge opens the
/// sidebar while the chat is on top (on a pushed page that swipe is the system's Back).
private struct Drawer: View {
    @Environment(AppModel.self) private var model
    @State private var drag: CGFloat = 0

    var body: some View {
        GeometryReader { geometry in
            let width = min(330, geometry.size.width * 0.86)
            let open = model.sidebarOpen
            let offset = open ? min(0, drag) : -width + max(0, drag)
            let shown = (offset + width) / width

            ZStack(alignment: .leading) {
                BotStack()

                Theme.canvas
                    .opacity(0.55 * shown)
                    .ignoresSafeArea()
                    .allowsHitTesting(open)
                    .onTapGesture { model.sidebarOpen = false }

                BotSidebar()
                    .frame(width: width)
                    .background(Theme.well.ignoresSafeArea())
                    .shadow(color: .black.opacity(0.08 * shown), radius: 18, x: 6)
                    .offset(x: offset)
                    .gesture(open ? closeGesture(width) : nil)

                if !open && model.path.isEmpty {
                    Color.clear
                        .frame(width: 18)
                        .contentShape(Rectangle())
                        .gesture(openGesture(width))
                        .ignoresSafeArea()
                }
            }
            .animation(Motion.quiet(0.32), value: model.sidebarOpen)
        }
    }

    private func openGesture(_ width: CGFloat) -> some Gesture {
        DragGesture(minimumDistance: 8)
            .onChanged { drag = min(width, max(0, $0.translation.width)) }
            .onEnded { value in
                let opens = value.translation.width > width / 3 || value.predictedEndTranslation.width > width / 2
                drag = 0
                model.sidebarOpen = opens
            }
    }

    private func closeGesture(_ width: CGFloat) -> some Gesture {
        DragGesture(minimumDistance: 8)
            .onChanged { drag = min(0, $0.translation.width) }
            .onEnded { value in
                let closes = value.translation.width < -width / 3 || value.predictedEndTranslation.width < -width / 2
                drag = 0
                if closes { model.sidebarOpen = false }
            }
    }
}

/// The chat at the root and the Bot's pages pushed over it.
struct BotStack: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        NavigationStack(path: $model.path) {
            ChatScreen()
                .navigationDestination(for: BotRoute.self) { route in
                    switch route {
                    case .page(let tab): BotPage(tab: tab)
                    case .settings: SectionPage(title: "Settings", tabs: [.settings, .people, .rules, .secrets, .tunnels, .container, .audit, .workspace])
                    case .workspace: SiloPage(title: "Workspace") { WorkspacePage() }
                    case .space(let tab): SpaceTabPage(tab: tab)
                    }
                }
        }
        .id(model.selectedBotID)
        .sheet(isPresented: $model.machineOpen) { MachineSheet() }
    }
}

/// No Bot open: the current Workspace's page at the root, with the Workspace switcher and the
/// account in its head. A new Workspace, or one with none of the user's Bots, lands here.
private struct SpaceHome: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var path: [BotRoute] = []
    @State private var account = false
    @State private var newSpace = false

    var body: some View {
        NavigationStack(path: $path) {
            SiloPage(title: spaces.current?.name ?? "Workspace", back: false) {
                WorkspacePage()
                    .pageActions {
                        SpaceSwitcher(newSpace: { newSpace = true }) {
                            Image(systemName: "chevron.up.chevron.down").font(.system(size: 13, weight: .medium))
                                .foregroundStyle(Theme.ink2).frame(width: 40, height: 40)
                        }
                        Button { account = true } label: {
                            Image(systemName: "person.crop.circle")
                        }
                        .buttonStyle(.glyph)
                        .accessibilityLabel(model.user?.email ?? "Account")
                    }
            }
            .navigationDestination(for: BotRoute.self) { route in
                if case .space(let tab) = route { SpaceTabPage(tab: tab) } else { SiloPage(title: "Workspace") { WorkspacePage() } }
            }
        }
        .sheet(isPresented: $account) { AccountView(modal: true).siloSheet() }
        .sheet(isPresented: $newSpace) { NewSpaceSheet().siloSheet() }
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

    var body: some View {
        SiloPage(title: title) {
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

    var body: some View {
        SiloPage(title: tab == .settings ? "General" : tab.title) { content }
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
