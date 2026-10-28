import SiloClient
import SwiftUI

/// The Workspace page: its overview of the Bots the user is on (those that need you or are at work
/// first, each live conversation and its tool, else when it last worked and what it will do next),
/// then a tab for everything that is the Workspace's, each shown only to the roles it is for.
struct WorkspacePage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var creating = false
    @State private var inviting = false

    private var space: Silo_V1_Space? { spaces.current }
    private var role: String { spaces.role }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                head
                tabs.padding(.top, 18)
                if let error = model.botsError {
                    Text(error).font(.silo(13)).foregroundStyle(Theme.vermilion).padding(.top, 14)
                }
                if space == nil && !spaces.loaded {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 48)
                } else if spaces.bots.isEmpty {
                    empty
                } else {
                    overview
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 8)
            .padding(.bottom, 40)
        }
        .scrollIndicators(.hidden)
        .background(Theme.canvas.ignoresSafeArea())
        .navigationTitle(space?.name ?? "Workspace")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .principal) { Text("") } }
        .refreshable {
            await model.refreshBots()
            await spaces.load()
        }
        .task { await spaces.load() }
        .sheet(isPresented: $creating) { NewBotView() }
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
                        .lineLimit(1)
                    if let space {
                        Text([plural(Int(space.bots), "Bot", "Bots"), plural(Int(space.people), "person", "people"), spaceRoleWord(role)]
                            .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.silo(13))
                            .foregroundStyle(Theme.ink3)
                    }
                }
            }
            if canBuildSpace(role) || canAdminSpace(role) {
                HStack(spacing: 8) {
                    if canBuildSpace(role) {
                        Button { creating = true } label: { Label("New Bot", systemImage: "plus") }
                            .buttonStyle(.ink).controlSize(.small)
                    }
                    if canAdminSpace(role) {
                        Button { inviting = true } label: { Label("Invite people", systemImage: "person.badge.plus") }
                            .buttonStyle(.quiet).controlSize(.small)
                    }
                }
            }
        }
    }

    /// The Workspace's tabs as chips; each opens its page.
    private var tabs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(SpaceTab.visible(role: role)) { tab in
                    NavigationLink(value: BotRoute.space(tab)) {
                        Label(tab.title, systemImage: tab.symbol)
                            .font(.silo(13, .medium))
                            .foregroundStyle(Theme.ink2)
                            .padding(.horizontal, 11)
                            .frame(height: 34)
                            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.line))
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .scrollClipDisabled()
    }

    // MARK: overview

    @ViewBuilder
    private var overview: some View {
        let bots = spaces.bots
        let mood = { (b: Silo_V1_Bot) in botMood(status: b.status, asks: Int(b.dashboardAsks), live: b.activity.count) }
        let groups = overviewGroups(bots, mood: mood)
        let pulse = overviewPulse(moods: bots.map(mood), asks: bots.reduce(0) { $0 + Int($1.dashboardAsks) })
        if !pulse.isEmpty {
            FlowLine(parts: pulse)
                .padding(.top, 20)
        }
        if !groups.now.isEmpty {
            group(title: "Now", groups.now)
        }
        if !groups.resting.isEmpty {
            group(title: groups.now.isEmpty ? "Bots" : "Resting", groups.resting)
        }
    }

    private func group(title: String, _ bots: [Silo_V1_Bot]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title.uppercased())
                .font(.silo(11, .medium))
                .tracking(0.9)
                .foregroundStyle(Theme.ink3)
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 300), spacing: 12)], spacing: 12) {
                ForEach(bots, id: \.id) { OverviewCard(bot: $0) }
            }
        }
        .padding(.top, 24)
    }

    private var empty: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("No Bots yet").font(.silo(30, .medium))
            Text(canBuildSpace(role)
                 ? "A Bot is its own machine. It does not share files with the others."
                 : "Nobody has given you a Bot in this workspace yet.")
                .font(.silo(15))
                .foregroundStyle(Theme.ink2)
            if canBuildSpace(role) {
                Button { creating = true } label: { Text("New Bot") }
                    .buttonStyle(.ink)
                    .padding(.top, 6)
            }
        }
        .padding(.top, 48)
    }
}

private func plural(_ n: Int, _ one: String, _ many: String) -> String { "\(n) \(n == 1 ? one : many)" }

/// The pulse: a lamp and count per state that has any, on a `well` band.
private struct FlowLine: View {
    let parts: [(mood: BotMood, words: String)]

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 16) { items }
            VStack(alignment: .leading, spacing: 6) { items }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
    }

    private var items: some View {
        ForEach(parts, id: \.words) { part in
            HStack(spacing: 6) {
                if part.mood == .asks {
                    Image(systemName: "questionmark.bubble").font(.system(size: 12))
                } else {
                    StatusDot(status: status(part.mood), size: 7)
                }
                Text(part.words).font(.silo(13, .medium))
            }
            .foregroundStyle(part.mood == .needsYou ? Theme.vermilion : part.mood == .asks ? Theme.cobalt : Theme.ink2)
        }
    }

    private func status(_ mood: BotMood) -> String {
        switch mood {
        case .needsYou: return "needs_you"
        case .working: return "working"
        case .online: return "online"
        default: return "stopped"
        }
    }
}

