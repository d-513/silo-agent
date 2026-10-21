import Observation
import SiloClient
import SwiftUI

/// The signed-in app. It opens on a chat, as the web does: the Bot's sidebar (its pages, chats,
/// Customize and Settings) slides in from the left edge on a phone and stays as a column on an
/// iPad; the machine (Files, Changes, Mail, Start/Stop) is a sheet off the status lamp.
struct RootView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var creating = false

    var body: some View {
        Group {
            if model.bots.isEmpty {
                NoBots(creating: $creating)
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
        .overlay(alignment: .top) { ErrorBanner() }
        .sheet(isPresented: $creating) { NewBotView() }
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
                    case .customize: SectionPage(title: "Customize", tabs: [.connectors, .skills, .drives, .channels])
                    case .settings: SectionPage(title: "Settings", tabs: [.settings, .rules, .secrets, .tunnels, .container, .audit])
                    }
                }
        }
        .id(model.selectedBotID)
        .sheet(isPresented: $model.machineOpen) { MachineSheet() }
    }
}

/// No Bot yet: the one thing to do is make one.
private struct NoBots: View {
    @Environment(AppModel.self) private var model
    @Binding var creating: Bool
    @State private var account = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Spacer()
            Text("No Bots yet").font(.silo(30, .medium))
            Text("A Bot is its own machine. It does not share files with the others.")
                .font(.silo(15))
                .foregroundStyle(Theme.ink2)
            Button { creating = true } label: { Text("New Bot") }
                .buttonStyle(.ink)
                .padding(.top, 6)
            if let error = model.botsError {
                Text(error).font(.silo(13)).foregroundStyle(Theme.vermilion)
            }
            Spacer()
            Button { account = true } label: {
                Text(model.user?.email ?? "Account").font(.silo(13)).foregroundStyle(Theme.ink3)
            }
            .buttonStyle(.plain)
        }
        .padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .background(Theme.canvas.ignoresSafeArea())
        .refreshable { await model.refreshBots() }
        .sheet(isPresented: $account) { AccountView(modal: true) }
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
                    Text("Desktop and Console stay on the web client.")
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
        case .drives: DrivesPage()
        case .connectors: ConnectorsPage()
        case .channels: ChannelsPage()
        case .skills: SkillsPage()
        case .secrets: SecretsPage()
        case .rules: RulesPage()
        case .container: ContainersPage()
        case .settings: BotSettingsPage()
        case .chat, .desktop: PlaceholderPane(tab: tab)
        }
    }
}
