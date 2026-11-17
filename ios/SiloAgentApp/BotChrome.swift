import Observation
import SiloClient
import SwiftUI

// The chrome over every page of a Bot, as the web draws it below 960 (`bot/BotDrawer.tsx`,
// `bot/ChatTabs.tsx`): one top bar (the sidebar's menu, the Bot's crest and name over what it is
// doing, Call, the rail), the conversations as a strip of tabs under it, and the rail as a sheet
// from the right with the pages beside the conversations, the machine and Settings.

/// The Bot's top bar. The crest and name (or the rail glyph, or a swipe in from the right edge)
/// slide in the rail. On a conversation the line under the name is the Bot's own, the way a
/// messenger says someone is typing; on any other page it names the page.
struct BotTopBar: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    var body: some View {
        let look = botLook(bot)
        let label = pageLabel
        HStack(spacing: 2) {
            MenuGlyph()
            Button { model.railOpen = true } label: {
                HStack(spacing: 10) {
                    CrestView(index: bot.crest, size: 32, state: crestState(bot))
                        .overlay(alignment: .topTrailing) { Lamp(status: look.lamp, ground: Theme.well).offset(x: 2, y: -2) }
                    VStack(alignment: .leading, spacing: 1) {
                        Text(bot.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        if let label {
                            Text(label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3).lineLimit(1)
                        } else {
                            StatusWord(look: look, font: .silo(12, .medium), lamp: false)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .padding(.leading, 6)
                .padding(.trailing, 8)
                .frame(height: 44)
                .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            .accessibilityLabel("\(bot.name), \(label ?? look.word). Pages and machine")
            if model.callEnabled { call }
            Button { model.railOpen = true } label: {
                Image(systemName: "sidebar.right")
                    .font(.system(size: 17))
                    .overlay(alignment: .topTrailing) {
                        if bot.feedUnread + bot.auditUnseen > 0 {
                            Circle().fill(Theme.cobalt).frame(width: 8, height: 8)
                                .overlay(Circle().stroke(Theme.well, lineWidth: 2))
                                .offset(x: 4, y: -4)
                        }
                    }
            }
            .buttonStyle(.glyph(tint: Theme.ink2))
            .accessibilityLabel("Pages and machine")
        }
        .padding(.horizontal, 4)
        .frame(height: 48)
        .background(Theme.well.ignoresSafeArea(edges: .top))
    }

    /// The page the bar names when it is not a conversation; nil on the chat and the Dashboard.
    private var pageLabel: String? {
        switch model.page {
        case nil, .page(.dashboard): return nil
        case .page(let tab): return tab.title
        case .settings: return "Settings"
        case .workspace, .space: return nil
        }
    }

    /// Rings the main chat: a 32pt ink circle, or during a call the `surface` pill with the lamp and
    /// the clock, which leads back to it.
    @ViewBuilder
    private var call: some View {
        let call = CallModel.shared
        if call.live, call.who?.id == bot.id, let s = call.snap {
            Button { call.expanded = true } label: {
                HStack(spacing: 6) {
                    if s.state == .connecting {
                        Circle().fill(Theme.ink3).frame(width: 7, height: 7).breathing()
                        Text("Calling…").font(.silo(13, .medium)).foregroundStyle(Theme.ink2)
                    } else {
                        Lamp(status: .online)
                        CallClock(since: s.startedAt)
                    }
                }
                .padding(.horizontal, 12)
                .frame(height: 34)
                .background(Theme.surface, in: Capsule())
                .floatShadow()
            }
            .buttonStyle(.press)
            .padding(.horizontal, 2)
            .accessibilityLabel("Back to the call")
        } else {
            Button { model.call(bot: bot) } label: {
                Image(systemName: "phone.fill")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 32, height: 32)
                    .background(Theme.ink, in: Circle())
                    .frame(width: 40, height: 40)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            .sensoryFeedback(.impact(weight: .medium), trigger: call.live)
            .accessibilityLabel("Call \(bot.name)")
        }
    }
}

// MARK: - The chat tabs

/// The conversations as a strip of tabs: the Dashboard (its glyph, with what waits there), the main
/// chat, then your side chats in the order they were made, so a reply never moves a tab; + opens a
/// new one, and the arrow lists the closed ones (and, for a manager, everyone else's). A long press
/// on a side tab renames, closes or deletes it; × on the open one closes it, which only puts it away.
struct ChatTabsStrip: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot
    @State private var renaming: Silo_V1_Chat?
    @State private var renameText = ""
    @State private var deleting: Silo_V1_Chat?

    private var onChat: Bool { model.page == nil }
    private var openID: String? { onChat ? model.selectedChatID : nil }

    var body: some View {
        let main = model.mainChat
        let tabs = chatTabs(model.chats, open: openID)
        let draft = onChat && model.selectedChatID == nil
        HStack(alignment: .bottom, spacing: 2) {
            dashboardTab
            if let main {
                StripTab(on: openID == main.id, close: nil) {
                    model.showChat(main.id)
                } label: {
                    Image(systemName: "bubble.left").font(.system(size: 13))
                    Text("Main chat").font(.silo(13, .medium)).fixedSize()
                    lamp(main.id)
                }
            }
            if !tabs.isEmpty || draft {
                Theme.line.frame(width: 1, height: 16).padding(.horizontal, 3).padding(.bottom, 10)
            }
            ScrollViewReader { reader in
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .bottom, spacing: 2) {
                        ForEach(tabs, id: \.id) { chat in
                            sideTab(chat).id(chat.id)
                        }
                        if draft {
                            StripTab(on: true, close: { home() }) {} label: {
                                Text("New chat").font(.silo(13, .medium)).fixedSize()
                            }
                            .id("draft")
                        }
                    }
                    .frame(maxHeight: .infinity, alignment: .bottom)
                }
                .onChange(of: openID ?? (draft ? "draft" : ""), initial: true) { _, id in
                    guard !id.isEmpty else { return }
                    withAnimation(Motion.state) { reader.scrollTo(id) }
                }
            }
            .layoutPriority(-1)
            Button { model.newChat() } label: {
                Image(systemName: "plus").font(.system(size: 15, weight: .medium))
            }
            .buttonStyle(.glyph(tint: Theme.ink2, side: 34))
            .padding(.bottom, 3)
            .accessibilityLabel("New chat")
            ClosedMenu()
                .padding(.bottom, 3)
        }
        .padding(.horizontal, 6)
        .frame(height: 40)
        .background(Theme.well)
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
        .promptSlip(Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } }),
                    title: "Rename chat", prompt: "Title", text: $renameText) {
            if let id = renaming?.id { Task { await model.renameChat(id, title: renameText) } }
        }
        .confirmSlip(Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                     title: "Delete this chat?", message: "The conversation and its history are removed.", action: "Delete chat") {
            if let id = deleting?.id { Task { await model.deleteChat(id) } }
        }
    }

    /// The Dashboard as its glyph, with what waits there: what needs you (approvals and urgent
    /// questions) in vermilion, else what is new in cobalt.
    private var dashboardTab: some View {
        let needs = model.approvals.count + Int(bot.dashboardUrgent)
        let on = model.page == .page(.dashboard)
        let fresh = on ? 0 : Int(bot.dashboardAsks + bot.feedUnread)
        return StripTab(on: on, close: nil) {
            model.open(.page(.dashboard))
        } label: {
            Image(systemName: BotTab.dashboard.symbol).font(.system(size: 14))
            if needs > 0 {
                Text(needs > 99 ? "99+" : "\(needs)")
                    .font(.siloMono(11, .medium))
                    .foregroundStyle(Theme.onAccent)
                    .padding(.horizontal, 6)
                    .frame(minWidth: 18)
                    .frame(height: 18)
                    .background(Theme.vermilion, in: Capsule())
                    .transition(.rise)
            } else if fresh > 0 {
                UnreadBadge(count: fresh)
            }
        }
        .accessibilityLabel(needs > 0 ? "Dashboard, \(needs) need you" : "Dashboard")
    }

    private func sideTab(_ chat: Silo_V1_Chat) -> some View {
        let on = openID == chat.id
        return StripTab(on: on, close: on && chat.mine ? { close(chat) } : nil) {
            model.showChat(chat.id)
        } label: {
            if !chat.mine {
                if chat.author.isEmpty {
                    Image(systemName: "cpu").font(.system(size: 11)).foregroundStyle(Theme.ink3)
                        .accessibilityLabel("The Bot's own")
                } else {
                    Initials(email: chat.author, size: 18)
                }
            }
            Text(chat.title.isEmpty ? "New chat" : chat.title)
                .font(.silo(13, .medium))
                .lineLimit(1)
                .frame(maxWidth: 140, alignment: .leading)
                .fixedSize(horizontal: true, vertical: false)
            lamp(chat.id)
        }
        .longPressMenu(enabled: chat.mine) {
            MenuRow(title: "Rename", symbol: "pencil") {
                renameText = chat.title
                renaming = chat
            }
            if chat.hidden {
                MenuRow(title: "Open as a tab again", symbol: "arrow.uturn.up") { Task { await model.hideChat(chat.id, hidden: false) } }
            } else {
                MenuRow(title: "Close tab", symbol: "xmark") { close(chat) }
            }
            MenuDivider()
            MenuRow(title: "Delete chat", symbol: "trash", destructive: true) { deleting = chat }
        }
    }

    /// What a chat is doing, as its tab has room for it: the lamp.
    @ViewBuilder
    private func lamp(_ id: String) -> some View {
        let call = CallModel.shared
        if let row = chatDoing(bot, id) {
            Lamp(status: runLook(Doing(row)).lamp)
        } else if id == model.selectedChatID, model.isRunning {
            Lamp(status: .working)
        } else if call.live, call.who?.id == bot.id, call.snap?.chatID == id {
            Lamp(status: runLook(Doing(state: "on_call")).lamp)
        }
    }

    /// Closing a tab puts the chat away; if it was the open one, its neighbour opens (the main chat
    /// after the last).
    private func close(_ chat: Silo_V1_Chat) {
        let next = afterClose(chatTabs(model.chats, open: openID).map(\.id), closing: chat.id)
        if chat.mine && !chat.hidden { Task { await model.hideChat(chat.id, hidden: true) } }
        if openID == chat.id {
            if next.isEmpty { home() } else { model.showChat(next) }
        }
    }

    private func home() {
        if let main = model.mainChat { model.showChat(main.id) } else { model.open(.page(.dashboard)) }
    }
}

