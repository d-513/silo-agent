import SiloClient
import SwiftUI

/// The Workspace page's body, set inside a `SiloPage` by whoever shows it: its overview of the Bots
/// the user is on (those that need you or are at work first, each live conversation and what it is
/// on, else when it last worked and what it will do next), then a tab for everything that is the
/// Workspace's, each shown only to the roles it is for.
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
            } else {
                overview
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

    /// The Workspace's tabs as chips; each opens its page.
    private var tabs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(SpaceTab.visible(role: role)) { tab in
                    let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    NavigationLink(value: BotRoute.space(tab)) {
                        HStack(spacing: 6) {
                            Image(systemName: tab.symbol).font(.system(size: 13))
                            Text(tab.title).font(.silo(13, .medium))
                        }
                        .foregroundStyle(Theme.ink2)
                        .padding(.horizontal, 11)
                        .frame(height: 34)
                        .background(Theme.surface, in: shape)
                        .overlay(shape.stroke(Theme.line))
                    }
                    .buttonStyle(.press)
                }
            }
        }
        .scrollClipDisabled()
    }

    // MARK: overview

    @ViewBuilder
    private var overview: some View {
        let bots = spaces.bots
        let mood = { (b: Silo_V1_Bot) in botMood(status: b.status, asks: Int(b.dashboardAsks)) }
        let groups = overviewGroups(bots, mood: mood)
        let pulse = overviewPulse(moods: bots.map(mood), asks: bots.reduce(0) { $0 + Int($1.dashboardAsks) })
        if !pulse.isEmpty {
            PulseBand(parts: pulse).padding(.top, 20)
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
            SectionLabel(text: title)
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 300), spacing: 12, alignment: .top)], spacing: 12) {
                ForEach(bots, id: \.id) { OverviewCard(bot: $0).rise() }
            }
        }
        .padding(.top, 24)
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

/// The pulse: a lamp and count per state that has any, on a `well` band. Resting has no lamp, and
/// the questions waiting are in cobalt with their glyph.
private struct PulseBand: View {
    let parts: [(mood: BotMood, words: String)]

