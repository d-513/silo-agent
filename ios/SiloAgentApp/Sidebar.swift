import Observation
import SiloClient
import SwiftUI

/// The web sidebar on a phone: the Bots' crests, the Bot's name and status, New chat, the pages
/// beside the chat, the chats in date groups, and Customize, Settings and the account at the foot.
struct BotSidebar: View {
    @Environment(AppModel.self) private var model
    @State private var creating = false
    @State private var account = false
    @State private var hub = false

    var body: some View {
        VStack(spacing: 0) {
            crests
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
                    if let bot = model.selectedBot { header(bot) }
                    SideRow(symbol: "plus", title: "New chat", strong: true, on: model.selectedChatID == nil && model.path.isEmpty) {
                        model.newChat()
                    }
                    .padding(.bottom, 6)
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
        .sheet(isPresented: $hub) { SkillHub() }
    }

    // MARK: crests

    private var crests: some View {
        @Bindable var model = model
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 10) {
                ForEach(model.bots, id: \.id) { bot in
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
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    private func header(_ bot: Silo_V1_Bot) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(bot.name).font(.silo(17, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
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
        let waiting = model.approvals.count + Int(bot?.dashboardAsks ?? 0)
        SideRow(symbol: BotTab.dashboard.symbol, title: "Dashboard", strong: true, on: on(.dashboard),
                trailing: waiting > 0 ? AnyView(NeedsYouMark()) : nil) { open(.dashboard) }
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

    @ViewBuilder
    private var chats: some View {
        let groups = chatGroups(model.chats, updatedAt: { parseDate($0.updatedAt) })
        ForEach(groups) { group in
            Section {
                ForEach(group.items, id: \.id) { chat in
                    SidebarChatRow(chat: chat, on: chat.id == model.selectedChatID && model.path.isEmpty)
                }
            } header: {
                Text(group.title.uppercased())
                    .font(.silo(11, .medium))
                    .tracking(0.9)
                    .foregroundStyle(Theme.ink3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.top, 14)
                    .padding(.bottom, 4)
                    .background(Theme.well)
            }
        }
    }

    // MARK: foot

    private var foot: some View {
        VStack(alignment: .leading, spacing: 0) {
            Theme.line.frame(height: 1)
            SideRow(symbol: "slider.horizontal.3", title: "Customize", on: model.path.first == .customize) { go(.customize) }
            SideRow(symbol: "gearshape", title: "Settings", on: model.path.first == .settings) { go(.settings) }
            HStack(spacing: 0) {
                Button { account = true } label: {
                    Text(model.user?.email ?? "Account").lineLimit(1)
                }
                Text("  ·  ")
                Button { hub = true } label: { Text("Skill Hub") }
                Spacer(minLength: 0)
            }
            .buttonStyle(.plain)
            .font(.silo(13))
            .foregroundStyle(Theme.ink3)
            .padding(.horizontal, 16)
            .frame(height: 40)
        }
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

/// The Needs you lamp and word, as on the web's Dashboard row.
private struct NeedsYouMark: View {
    var body: some View {
        HStack(spacing: 5) {
            StatusDot(status: "needs_you", size: 7)
            Text("Needs you").font(.silo(12, .medium)).foregroundStyle(Theme.vermilion)
        }
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
    @State private var renaming = false
    @State private var renameText = ""
    @State private var deleting = false

    var body: some View {
        Button { model.showChat(chat.id) } label: {
            Text(chat.title.isEmpty ? "New chat" : chat.title)
                .font(.silo(15))
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 16)
                .frame(height: 40)
                .background(on ? Theme.surface : .clear)
                .overlay(alignment: .leading) { if on { Theme.cobalt.frame(width: 2) } }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button { begin() } label: { Label("Rename", systemImage: "pencil") }
            Button(role: .destructive) { deleting = true } label: { Label("Delete", systemImage: "trash") }
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