/// One tab. The open one stands on the page's own `canvas`, joined to the page under it, with the
/// cobalt bar on its top edge; any other is `ink2` on the strip.
private struct StripTab<Label: View>: View {
    let on: Bool
    /// The × of the open side tab.
    let close: (() -> Void)?
    let action: () -> Void
    @ViewBuilder let label: () -> Label

    var body: some View {
        let shape = UnevenRoundedRectangle(topLeadingRadius: Theme.Radius.control, topTrailingRadius: Theme.Radius.control, style: .continuous)
        HStack(spacing: 6) {
            Button(action: action) {
                HStack(spacing: 7) { label() }
                    .foregroundStyle(on ? Theme.ink : Theme.ink2)
                    .padding(.leading, 11)
                    .padding(.trailing, close == nil ? 11 : 0)
                    .frame(height: 36)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            .accessibilityAddTraits(on ? .isSelected : [])
            if let close {
                Button(action: close) {
                    Image(systemName: "xmark").font(.system(size: 10, weight: .semibold))
                }
                .buttonStyle(.glyph(tint: Theme.ink3, side: 28))
                .padding(.trailing, 3)
                .accessibilityLabel("Close tab")
            }
        }
        .background {
            if on {
                shape.fill(Theme.canvas)
                    .overlay(shape.stroke(Theme.line, lineWidth: 1).mask(Rectangle().padding(.bottom, 1)))
                    .padding(.bottom, -1)
            }
        }
        .overlay(alignment: .top) {
            Capsule().fill(Theme.cobalt).frame(height: 2).padding(.horizontal, 10)
                .scaleEffect(x: on ? 1 : 0.5).opacity(on ? 1 : 0)
        }
        .animation(Motion.state, value: on)
    }
}

/// The arrow at the end of the tabs: the chats you closed, each a press from being a tab again,
/// and for a manager the Team's chats, which only open.
private struct ClosedMenu: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        let closed = closedChats(model.chats)
        let team = model.chats.filter { !$0.mine }
        SiloMenu {
            MenuHeading(text: "Closed chats")
            if closed.isEmpty {
                Text("Nothing here yet. A tab you close waits here.")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 8)
            }
            ForEach(closed, id: \.id) { chat in
                MenuRow(title: chat.title.isEmpty ? "New chat" : chat.title, detail: shortWhen(chat.updatedAt)) {
                    Task { await model.hideChat(chat.id, hidden: false) }
                    model.showChat(chat.id)
                }
            }
            if !team.isEmpty {
                MenuDivider()
                MenuHeading(text: "Team")
                ForEach(team, id: \.id) { chat in
                    MenuRow(title: chat.main ? "Main chat" : chat.title.isEmpty ? "New chat" : chat.title,
                            detail: chat.author.isEmpty ? "The Bot's own" : chat.author) {
                        model.showChat(chat.id)
                    }
                }
            }
        } label: {
            HStack(spacing: 3) {
                if !closed.isEmpty {
                    Text("\(closed.count)").font(.siloMono(11.5)).foregroundStyle(Theme.ink3).monospacedDigit()
                }
                Image(systemName: "chevron.down").font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.ink2)
            }
            .padding(.horizontal, 8)
            .frame(minWidth: 34)
            .frame(height: 34)
        }
        .accessibilityLabel(closed.isEmpty ? "Closed chats" : "Closed chats, \(closed.count)")
    }
}

