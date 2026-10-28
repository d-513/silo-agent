import Observation
import SiloClient
import SwiftUI

/// The web sidebar on a phone: the Workspace tile and its Bots' crests, the Bot's name and status,
/// New chat, the pages beside the chat, the chats in date groups, and Channels, the Workspace,
/// Settings and the account at the foot.
struct BotSidebar: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var creating = false
    @State private var account = false
    @State private var newSpace = false

    var body: some View {
        VStack(spacing: 0) {
            crests
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
                    if let bot = model.selectedBot {
                        header(bot)
                        ActivityStrip(bot: bot)
                    }
                    MainChatRow()
                        .padding(.bottom, 8)
                    pages
                    chats
                }
                .padding(.bottom, 12)
            }
            .scrollIndicators(.hidden)
            foot
        }
        .background(Theme.well.ignoresSafeArea())
        .sheet(isPresented: $creating) { NewBotView() }
        .sheet(isPresented: $account) { AccountView(modal: true) }
        .sheet(isPresented: $newSpace) { NewSpaceSheet() }
    }

    // MARK: crests

    private var crests: some View {
        @Bindable var model = model
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 10) {
                SpaceSwitcher(newSpace: { newSpace = true }) {
                    SpaceMarkView(mark: spaces.current?.mark ?? "", size: 40)
                }
                Theme.line.frame(width: 1, height: 24)
                ForEach(spaces.bots, id: \.id) { bot in
                    Button {
                        if bot.id == model.selectedBotID { model.newChat() } else { model.selectedBotID = bot.id }
                    } label: {
                        CrestView(index: bot.crest, size: 26)
                            .frame(width: 40, height: 40)
                            .background(Theme.surface, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
                            .overlay {
                                RoundedRectangle(cornerRadius: 11, style: .continuous)
                                    .stroke(bot.id == model.selectedBotID ? Theme.cobalt : Theme.line,
                                            lineWidth: bot.id == model.selectedBotID ? 2 : 1)
                            }
                            .overlay(alignment: .topTrailing) {
                                if bot.status == "needs_you" { StatusDot(status: bot.status, size: 8).offset(x: 2, y: -2) }
                            }
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(bot.name)
                }
                if canBuildSpace(spaces.role) {
                    Button { creating = true } label: {
                        Image(systemName: "plus")
                            .font(.system(size: 14, weight: .medium))
                            .foregroundStyle(Theme.ink3)
                            .frame(width: 40, height: 40)
                            .overlay(RoundedRectangle(cornerRadius: 11, style: .continuous).stroke(Theme.line, style: StrokeStyle(lineWidth: 1, dash: [3, 3])))
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("New Bot")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    private func header(_ bot: Silo_V1_Bot) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(bot.name).font(.silo(17, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                Spacer(minLength: 0)
                if bot.people.count > 1 { PeopleFaces(people: bot.people) }
            }
            StatusLine(status: bot.status)
        }
        .padding(.horizontal, 16)
        .padding(.top, 14)
        .padding(.bottom, 10)
    }

    // MARK: pages

    @ViewBuilder
    private var pages: some View {
        let bot = model.selectedBot
        // Approvals are Needs you (a run is paused on them); the Bot's questions only wait.
        let asks = Int(bot?.dashboardAsks ?? 0)
        SideRow(symbol: BotTab.dashboard.symbol, title: "Dashboard", strong: true, on: on(.dashboard),
                trailing: !model.approvals.isEmpty ? AnyView(NeedsYouMark(count: model.approvals.count))
                    : asks > 0 ? AnyView(QuestionsMark(count: asks)) : nil) { open(.dashboard) }
        SideRow(symbol: BotTab.automations.symbol, title: "Automations", on: on(.automations)) { open(.automations) }
        SideRow(symbol: BotTab.artifacts.symbol, title: "Artifacts", on: on(.artifacts)) { open(.artifacts) }
        SideRow(symbol: BotTab.memories.symbol, title: "Memories", on: on(.memories)) { open(.memories) }
        SideRow(symbol: BotTab.knowledge.symbol, title: "Knowledge", on: on(.knowledge)) { open(.knowledge) }
        SideRow(symbol: BotTab.feed.symbol, title: "Feed", on: on(.feed),
                trailing: (bot?.feedUnread ?? 0) > 0 ? AnyView(UnreadCount(count: Int(bot?.feedUnread ?? 0))) : nil) { open(.feed) }
    }

    private func on(_ tab: BotTab) -> Bool { model.path.first == .page(tab) }

    private func open(_ tab: BotTab) { go(.page(tab)) }

    private func go(_ route: BotRoute) {
        model.path = [route]
        model.sidebarOpen = false
    }

    // MARK: chats

    /// Your own side chats by date (the main chat is above the pages); on a Bot several people use,
    /// its managers also get the Team: everyone else's chats and the Bot's own, read-only.
    @ViewBuilder
    private var chats: some View {
        let own = chatGroups(model.chats.filter { $0.mine && !$0.main }, updatedAt: { parseDate($0.updatedAt) })
        let others = model.chats.filter { !$0.mine }
        ForEach(own) { group in
            Section {
                ForEach(group.items, id: \.id) { chat in
                    SidebarChatRow(chat: chat, on: chat.id == model.selectedChatID && model.path.isEmpty)
                }
            } header: {
                groupHeader(group.title)
            }
        }
        if !others.isEmpty {
            Section {
                ForEach(others, id: \.id) { chat in
                    SidebarChatRow(chat: chat, on: chat.id == model.selectedChatID && model.path.isEmpty, team: true)
                }
            } header: {
                groupHeader("Team")
            }
        }
    }

    private func groupHeader(_ title: String) -> some View {
                Text(title.uppercased())
                    .font(.silo(11, .medium))
                    .tracking(0.9)
                    .foregroundStyle(Theme.ink3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.top, 14)
                    .padding(.bottom, 4)
                    .background(Theme.well)
    }

    // MARK: foot

    private var foot: some View {
        VStack(alignment: .leading, spacing: 0) {
            Theme.line.frame(height: 1)
            SideRow(symbol: BotTab.channels.symbol, title: "Channels", on: model.path.first == .page(.channels)) { go(.page(.channels)) }
            SideRow(symbol: BotTab.workspace.symbol, title: spaces.current?.name ?? "Workspace",
                    on: model.path.first == .workspace || model.path.first.map { if case .space = $0 { return true } else { return false } } == true) { go(.workspace) }
            SideRow(symbol: "gearshape", title: "Settings", on: model.path.first == .settings) { go(.settings) }
            Theme.line.frame(height: 1).padding(.top, 4)
            ProfileRow { account = true }
        }
    }
}

/// Who is signed in, at the foot: an initial in a well circle, the email, and the way to the
/// account (email, password, two-factor, sessions, sign out).
private struct ProfileRow: View {
    @Environment(AppModel.self) private var model
    let action: () -> Void

    private var email: String { model.user?.email ?? "" }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 11) {
                Text(email.first.map { String($0).uppercased() } ?? "?")
                    .font(.silo(13, .semibold))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 28, height: 28)
                    .background(Theme.ink2, in: Circle())
                VStack(alignment: .leading, spacing: 1) {
                    Text(email.isEmpty ? "Account" : email)
                        .font(.silo(14, .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Text(model.user?.admin == true ? "Administrator · Account" : "Account")
                        .font(.silo(12))
                        .foregroundStyle(Theme.ink3)
                }
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 14)
            .frame(height: 54)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Account, \(email)")
    }
}

/// One sidebar row: a 16pt glyph and a label; the open one is a surface fill with the cobalt bar.
struct SideRow: View {
    let symbol: String
    let title: String
    var strong = false
    var on = false
    var trailing: AnyView?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: symbol)
                    .font(.system(size: 15, weight: .regular))
                    .foregroundStyle(on || strong ? Theme.ink : Theme.ink3)
                    .frame(width: 20)
                Text(title)
                    .font(.silo(15, .medium))
                    .foregroundStyle(on || strong ? Theme.ink : Theme.ink2)
                    .lineLimit(1)
                Spacer(minLength: 0)
                if let trailing { trailing }
            }
            .padding(.horizontal, 16)
            .frame(height: 42)
            .background(on ? Theme.surface : .clear)
            .overlay(alignment: .leading) { if on { Theme.cobalt.frame(width: 2) } }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// The Needs you lamp and word, as on the web's Dashboard row: approvals a run is paused on.
private struct NeedsYouMark: View {
    let count: Int

    var body: some View {
        HStack(spacing: 5) {
            StatusDot(status: "needs_you", size: 7)
            Text(count == 1 ? "Needs you" : "\(count) need you").font(.silo(12, .medium)).foregroundStyle(Theme.vermilion)
        }
    }
}

/// The Bot's open questions: they only wait, so cobalt with the question glyph, not Needs you.
private struct QuestionsMark: View {
    let count: Int

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: "questionmark.bubble").font(.system(size: 11, weight: .medium))
            Text(count == 1 ? "1 question" : "\(count) questions").font(.silo(12, .medium))
        }
        .foregroundStyle(Theme.cobalt)
    }
}

