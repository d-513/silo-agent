import Observation
import SiloClient
import SwiftUI

/// Top-level sections: Bots, the Skill Hub, and the account. Everything about one Bot lives inside
/// the Bots tab as a navigation stack (Bot hub → chats and pages), which is how Apple's own apps
/// with deep content work: a tab bar for sections, push navigation within one.
struct RootView: View {
    var body: some View {
        TabView {
            Tab("Bots", systemImage: "shippingbox") { BotsRoot() }
            Tab("Skills", systemImage: "book") { SkillHub(embedded: true) }
            Tab("Account", systemImage: "person.crop.circle") { AccountView() }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
    }
}

/// Bots in the sidebar (a plain list on iPhone), the selected Bot's stack in the detail column.
struct BotsRoot: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        NavigationSplitView {
            BotsList()
        } detail: {
            if model.selectedBot != nil {
                BotStack()
            } else {
                ContentUnavailableView(
                    "No Bot selected",
                    systemImage: "shippingbox",
                    description: Text("Pick a Bot to chat with it or manage its machine.")
                )
            }
        }
        .onChange(of: model.selectedBotID) { _, newValue in
            model.path = []
            Task { await model.chooseBot(newValue) }
        }
    }
}

/// One Bot's navigation stack. The approval sheet and error banner live here so they cover every
/// pushed page.
struct BotStack: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        NavigationStack(path: $model.path) {
            BotHub()
                .navigationDestination(for: BotRoute.self) { route in
                    switch route {
                    case .chat(let id): ChatScreen(chatID: id)
                    case .chats: ChatsPage()
                    case .page(let tab): BotPage(tab: tab)
                    }
                }
        }
        .id(model.selectedBotID)
        .sheet(item: pendingSheet) { approval in
            if let bot = model.selectedBot { ApprovalSheet(bot: bot, approval: approval) }
        }
        .overlay(alignment: .bottom) { ErrorBanner() }
    }

    /// The sheet shows the oldest pending approval until the reader swipes it away.
    private var pendingSheet: Binding<Silo_V1_Approval?> {
        Binding(
            get: {
                guard let first = model.approvals.first, first.id != model.dismissedApprovalID else { return nil }
                return first
            },
            set: { newValue in
                if newValue == nil { model.dismissedApprovalID = model.approvals.first?.id }
            }
        )
    }
}

/// A failed action, shown as a glass capsule above the tab bar; tap to dismiss.
struct ErrorBanner: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let message = model.errorMessage {
            Button {
                model.errorMessage = nil
            } label: {
                Label(message, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(Theme.vermilion)
                    .multilineTextAlignment(.leading)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .glassEffect(.regular, in: .rect(cornerRadius: 20))
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 16)
            .padding(.bottom, 72)
            .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }
}

/// A pushed Bot page with its title; each page supplies its own toolbar items.
struct BotPage: View {
    let tab: BotTab

    var body: some View {
        content
            .navigationTitle(tab.title)
            .navigationBarTitleDisplayMode(.large)
    }

    @ViewBuilder
    private var content: some View {
        switch tab {
        case .automations: AutomationsPage()
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