/// One Bot on the overview: who it is, what it is doing (or did last, and will do next), and what
/// waits for you there. The card opens the Bot; a live conversation on it opens that chat.
private struct OverviewCard: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    private var mood: BotMood { botMood(status: bot.status, asks: Int(bot.dashboardAsks), live: bot.activity.count) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 12) {
                CrestView(index: bot.crest, size: 44)
                    .opacity(mood == .stopped ? 0.7 : 1)
                VStack(alignment: .leading, spacing: 4) {
                    Text(bot.name).font(.silo(16, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                    StatusLine(status: bot.status)
                }
                Spacer(minLength: 0)
                if bot.people.contains(where: { !$0.you }) {
                    HStack(spacing: -6) {
                        ForEach(bot.people.prefix(3), id: \.userID) { Initials(email: $0.email, size: 22) }
                    }
                }
            }
            if !bot.description_p.isEmpty {
                Text(bot.description_p)
                    .font(.silo(13))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(2)
                    .padding(.top, 10)
            }
            doing.padding(.top, 14)
            waiting
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(alignment: .leading) {
            if mood == .needsYou { Theme.vermilion.frame(width: 2).padding(.vertical, 14) }
        }
        .shadow(color: .black.opacity(0.05), radius: 8, y: 2)
        .contentShape(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .onTapGesture { open(chat: nil) }
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isButton)
        .accessibilityLabel(bot.name)
    }

    @ViewBuilder
    private var doing: some View {
        let runs = bot.activity.filter { $0.kind != "subagent" }
        let agents = bot.activity.count - runs.count
        if !runs.isEmpty || agents > 0 {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(runs.prefix(2), id: \.chatID) { run in
                    Button { open(chat: run.chatID) } label: {
                        HStack(spacing: 8) {
                            StatusDot(status: "working", size: 7)
                            Text(activitySource(kind: run.kind, title: run.title))
                                .font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                            Spacer(minLength: 4)
                            if !run.step.isEmpty {
                                Text(activityStepApp(run.step))
                                    .font(.siloMono(11)).foregroundStyle(Theme.ink3)
                                    .padding(.horizontal, 6).padding(.vertical, 1)
                                    .background(Theme.well, in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                            }
                        }
                        .frame(height: 30)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
                if runs.count > 2 {
                    Text("…and \(runs.count - 2) more").font(.silo(12)).foregroundStyle(Theme.ink3)
                }
                if agents > 0 {
                    Text(agents == 1 ? "1 subagent at work" : "\(agents) subagents at work").font(.silo(12)).foregroundStyle(Theme.ink3)
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 4) {
                Text(bot.lastActiveAt.isEmpty ? "Has not worked yet" : "Last active \(relativeTime(bot.lastActiveAt))")
                if !bot.nextRunAt.isEmpty {
                    Label("\(bot.nextRunName) · \(nextRunWhen(bot.nextRunAt))", systemImage: "timer")
                        .lineLimit(1)
                }
            }
            .font(.silo(12.5))
            .foregroundStyle(Theme.ink3)
        }
    }

    @ViewBuilder
    private var waiting: some View {
        if bot.dashboardAsks > 0 || bot.feedUnread > 0 {
            VStack(alignment: .leading, spacing: 0) {
                Theme.line.frame(height: 1).padding(.vertical, 10)
                HStack(spacing: 14) {
                    if bot.dashboardAsks > 0 {
                        Label(bot.dashboardAsks == 1 ? "1 question" : "\(bot.dashboardAsks) questions", systemImage: "questionmark.bubble")
                    }
                    if bot.feedUnread > 0 {
                        Label("\(bot.feedUnread) new in Feed", systemImage: "tray")
                    }
                }
                .font(.silo(12.5, .medium))
                .foregroundStyle(Theme.cobalt)
            }
        }
    }

    /// Opens the Bot, at a chat when one is named. Another Bot's chat is opened through the
    /// "last chat" it remembers, which is where `chooseBot` lands.
    private func open(chat: String?) {
        let target = chat.flatMap { id in bot.activity.first { $0.chatID == id }.map { $0.kind == "subagent" && !$0.leadChatID.isEmpty ? $0.leadChatID : id } }
        if bot.id == model.selectedBotID {
            if let target { model.showChat(target) } else { model.path = [] }
            model.sidebarOpen = false
            return
        }
        if let target { UserDefaults.standard.set(target, forKey: "lastChat.\(bot.id)") }
        model.selectedBotID = bot.id
    }
}

/// "Tue, Oct 30 09:00": when an automation fires next.
func nextRunWhen(_ iso: String) -> String {
    guard let date = parseDate(iso) else { return "" }
    return date.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day().hour().minute())
}

/// One tab of the Workspace page.
struct SpaceTabPage: View {
    let tab: SpaceTab

    var body: some View {
        content
            .scrollContentBackground(.hidden)
            .background(Theme.canvas.ignoresSafeArea())
            .navigationTitle(tab.title)
            .navigationBarTitleDisplayMode(.large)
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