/// The Bot's one standing conversation, where it opens and where replies land, with New chat as the
/// small plus beside it.
private struct MainChatRow: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        let main = model.mainChat
        let on = main != nil && main?.id == model.selectedChatID && model.path.isEmpty
        let working = model.selectedBot?.activity.contains { $0.kind == "main" } == true || (on && model.isRunning)
        HStack(spacing: 6) {
            Button { model.showMainChat() } label: {
                HStack(spacing: 10) {
                    Image(systemName: "bubble.left.fill")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(Theme.onInk)
                        .frame(width: 24, height: 24)
                        .background(Theme.ink, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    Text("Main chat").font(.silo(15, .medium)).foregroundStyle(Theme.ink)
                    Spacer(minLength: 0)
                    if on && model.chatApproval != nil {
                        StatusDot(status: "needs_you", size: 7)
                    } else if working {
                        StatusDot(status: "working", size: 7)
                    }
                }
                .padding(.horizontal, 10)
                .frame(height: 44)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    .stroke(on ? Theme.cobalt : Theme.line, lineWidth: on ? 1.5 : 1))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            Button { model.newChat() } label: {
                Image(systemName: "plus")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(Theme.ink)
                    .frame(width: 44, height: 44)
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                        .stroke(model.selectedChatID == nil && model.path.isEmpty ? Theme.cobalt : Theme.line))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("New chat")
        }
        .padding(.horizontal, 12)
    }
}