// MARK: - The rail

/// The right rail with room for words: who the Bot is and what it is doing, its work with no tab
/// of its own, the pages beside the conversations, the machine, and Settings at the foot.
struct RailSheet: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    var body: some View {
        let look = botLook(bot)
        let paused = bot.activity.first { $0.state == "needs_you" }
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(spacing: 12) {
                        CrestView(index: bot.crest, size: 44, state: crestState(bot))
                        VStack(alignment: .leading, spacing: 2) {
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
                    .padding(.horizontal, 16)
                    .padding(.top, 16)
                    .padding(.bottom, 10)
                    ActivityStrip(bot: bot)
                        .padding(4)
                        .background(Theme.surface.opacity(0.6), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                        .padding(.horizontal, 12)
                        .padding(.bottom, 8)
                    VStack(spacing: 2) {
                        ForEach([BotTab.automations, .artifacts, .memories, .knowledge, .feed, .channels]) { tab in
                            let on = model.page == .page(tab)
                            SideRow(symbol: tab.symbol, title: tab.title, on: on,
                                    trailing: tab == .feed && !on && bot.feedUnread > 0 ? AnyView(UnreadBadge(count: Int(bot.feedUnread))) : nil) {
                                model.open(.page(tab))
                            }
                        }
                    }
                    .padding(.horizontal, 8)
                    MachineTiles(bot: bot)
                        .padding(.top, 14)
                }
                .padding(.bottom, 12)
            }
            .scrollIndicators(.hidden)
            let on = model.page == .settings
            SideRow(symbol: "gearshape", title: "Settings", on: on,
                    trailing: !on && bot.auditUnseen > 0 ? AnyView(UnreadBadge(count: Int(bot.auditUnseen))) : nil) {
                model.open(.settings)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 8)
            .overlay(alignment: .top) { Theme.line.frame(height: 1) }
        }
        .background(Theme.well.ignoresSafeArea())
    }
}

