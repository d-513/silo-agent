import SiloClient
import SwiftUI

/// The Workspace page's body, set inside a `SiloPage` by whoever shows it: its head, then a tab for
/// everything that is the Workspace's, each shown only to the roles it is for. Its Bots are the
/// sidebar's list; with none of the user's, the page says so and offers New Bot.
struct WorkspacePage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var creating = false
    @State private var inviting = false

    private var space: Silo_V1_Space? { spaces.current }
    private var role: String { spaces.role }

    var body: some View {
        PageScroll(spacing: 0) {
            head
            tabs.padding(.top, 18)
            if let error = model.botsError {
                ErrorLine(text: error).padding(.top, 14)
            }
            if space == nil && !spaces.loaded {
                SkeletonRows(count: 3, height: 148).padding(.top, 24)
            } else if spaces.bots.isEmpty {
                empty
            }
        }
        .scrollIndicators(.hidden)
        .refreshable {
            await model.refreshBots()
            await spaces.load()
        }
        .task { await spaces.load() }
        .sheet(isPresented: $creating) { NewBotView().siloSheet() }
        .sheet(isPresented: $inviting) { if let space { SpaceInviteSheet(space: space) } }
    }

    // MARK: head

    private var head: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 14) {
                SpaceMarkView(mark: space?.mark ?? "", size: 52)
                VStack(alignment: .leading, spacing: 2) {
                    Text(space?.name ?? "Workspace")
                        .font(.silo(26, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    if let space {
                        Text([plural(Int(space.bots), "Bot", "Bots"), plural(Int(space.people), "person", "people"), spaceRoleWord(role)]
                            .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.silo(13))
                            .foregroundStyle(Theme.ink3)
                            .lineLimit(1)
                    }
                }
            }
            if canBuildSpace(role) || canAdminSpace(role) {
                HStack(spacing: 8) {
                    if canBuildSpace(role) {
                        Button { creating = true } label: { headButton("New Bot", "plus") }
                            .buttonStyle(.ink)
                            .controlSize(.small)
                    }
                    if canAdminSpace(role) {
                        Button { inviting = true } label: { headButton("Invite people", "person.badge.plus") }
                            .buttonStyle(.quiet)
                            .controlSize(.small)
                    }
                }
            }
        }
    }

    private func headButton(_ title: String, _ symbol: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).font(.system(size: 12, weight: .medium))
            Text(title)
        }
    }

    /// The Workspace's tabs as rows, as Settings lists a Bot's; each opens its page.
    private var tabs: some View {
        Panel {
            ForEach(SpaceTab.visible(role: role)) { tab in
                PanelRow(symbol: tab.symbol, title: tab.title, chevron: true) {
                    model.spacePath.append(.space(tab))
                }
            }
        }
    }

    private var empty: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("No Bots yet").font(.silo(30, .medium)).foregroundStyle(Theme.ink)
            Text(canBuildSpace(role)
                 ? "A Bot is its own machine. It does not share files with the others."
                 : "Nobody has given you a Bot in this workspace yet.")
                .font(.silo(15))
                .foregroundStyle(Theme.ink2)
            if canBuildSpace(role) {
                Button { creating = true } label: { headButton("New Bot", "plus") }
                    .buttonStyle(.ink)
                    .padding(.top, 6)
            }
        }
        .padding(.top, 48)
        .rise()
    }
}

private func plural(_ n: Int, _ one: String, _ many: String) -> String { "\(n) \(n == 1 ? one : many)" }

/// One tab of the Workspace page, in the page frame; each tab puts its actions in the head.
struct SpaceTabPage: View {
    let tab: SpaceTab

    var body: some View {
        SiloPage(title: tab.title) { content }
    }

    @ViewBuilder
    private var content: some View {
        switch tab {
        case .connectors: SpaceConnectorsPage()
        case .skills: SpaceSkillsPage()
        case .drives: SpaceDrivesPage()
        case .people: SpacePeoplePage()
        case .rules: SpaceRulesPage()
        case .activity: SpaceActivityPage()
        case .settings: SpaceSettingsPage()
        }
    }
}
