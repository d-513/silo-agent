import Observation
import SiloClient
import SwiftUI

/// The web's sidebar on a phone: the Workspace and its Bots' crests, the Bot's face and state, the
/// home card (the main chat with its Call, and the Dashboard), work with no row of its own, the
/// pages beside the chat, the machine, the side chats folded into date groups, and Settings at the
/// foot. Web: `web/src/bot/BotSidebar.tsx`.
struct BotSidebar: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var creating = false
    @State private var account = false
    @State private var newSpace = false
    @AppStorage("foldedChatGroups") private var folded = ""

    var body: some View {
        VStack(spacing: 0) {
            crests
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
                    if let bot = model.selectedBot {
                        BotHead(bot: bot)
                        HomeCard(bot: bot, go: go)
                            .padding(.horizontal, 10)
                            .padding(.bottom, 10)
                        ActivityStrip(bot: bot)
                    }
                    pages
                    if let bot = model.selectedBot { MachineTiles(bot: bot, go: go) }
                    sideChatsHead
                    chats
                }
                .padding(.bottom, 12)
            }
            .scrollIndicators(.hidden)
            foot
        }
        .background(Theme.well.ignoresSafeArea())
        .sheet(isPresented: $creating) { NewBotView().siloSheet() }
        .sheet(isPresented: $account) { AccountView(modal: true).siloSheet() }
        .sheet(isPresented: $newSpace) { NewSpaceSheet().siloSheet() }
    }

    // MARK: crests

    private var crests: some View {
        @Bindable var model = model
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                SpaceSwitcher(newSpace: { newSpace = true }) {
                    SpaceMarkView(mark: spaces.current?.mark ?? "", size: 40)
                }
                Theme.lineStrong.frame(width: 1, height: 24).padding(.horizontal, 2)
                ForEach(spaces.bots, id: \.id) { bot in
                    let on = bot.id == model.selectedBotID
                    let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    Button {
                        if on { model.sidebarOpen = false } else { model.selectedBotID = bot.id }
                    } label: {
                        CrestView(index: bot.crest, size: 28)
                            .frame(width: 40, height: 40)
                            .background(on ? Theme.surface : .clear, in: shape)
                            .overlay(shape.stroke(on ? Theme.line : .clear))
                            .overlay(alignment: .bottomTrailing) {
                                let look = botLook(bot)
                                Lamp(status: look.lamp, ground: Theme.well).offset(x: 1, y: 1)
                            }
                    }
                    .buttonStyle(.press)
                    .accessibilityLabel("\(bot.name), \(botLook(bot).word)")
                }
                if canBuildSpace(spaces.role) {
                    Button { creating = true } label: {
                        Image(systemName: "plus").font(.system(size: 14, weight: .medium))
                            .frame(width: 40, height: 40)
                    }
                    .buttonStyle(.glyph(tint: Theme.ink3))
                    .accessibilityLabel("New Bot")
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    // MARK: pages

    @ViewBuilder
    private var pages: some View {
        let bot = model.selectedBot
        VStack(spacing: 2) {
            SideRow(symbol: BotTab.automations.symbol, title: "Automations", on: on(.automations)) { open(.automations) }
            SideRow(symbol: BotTab.artifacts.symbol, title: "Artifacts", on: on(.artifacts)) { open(.artifacts) }
            SideRow(symbol: BotTab.memories.symbol, title: "Memories", on: on(.memories)) { open(.memories) }
            SideRow(symbol: BotTab.knowledge.symbol, title: "Knowledge", on: on(.knowledge)) { open(.knowledge) }
            SideRow(symbol: BotTab.feed.symbol, title: "Feed", on: on(.feed),
                    trailing: (bot?.feedUnread ?? 0) > 0 ? AnyView(UnreadCount(count: Int(bot?.feedUnread ?? 0))) : nil) { open(.feed) }
            SideRow(symbol: BotTab.channels.symbol, title: "Channels", on: on(.channels)) { open(.channels) }
        }
        .padding(.horizontal, 10)
    }

    private func on(_ tab: BotTab) -> Bool { model.path.first == .page(tab) }

    private func open(_ tab: BotTab) { go(.page(tab)) }

    private func go(_ route: BotRoute) {
        model.path = [route]
        model.sidebarOpen = false
    }

    // MARK: chats

    private var sideChatsHead: some View {
        HStack(spacing: 8) {
            Text("Side chats").font(.silo(13, .medium)).foregroundStyle(Theme.ink2)
            Spacer(minLength: 0)
            Button { model.newChat() } label: {
                Image(systemName: "plus")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 30, height: 30)
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous).stroke(Theme.line))
            }
            .buttonStyle(.press)
            .accessibilityLabel("New side chat")
        }
        .padding(.leading, 18)
        .padding(.trailing, 10)
        .padding(.top, 16)
        .padding(.bottom, 6)
    }

    /// Your own side chats by date; on a Bot several people use, its managers also get the Team:
    /// everyone else's chats and the Bot's own, read-only.
    @ViewBuilder
    private var chats: some View {
        let own = chatGroups(model.chats.filter { $0.mine && !$0.main }, updatedAt: { parseDate($0.updatedAt) })
        let others = model.chats.filter { !$0.mine }
        let groups = others.isEmpty ? own : own + [ChatGroup(title: "Team", items: others)]
        ForEach(groups) { group in
            let open = isOpen(group)
            Section {
                if open {
                    ForEach(group.items, id: \.id) { chat in
                        SidebarChatRow(chat: chat, on: chat.id == model.selectedChatID && model.path.isEmpty, team: group.title == "Team")
                            .transition(.opacity)
                    }
                }
            } header: {
                groupHeader(group, open: open)
            }
        }
    }

    // A group folds to the opposite of how it starts; what the reader did is remembered.
    private func isOpen(_ group: ChatGroup<Silo_V1_Chat>) -> Bool {
        let flipped = folded.split(separator: "|").contains { $0 == group.title }
        return group.opensByDefault != flipped
    }

    private func flip(_ title: String) {
        var set = Set(folded.split(separator: "|").map(String.init))
        if set.remove(title) == nil { set.insert(title) }
        folded = set.sorted().joined(separator: "|")
    }

    private func groupHeader(_ group: ChatGroup<Silo_V1_Chat>, open: Bool) -> some View {
        Button {
            withAnimation(Motion.fold) { flip(group.title) }
        } label: {
            HStack(spacing: 6) {
                Text(group.title.uppercased()).font(.silo(11, .medium)).tracking(0.88).foregroundStyle(Theme.ink3).lineLimit(1)
                Spacer(minLength: 0)
                if !open { Text("\(group.items.count)").font(.silo(11)).foregroundStyle(Theme.ink3).monospacedDigit() }
                Image(systemName: "chevron.down")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(Theme.ink3)
                    .rotationEffect(.degrees(open ? 0 : -90))
            }
            .padding(.horizontal, 18)
            .frame(height: 34)
            .background(Theme.well)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(group.title), \(group.items.count) chats")
    }

    // MARK: foot

    private var foot: some View {
        VStack(alignment: .leading, spacing: 2) {
            SideRow(symbol: BotTab.workspace.symbol, title: spaces.current?.name ?? "Workspace",
                    on: model.path.first == .workspace || model.path.first.map { if case .space = $0 { return true } else { return false } } == true) { go(.workspace) }
            SideRow(symbol: "gearshape", title: "Settings", on: model.path.first == .settings,
                    trailing: (model.selectedBot?.auditUnseen ?? 0) > 0 ? AnyView(UnreadCount(count: Int(model.selectedBot?.auditUnseen ?? 0))) : nil) { go(.settings) }
            ProfileRow { account = true }
        }
        .padding(.horizontal, 10)
        .padding(.top, 6)
        .padding(.bottom, 4)
        .overlay(alignment: .top) { Theme.line.frame(height: 1) }
    }
}