    var body: some View {
        FlowRow(spacing: 12) {
            ForEach(parts, id: \.words) { part in
                HStack(spacing: 7) {
                    switch part.mood {
                    case .asks: Image(systemName: "questionmark.bubble").font(.system(size: 12))
                    case .needsYou: Lamp(status: .needsYou)
                    case .working: Lamp(status: .working)
                    case .resting: EmptyView()
                    }
                    Text(part.words).font(.silo(13, .medium))
                }
                .foregroundStyle(part.mood == .needsYou ? Theme.vermilion : part.mood == .asks ? Theme.cobalt : Theme.ink2)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
    }
}

/// One Bot on the overview: who it is, what it is doing (or what it will do next, the status line
/// saying when it last worked), and what waits for you there. The card opens the Bot; a live
/// conversation on it opens that chat, and what waits opens the Dashboard or the Feed.
private struct OverviewCard: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    private var mood: BotMood { botMood(status: bot.status, asks: Int(bot.dashboardAsks)) }

    var body: some View {
        PressCard(label: bot.name, ribbon: mood == .needsYou, action: { open(chat: nil) }) {
            VStack(alignment: .leading, spacing: 0) {
                who.allowsHitTesting(false)
                doing
                waiting
            }
        }
    }

    private var who: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 14) {
                CrestView(index: bot.crest, size: 44)
                VStack(alignment: .leading, spacing: 4) {
                    Text(bot.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                    StatusWord(look: botLook(bot))
                }
                .padding(.top, 2)
                Spacer(minLength: 0)
                if bot.people.contains(where: { !$0.you }) { people }
            }
            if !bot.description_p.isEmpty {
                Text(bot.description_p)
                    .font(.silo(13))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(2)
                    .padding(.top, 12)
            }
        }
    }

    private var people: some View {
        HStack(spacing: -6) {
            ForEach(bot.people.prefix(3), id: \.userID) { person in
                Initials(email: person.email, size: 22)
                    .background(Circle().fill(Theme.surface).padding(-2))
            }
            if bot.people.count > 3 {
                Text("+\(bot.people.count - 3)")
                    .font(.silo(11))
                    .foregroundStyle(Theme.ink3)
                    .padding(.leading, 10)
            }
        }
    }

    /// Each live conversation and what it is on, else what it will do next on its own.
    @ViewBuilder
    private var doing: some View {
        let runs = bot.activity.filter { $0.kind != "subagent" }
        let agents = bot.activity.count - runs.count
        if !runs.isEmpty || agents > 0 {
            VStack(alignment: .leading, spacing: 1) {
                ForEach(Array(runs.prefix(2).enumerated()), id: \.offset) { _, run in
                    RunRow(row: run) { open(chat: run.chatID) }
                }
                if runs.count > 2 {
                    Text("…and \(runs.count - 2) more")
                        .font(.silo(12))
                        .foregroundStyle(Theme.ink3)
                        .padding(.horizontal, 8)
                        .allowsHitTesting(false)
                }
                if agents > 0 {
                    Text(agents == 1 ? "1 subagent at work" : "\(agents) subagents at work")
                        .font(.silo(12))
                        .foregroundStyle(Theme.ink3)
                        .padding(.horizontal, 8)
                        .frame(height: 28)
                        .allowsHitTesting(false)
                }
            }
            .padding(.horizontal, -8)
            .padding(.top, 12)
        } else if !bot.nextRunAt.isEmpty {
            HStack(spacing: 6) {
                Image(systemName: "timer").font(.system(size: 12))
                Text("\(bot.nextRunName) · \(nextRunWhen(bot.nextRunAt))").lineLimit(1)
            }
            .font(.silo(12.5))
            .foregroundStyle(Theme.ink3)
            .padding(.top, 14)
            .allowsHitTesting(false)
        }
    }

    /// What the Bot left for you: its questions on the Dashboard, new posts in the Feed.
    @ViewBuilder
    private var waiting: some View {
        if bot.dashboardAsks > 0 || bot.feedUnread > 0 {
            VStack(alignment: .leading, spacing: 4) {
                Theme.line.frame(height: 1).allowsHitTesting(false)
                FlowRow(spacing: 4) {
                    if bot.dashboardAsks > 0 {
                        waits(bot.dashboardAsks == 1 ? "1 question" : "\(bot.dashboardAsks) questions", "questionmark.bubble", .dashboard)
                    }
                    if bot.feedUnread > 0 {
                        waits("\(bot.feedUnread) new in Feed", "tray", .feed)
                    }
                }
            }
            .padding(.horizontal, -8)
            .padding(.top, 10)
        }
    }

    private func waits(_ words: String, _ symbol: String, _ page: BotTab) -> some View {
        Button { open(page: page) } label: {
            HStack(spacing: 6) {
                Image(systemName: symbol).font(.system(size: 12))
                Text(words).font(.silo(12.5, .medium))
            }
            .foregroundStyle(Theme.cobalt)
            .padding(.horizontal, 8)
            .frame(height: 32)
            .contentShape(Rectangle())
        }
        .buttonStyle(RowPressStyle())
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
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

    /// Opens one of the Bot's pages. Choosing another Bot clears the path, so the page is pushed
    /// once it has been chosen.
    private func open(page: BotTab) {
        model.sidebarOpen = false
        guard bot.id != model.selectedBotID else {
            model.path = [.page(page)]
            return
        }
        model.selectedBotID = bot.id
        Task {
            await model.chooseBot(bot.id)
            model.path = [.page(page)]
        }
    }
}

/// One live conversation on a card: lamp, where it runs, what it is on, its place on the taskboard.
/// A conversation the reader may not open is only a line, and a press on it opens the Bot.
private struct RunRow: View {
    let row: Silo_V1_BotActivity
    let open: () -> Void

    var body: some View {
        if row.chatID.isEmpty {
            line.allowsHitTesting(false)
        } else {
            Button(action: open) { line }
                .buttonStyle(RowPressStyle())
                .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
        }
    }

    private var line: some View {
        let look = runLook(Doing(row))
        let step = progress(row)
        return HStack(spacing: 8) {
            Lamp(status: look.lamp)
            // Their own stack, so the two share what is left: the shorter keeps all its words.
            HStack(spacing: 8) {
                Text(activitySource(kind: row.kind, title: row.title))
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(1)
                    .frame(minWidth: 48, alignment: .leading)
                Text(look.word)
                    .font(.silo(12.5))
                    .foregroundStyle(look.lamp == .working ? Theme.ink3 : Theme.color(look.tone))
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !step.isEmpty {
                Text(step).font(.siloMono(11.5)).monospacedDigit().foregroundStyle(Theme.ink3).fixedSize()
            }
        }
        .padding(.horizontal, 8)
        .frame(height: 32)
        .contentShape(Rectangle())
    }
}

/// "Tue, Oct 30 09:00": when an automation fires next.
func nextRunWhen(_ iso: String) -> String {
    guard let date = parseDate(iso) else { return "" }
    return date.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day().hour().minute())
}

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
