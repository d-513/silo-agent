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
        Group {
            if model.selectedBot == nil {
                SpaceHome()
            } else if sizeClass == .regular {
                NavigationSplitView {
                    BotSidebar()
                        .toolbar(.hidden, for: .navigationBar)
                } detail: {
                    BotStack()
                }
            } else {
                Drawer()
            }
        }
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
            .animation(.snappy(duration: 0.32), value: model.sidebarOpen)
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
                    case .workspace: WorkspacePage()
                    case .space(let tab): SpaceTabPage(tab: tab)
                    }
                }
        }
        .id(model.selectedBotID)
        .sheet(isPresented: $model.machineOpen) { MachineSheet() }
    }
}

/// No Bot open: the current Workspace's page at the root, with the Workspace switcher and the
/// account in its bar. A new Workspace, or one with none of the user's Bots, lands here.
private struct SpaceHome: View {
    @Environment(AppModel.self) private var model
    @State private var path: [BotRoute] = []
    @State private var account = false
    @State private var newSpace = false

    var body: some View {
        NavigationStack(path: $path) {
            WorkspacePage()
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        SpaceSwitcher(newSpace: { newSpace = true }) {
                            HStack(spacing: 4) {
                                Text("Workspaces").font(.silo(15, .medium))
                                Image(systemName: "chevron.down").font(.system(size: 11, weight: .semibold))
                            }
                            .foregroundStyle(Theme.ink)
                        }
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button { account = true } label: {
                            Image(systemName: "person.crop.circle").foregroundStyle(Theme.ink2)
                        }
                        .accessibilityLabel(model.user?.email ?? "Account")
                    }
                }
                .navigationDestination(for: BotRoute.self) { route in
                    if case .space(let tab) = route { SpaceTabPage(tab: tab) } else { WorkspacePage() }
                }
        }
        .sheet(isPresented: $account) { AccountView(modal: true) }
        .sheet(isPresented: $newSpace) { NewSpaceSheet() }
    }
}

/// The Workspace switcher: the user's Workspaces (the current one checked, with their role), then
/// New workspace. Picking one opens its first Bot, or its page when it has none.
struct SpaceSwitcher<Content: View>: View {
    @Environment(SpaceModel.self) private var spaces
    let newSpace: () -> Void
    @ViewBuilder let label: () -> Content

    var body: some View {
        Menu {
            Picker("Workspaces", selection: Binding(get: { spaces.currentID ?? "" }, set: { spaces.open($0) })) {
                ForEach(spaces.spaces) { space in
                    let role = spaceRoleWord(space.role)
                    Text(role.isEmpty ? space.name : "\(space.name) · \(role)").tag(space.id)
                }
            }
            .pickerStyle(.inline)
            Divider()
            Button(action: newSpace) { Label("New workspace", systemImage: "plus") }
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

/// The Customize or Settings list: one row per page, as the web's section tabs.
struct SectionPage: View {
    let title: String
    let tabs: [BotTab]

    var body: some View {
        List {
            Section {
                ForEach(tabs) { tab in
                    NavigationLink(value: BotRoute.page(tab)) {
                        Label {
                            Text(tab == .settings ? "General" : tab.title).font(.silo(15))
                        } icon: {
                            Image(systemName: tab.symbol).foregroundStyle(Theme.ink2)
                        }
                    }
                }
            } footer: {
                if title == "Settings" {
                    Text("Desktop and Console stay on the web client. Connectors, skills and drives are the workspace's: they are on the Workspace page.")
                }
            }
        }
        .siloList()
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.large)
    }
}

/// A pushed Bot page with its title; each page supplies its own toolbar items.
struct BotPage: View {
    let tab: BotTab

    var body: some View {
        content
            // Paper behind every list and form on the page; rows stay surface.
            .scrollContentBackground(.hidden)
            .background(Theme.canvas.ignoresSafeArea())
            .navigationTitle(tab == .settings ? "General" : tab.title)
            .navigationBarTitleDisplayMode(.large)
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