/// The Bot's face, name and one line; what needs you says where.
private struct BotHead: View {
    let bot: Silo_V1_Bot

    var body: some View {
        let look = botLook(bot)
        let paused = bot.activity.first { $0.state == "needs_you" }
        HStack(spacing: 10) {
            CrestView(index: bot.crest, size: 30)
            VStack(alignment: .leading, spacing: 1) {
                Text(bot.name).font(.silo(16, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                HStack(spacing: 6) {
                    StatusWord(look: look)
                    if let paused {
                        Text("· \(activitySource(kind: paused.kind, title: paused.title))")
                            .font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                    }
                }
            }
            Spacer(minLength: 0)
            if bot.people.count > 1 { PeopleFaces(people: bot.people) }
        }
        .padding(.horizontal, 18)
        .padding(.top, 14)
        .padding(.bottom, 12)
    }
}

/// The Bot's two standing places, lifted as one card: the main chat with Call beside it, and the
/// Dashboard. Each row says what is there now.
private struct HomeCard: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot
    let go: (BotRoute) -> Void

    private var mainOn: Bool { model.mainChat != nil && model.mainChat?.id == model.selectedChatID && model.path.isEmpty }
    private var dashOn: Bool { model.path.first == .page(.dashboard) }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Button { model.showMainChat() } label: {
                    HStack(spacing: 10) {
                        Image(systemName: "bubble.left.fill")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(Theme.onInk)
                            .frame(width: 30, height: 30)
                            .background(Theme.ink, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
                        VStack(alignment: .leading, spacing: 1) {
                            Text("Main chat").font(.silo(14.5, .medium)).foregroundStyle(Theme.ink)
                            mainLine
                        }
                        Spacer(minLength: 0)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.press)
                if model.callEnabled, model.mainChat != nil { callButton }
            }
            .padding(.leading, 10)
            .padding(.trailing, 8)
            .padding(.vertical, 10)
            .overlay(alignment: .leading) { ActiveBar(on: mainOn) }
            Theme.line.frame(height: 1).padding(.horizontal, 10)
            Button { go(.page(.dashboard)) } label: {
                HStack(spacing: 10) {
                    Image(systemName: BotTab.dashboard.symbol)
                        .font(.system(size: 13))
                        .foregroundStyle(dashOn ? Theme.cobalt : Theme.ink2)
                        .frame(width: 30, height: 30)
                        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
                    VStack(alignment: .leading, spacing: 1) {
                        Text("Dashboard").font(.silo(14.5, .medium)).foregroundStyle(Theme.ink)
                        digest
                    }
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 10)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .overlay(alignment: .leading) { ActiveBar(on: dashOn) }
        }
        .background(Theme.surface, in: shape)
        .clipShape(shape)
        .overlay(shape.stroke(Theme.line))
        .floatShadow()
    }

    /// What the main chat is doing now, else when it last moved.
    @ViewBuilder
    private var mainLine: some View {
        let id = model.mainChat?.id
        let call = CallModel.shared
        let row = chatDoing(bot, id)
        if let row {
            DoingLine(look: runLook(Doing(row)), step: progress(row), font: .silo(12.5))
        } else if id != nil, id == model.selectedChatID, model.isRunning {
            DoingLine(look: runLook(.started), font: .silo(12.5))
        } else if call.live, call.who?.id == bot.id, call.snap?.chatID == id {
            DoingLine(look: runLook(Doing(state: "on_call")), font: .silo(12.5))
        } else {
            Text(model.mainChat.map { lastActive($0.updatedAt) } ?? " ")
                .font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
        }
    }

    /// The one thing worth saying about the board: what needs you, what changed, else nothing new.
    @ViewBuilder
    private var digest: some View {
        if !model.approvals.isEmpty {
            HStack(spacing: 6) {
                Circle().fill(Theme.vermilion).frame(width: 6, height: 6)
                Text("\(model.approvals.count) need you").font(.silo(12.5, .medium)).foregroundStyle(Theme.vermilion)
            }
        } else if bot.dashboardAsks > 0 {
            HStack(spacing: 5) {
                Image(systemName: "questionmark.bubble").font(.system(size: 11, weight: .medium))
                Text(bot.dashboardAsks == 1 ? "1 question" : "\(bot.dashboardAsks) questions").font(.silo(12.5, .medium))
            }
            .foregroundStyle(Theme.cobalt)
        } else {
            Text("Nothing new").font(.silo(12.5)).foregroundStyle(Theme.ink3)
        }
    }

    /// Rings the Bot in the main chat. It carries the call: Calling…, then the clock, which leads back.
    @ViewBuilder
    private var callButton: some View {
        let call = CallModel.shared
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        if call.live, call.who?.id == bot.id, let s = call.snap {
            Button { call.expanded = true } label: {
                HStack(spacing: 6) {
                    if s.state == .connecting {
                        Circle().fill(Theme.ink3).frame(width: 6, height: 6).breathing()
                        Text("Calling…").font(.silo(13, .medium)).foregroundStyle(Theme.ink2)
                    } else {
                        Lamp(status: .online)
                        CallClock(since: s.startedAt)
                    }
                }
                .padding(.horizontal, 10)
                .frame(height: 34)
                .background(Theme.well, in: shape)
            }
            .buttonStyle(.press)
            .accessibilityLabel("Back to the call")
        } else {
            Button { model.call() } label: {
                HStack(spacing: 6) {
                    Image(systemName: "phone.fill").font(.system(size: 12))
                    Text("Call").font(.silo(13, .medium))
                }
                .foregroundStyle(Theme.onInk)
                .padding(.horizontal, 11)
                .frame(height: 34)
                .background(Theme.ink, in: shape)
            }
            .buttonStyle(.press)
            .sensoryFeedback(.impact(weight: .medium), trigger: call.live)
        }
    }
}

/// The 2pt cobalt bar on the open row; it grows in as the last one shrinks out.
private struct ActiveBar: View {
    let on: Bool