/// The rail's way to the machine: each pane as its own page, three to a row, then Start or Stop.
/// Desktop and Console stay on the web client.
private struct MachineTiles: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    private var tabs: [BotTab] {
        var list: [BotTab] = [.files, .changes]
        if !bot.mailAddress.isEmpty || model.page == .page(.mail) { list.append(.mail) }
        return list
    }

    var body: some View {
        let online = bot.machine == "online"
        let starting = bot.machine == "starting" || model.startingBotID == bot.id
        VStack(alignment: .leading, spacing: 6) {
            SectionLabel(text: "Machine").padding(.horizontal, 10)
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 6), count: 3), spacing: 6) {
                ForEach(tabs) { tab in
                    let on = model.page == .page(tab)
                    Button { model.open(.page(tab)) } label: {
                        tile(symbol: tab.symbol, title: tab.title, tint: on ? Theme.ink : Theme.ink2, on: on)
                    }
                    .buttonStyle(.press)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
                Button {
                    Task { if online { await model.stopBot() } else { await model.startBot() } }
                } label: {
                    tile(symbol: "power", title: starting ? "Starting…" : online ? "Stop" : "Start",
                         tint: online ? Theme.ink2 : Theme.emerald, on: false)
                }
                .buttonStyle(.press)
                .disabled(starting)
                .opacity(starting ? 0.5 : 1)
                .sensoryFeedback(.impact(weight: .light), trigger: bot.machine)
                .accessibilityLabel(powerWord(machine: bot.machine))
            }
            Text("Desktop and Console are on the web client.")
                .font(.silo(12))
                .foregroundStyle(Theme.ink3)
                .padding(.horizontal, 10)
                .padding(.top, 4)
        }
        .padding(.horizontal, 8)
    }

    private func tile(symbol: String, title: String, tint: Color, on: Bool) -> some View {
        VStack(spacing: 6) {
            Image(systemName: symbol).font(.system(size: 17))
            Text(title).font(.silo(12, .medium)).lineLimit(1)
        }
        .foregroundStyle(tint)
        .frame(maxWidth: .infinity)
        .frame(height: 60)
        .background(on ? Theme.surface : Theme.surface.opacity(0.55), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        .shadow(color: Theme.shade.opacity(on ? 0.08 : 0), radius: 2, y: 1)
    }
}