/// What the Bot is doing right now, under its name: each live conversation with the tool it is in,
/// a tap away; its subagents counted on one line. Shown only while something runs.
private struct ActivityStrip: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    var body: some View {
        let runs = bot.activity.filter { $0.kind != "subagent" }
        let agents = bot.activity.filter { $0.kind == "subagent" }
        if !runs.isEmpty || !agents.isEmpty {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(runs.prefix(3), id: \.chatID) { activity in
                    row(where_(activity), step: activity.step) { open(activity.chatID) }
                }
                if let lead = agents.first {
                    row(agents.count == 1 ? "1 subagent at work" : "\(agents.count) subagents at work", step: "") { open(lead.leadChatID) }
                }
            }
            .padding(.vertical, 3)
            .background(Theme.surface.opacity(0.6), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .padding(.horizontal, 12)
            .padding(.bottom, 10)
        }
    }

    private func row(_ title: String, step: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 8) {
                StatusDot(status: "working", size: 7)
                Text(title).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                Spacer(minLength: 4)
                if !step.isEmpty { Text(toolApp(step)).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1) }
            }
            .padding(.horizontal, 10)
            .frame(height: 30)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// Where a live run is: the main chat, an automation, a channel, or a chat's title.
    private func where_(_ a: Silo_V1_BotActivity) -> String {
        switch a.kind {
        case "main": "Main chat"
        case "automation": "Automation “\(a.title)”"
        case "channel": "Channel “\(a.title)”"
        default: a.title.isEmpty ? "New chat" : a.title
        }
    }

    /// A chat this person can open; an automation's or a channel's log is not in the chat list.
    private func open(_ chatID: String) {
        if model.chats.contains(where: { $0.id == chatID }) { model.showChat(chatID) }
    }
}

/// The faces of a Bot several people use: up to three, then +N.
private struct PeopleFaces: View {
    let people: [Silo_V1_BotPerson]

    var body: some View {
        HStack(spacing: -6) {
            ForEach(people.prefix(3), id: \.userID) { Initials(email: $0.email, size: 20) }
            if people.count > 3 {
                Text("+\(people.count - 3)").font(.silo(11)).foregroundStyle(Theme.ink3).padding(.leading, 9)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(people.map { "\($0.email), \($0.role == "manage" ? "can manage" : "can use")" }.joined(separator: "; "))
    }
}

private struct UnreadCount: View {
    let count: Int

    var body: some View {
        Text(count > 99 ? "99+" : "\(count)")
            .font(.siloMono(11, .medium))
            .foregroundStyle(Theme.cobalt)
    }
}

/// A chat in the sidebar: tap to open, long-press to rename or delete.
struct SidebarChatRow: View {
    @Environment(AppModel.self) private var model
    let chat: Silo_V1_Chat
    let on: Bool
    /// A row of the Team group: someone else's chat, or the Bot's own. It reads only.
    var team = false
    @State private var renaming = false
    @State private var renameText = ""
    @State private var deleting = false

    var body: some View {
        Button { model.showChat(chat.id) } label: {
            HStack(spacing: 8) {
                if team {
                    if chat.author.isEmpty {
                        Image(systemName: "cpu").font(.system(size: 12)).foregroundStyle(Theme.ink3)
                            .accessibilityLabel("The Bot's own")
                    } else {
                        Initials(email: chat.author, size: 18)
                    }
                }
                Text(chat.title.isEmpty ? "New chat" : chat.title)
                    .font(.silo(15))
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
            }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 16)
                .frame(height: 40)
                .background(on ? Theme.surface : .clear)
                .overlay(alignment: .leading) { if on { Theme.cobalt.frame(width: 2) } }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .contextMenu {
            if !team {
                Button { begin() } label: { Label("Rename", systemImage: "pencil") }
                Button(role: .destructive) { deleting = true } label: { Label("Delete", systemImage: "trash") }
            }
        }
        .alert("Rename chat", isPresented: $renaming) {
            TextField("Title", text: $renameText)
            Button("Cancel", role: .cancel) {}
            Button("Rename") { Task { await model.renameChat(chat.id, title: renameText) } }
        }
        .confirmationDialog("Delete this chat?", isPresented: $deleting, titleVisibility: .visible) {
            Button("Delete chat", role: .destructive) { Task { await model.deleteChat(chat.id) } }
        } message: {
            Text("The conversation and its history are removed.")
        }
    }

    private func begin() {
        renameText = chat.title
        renaming = true
    }
}