    var body: some View {
        Theme.cobalt
            .frame(width: 2)
            .scaleEffect(y: on ? 1 : 0)
            .opacity(on ? 1 : 0)
            .animation(Motion.state, value: on)
    }
}

/// The machine under the pages: its panes as labelled tiles, three to a row, then Start or Stop.
private struct MachineTiles: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot
    let go: (BotRoute) -> Void

    private var tabs: [BotTab] {
        var list: [BotTab] = [.files, .changes]
        if !bot.mailAddress.isEmpty { list.append(.mail) }
        list.append(.container)
        return list
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Machine").font(.silo(13, .medium)).foregroundStyle(Theme.ink2).padding(.leading, 8)
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 6), count: 3), spacing: 6) {
                ForEach(tabs) { tab in
                    Button { go(.page(tab)) } label: {
                        tile(symbol: tab.symbol, title: tab.title, tint: Theme.ink2)
                    }
                    .buttonStyle(.press)
                }
                Button {
                    Task { if bot.machine == "online" { await model.stopBot() } else { await model.startBot() } }
                } label: {
                    tile(symbol: "power", title: bot.machine == "online" ? "Stop" : bot.machine == "starting" ? "Starting…" : "Start",
                         tint: bot.machine == "online" ? Theme.ink : Theme.ink2)
                }
                .buttonStyle(.press)
                .disabled(bot.machine == "starting" || model.startingBotID == bot.id)
                .sensoryFeedback(.impact(weight: .light), trigger: bot.machine)
                .accessibilityLabel(powerWord(machine: bot.machine))
            }
        }
        .padding(.horizontal, 10)
        .padding(.top, 14)
    }

    private func tile(symbol: String, title: String, tint: Color) -> some View {
        VStack(spacing: 4) {
            Image(systemName: symbol).font(.system(size: 15)).foregroundStyle(tint)
            Text(title).font(.silo(12, .medium)).foregroundStyle(Theme.ink2).lineLimit(1)
        }
        .frame(maxWidth: .infinity)
        .frame(height: 56)
        .background(Theme.surface.opacity(0.7), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

/// Who is signed in, at the foot: an initial, the email, and the way to the account.
private struct ProfileRow: View {
    @Environment(AppModel.self) private var model
    let action: () -> Void

    private var email: String { model.user?.email ?? "" }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Text(email.first.map { String($0).uppercased() } ?? "?")
                    .font(.silo(13, .semibold))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 28, height: 28)
                    .background(Theme.ink, in: Circle())
                Text(email.isEmpty ? "Account" : email)
                    .font(.silo(14, .medium))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 8)
            .frame(height: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .accessibilityLabel("Account, \(email)")
    }
}

/// One sidebar row: a glyph and a label; the open one is a flat `surface` with the cobalt bar.
struct SideRow: View {
    let symbol: String
    let title: String
    var on = false
    var trailing: AnyView?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 11) {
                Image(systemName: symbol)
                    .font(.system(size: 15))
                    .foregroundStyle(on ? Theme.ink : Theme.ink2)
                    .frame(width: 20)
                Text(title).font(.silo(14.5, .medium)).foregroundStyle(on ? Theme.ink : Theme.ink2).lineLimit(1)
                Spacer(minLength: 0)
                if let trailing { trailing }
            }
            .padding(.horizontal, 8)
            .frame(height: 40)
            .background(on ? Theme.surface : .clear, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .overlay(alignment: .leading) { ActiveBar(on: on).padding(.vertical, 8) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
    }
}

/// The Bot's work with no row of its own here: automations and channels with what they are on, its
/// subagents counted on one line. It shows only while there is some.
private struct ActivityStrip: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    var body: some View {
        let runs = bot.activity.filter { ["automation", "channel", "hidden"].contains($0.kind) }
        let agents = bot.activity.filter { $0.kind == "subagent" }
        if !runs.isEmpty || !agents.isEmpty {
            VStack(alignment: .leading, spacing: 1) {
                ForEach(Array(runs.prefix(3).enumerated()), id: \.offset) { _, row in
                    let look = runLook(Doing(row))
                    rowView(lamp: look.lamp, place: activitySource(kind: row.kind, title: row.title), word: look.word, step: progress(row),
                            tone: look.lamp == .working ? Theme.ink3 : Theme.color(look.tone)) { open(row.chatID) }
                }
                if let lead = agents.first {
                    rowView(lamp: agents.contains { $0.state == "needs_you" } ? .needsYou : .working,
                            place: agents.count == 1 ? "1 subagent at work" : "\(agents.count) subagents at work",
                            word: "", step: "", tone: Theme.ink3) { open(lead.leadChatID) }
                }
            }
            .padding(4)
            .background(Theme.surface.opacity(0.6), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .padding(.horizontal, 10)
            .padding(.bottom, 10)
            .transition(.rise)
        }
    }

    private func rowView(lamp: LampStatus, place: String, word: String, step: String, tone: Color, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Lamp(status: lamp)
                Text(place).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                Text(word).font(.silo(13)).foregroundStyle(tone).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                if !step.isEmpty { Text(step).font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
            }
            .padding(.horizontal, 8)
            .frame(height: 30)
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
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

/// Unread posts: a cobalt count.
private struct UnreadCount: View {
    let count: Int

    var body: some View {
        Text(count > 99 ? "99+" : "\(count)")
            .font(.siloMono(11, .medium))
            .foregroundStyle(Theme.onAccent)
            .padding(.horizontal, 6)
            .frame(height: 18)
            .background(Theme.cobalt, in: Capsule())
    }
}

/// A chat in the sidebar: one line, its live run at the end. The open one keeps rename and delete
/// in view (there is no hover here); any row opens them with a long press.
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
                    .font(.silo(14.5))
                    .foregroundStyle(on ? Theme.ink : Theme.ink2)
                    .lineLimit(1)
                Spacer(minLength: 4)
                doing
            }
            .padding(.leading, 18)
            .padding(.trailing, 12)
            .frame(height: 38)
            .background(on ? Theme.surface : .clear)
            .overlay(alignment: .leading) { ActiveBar(on: on) }
            .contentShape(Rectangle())
        }
        .buttonStyle(RowPressStyle())
        .longPressMenu(enabled: !team) {
            MenuRow(title: "Rename", symbol: "pencil") { begin() }
            MenuRow(title: "Delete", symbol: "trash", destructive: true) { deleting = true }
        }
        .promptSlip($renaming, title: "Rename chat", prompt: "Title", text: $renameText) {
            Task { await model.renameChat(chat.id, title: renameText) }
        }
        .confirmSlip($deleting, title: "Delete this chat?", message: "The conversation and its history are removed.", action: "Delete chat") {
            Task { await model.deleteChat(chat.id) }
        }
    }

    /// The lamp and its place on the Taskboard; Needs you and Waiting in words.
    @ViewBuilder
    private var doing: some View {
        if let bot = model.selectedBot, let row = chatDoing(bot, chat.id) {
            let look = runLook(Doing(row))
            HStack(spacing: 5) {
                Lamp(status: look.lamp)
                if look.lamp == .working {
                    if !progress(row).isEmpty { Text(progress(row)).font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
                } else {
                    Text(look.word).font(.silo(12)).foregroundStyle(Theme.color(look.tone)).lineLimit(1)
                }
            }
        } else if on && model.isRunning {
            Lamp(status: .working)
        }
    }

    private func begin() {
        renameText = chat.title
        renaming = true
    }
}
