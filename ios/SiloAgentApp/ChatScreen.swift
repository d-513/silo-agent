import Observation
import SiloClient
import SwiftUI

/// The chat the app opens on. Its head is the Bot's: the sidebar's press target, the Bot over the
/// chat you are in with the chat's own actions, Call, and New chat. A run paused on an approval puts
/// its slip where the composer was.
struct ChatScreen: View {
    @Environment(AppModel.self) private var model
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var renaming = false
    @State private var renameText = ""
    @State private var deleting = false

    private var title: String {
        guard let chat = model.selectedChat else { return model.selectedChatID == nil ? "New chat" : "" }
        if chat.main { return "Main chat" }
        return chat.title.isEmpty ? "New chat" : chat.title
    }

    var body: some View {
        VStack(spacing: 0) {
            head
            TaskboardStrip()
            Group {
                if model.selectedChatID == nil && model.events.isEmpty {
                    DraftIntro()
                } else {
                    ThreadView(events: model.events, busy: model.isRunning, interactive: !model.readOnly,
                               prepended: model.prepended, earlier: model.earlier, loadingEarlier: model.loadingEarlier,
                               onEarlier: { await model.loadEarlier() })
                }
            }
            .frame(maxHeight: .infinity)
            VStack(spacing: 0) {
                SubagentTray()
                if let approval = model.chatApproval {
                    ApprovalSlip(approval: approval, here: true)
                        .padding(.horizontal, 10)
                        .padding(.bottom, 6)
                        .transition(.slip)
                } else if model.readOnly, let chat = model.selectedChat {
                    ReadOnlyBar(chat: chat)
                } else {
                    ComposerView()
                }
            }
            .animation(Motion.settle(), value: model.chatApproval?.id)
        }
        .background(Theme.canvas.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
        .promptSlip($renaming, title: "Rename chat", prompt: "Title", text: $renameText) {
            if let id = model.selectedChatID { Task { await model.renameChat(id, title: renameText) } }
        }
        .confirmSlip($deleting, title: "Delete this chat?", message: "The conversation and its history are removed.", action: "Delete chat") {
            if let id = model.selectedChatID { Task { await model.deleteChat(id) } }
        }
    }

    // MARK: head

    private var head: some View {
        HStack(spacing: 4) {
            if sizeClass != .regular { drawerButton }
            SiloMenu {
                titleMenu
            } label: {
                VStack(alignment: .leading, spacing: 0) {
                    Text(model.selectedBot?.name ?? "").font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                    HStack(spacing: 3) {
                        Text(title).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                        Image(systemName: "chevron.down").font(.system(size: 8, weight: .semibold)).foregroundStyle(Theme.ink3)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.leading, 4)
            }
            if model.callEnabled {
                Button { model.call(chat: model.selectedChatID ?? "new") } label: {
                    Image(systemName: "phone")
                }
                .buttonStyle(.glyph(tint: Theme.ink))
                .accessibilityLabel("Call")
            }
            Button { model.machineOpen = true } label: {
                Image(systemName: "cpu")
            }
            .buttonStyle(.glyph)
            .accessibilityLabel("Machine, \(machineLook(model.selectedBot?.machine ?? "").word)")
            Button { model.newChat() } label: {
                Image(systemName: "plus")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(Theme.ink)
                    .frame(width: 36, height: 36)
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.line))
            }
            .buttonStyle(.press)
            .disabled(model.selectedChatID == nil)
            .accessibilityLabel("New chat")
        }
        .padding(.leading, 6)
        .padding(.trailing, 10)
        .frame(height: 52)
        .background(Theme.canvas)
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    /// The sidebar's press target: the menu glyph (a dot when something waits) and the crest with
    /// its lamp.
    private var drawerButton: some View {
        let bot = model.selectedBot
        let waits: Color? = !model.approvals.isEmpty ? Theme.vermilion : (bot?.dashboardAsks ?? 0) > 0 ? Theme.cobalt : nil
        return Button { model.sidebarOpen = true } label: {
            HStack(spacing: 8) {
                Image(systemName: "line.3.horizontal")
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(Theme.ink2)
                    .overlay(alignment: .topTrailing) {
                        if let waits { Circle().fill(waits).frame(width: 7, height: 7).offset(x: 4, y: -3) }
                    }
                if let bot {
                    CrestView(index: bot.crest, size: 28)
                        .overlay(alignment: .bottomTrailing) {
                            Lamp(status: botLook(bot).lamp, ground: Theme.canvas).offset(x: 2, y: 2)
                        }
                }
            }
            .padding(.leading, 8)
            .padding(.trailing, 4)
            .frame(height: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .accessibilityLabel("Open the sidebar")
    }

    @ViewBuilder
    private var titleMenu: some View {
        // The main chat is never renamed or deleted; someone else's chat is only read.
        if let chat = model.selectedChat, chat.mine {
            if !chat.main {
                MenuRow(title: "Rename", symbol: "pencil") {
                    renameText = chat.title
                    renaming = true
                }
            }
            MenuRow(title: "Save memories from this chat", symbol: "brain") { Task { await model.collectMemories() } }
            MenuRow(title: "Compact conversation", symbol: "rectangle.compress.vertical", enabled: !model.isRunning) {
                Task { await model.compact() }
            }
            if !chat.main {
                MenuDivider()
                MenuRow(title: "Delete chat", symbol: "trash", destructive: true) { deleting = true }
            }
        } else if let chat = model.selectedChat {
            MenuHeading(text: chat.author.isEmpty ? "The Bot's own chat" : "\(chat.author)'s chat")
        } else {
            MenuHeading(text: "A new chat")
        }
    }
}

/// A new chat before its first message: who you are talking to, and nothing else to do but type.
private struct DraftIntro: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            VStack(alignment: .leading, spacing: 12) {
                Spacer()
                CrestView(index: bot.crest, size: 44)
                Text(bot.name).font(.silo(26, .medium)).foregroundStyle(Theme.ink)
                if !bot.description_p.isEmpty {
                    Text(bot.description_p)
                        .font(.silo(15))
                        .foregroundStyle(Theme.ink2)
                        .lineLimit(4)
                }
                if bot.machine != "online" {
                    Text("Its machine is stopped. Sending a message starts it.")
                        .font(.silo(13))
                        .foregroundStyle(Theme.ink3)
                }
                if bot.people.count > 1 {
                    // The trust line: on a Bot several people use, managers read every chat.
                    Text("\(bot.name)'s managers can read this chat. What you tell \(bot.name), it may remember for everyone who uses it.")
                        .font(.silo(13))
                        .foregroundStyle(Theme.ink3)
                }
                Spacer()
                Spacer()
            }
            .padding(.horizontal, 28)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .rise()
        }
    }
}

/// In place of the composer on someone else's chat (or the Bot's own): who writes here, and Stop.
private struct ReadOnlyBar: View {
    @Environment(AppModel.self) private var model
    let chat: Silo_V1_Chat

    var body: some View {
        HStack(spacing: 12) {
            Text(chat.author.isEmpty
                 ? "\(model.selectedBot?.name ?? "The Bot")'s own chat. You can read it and stop it."
                 : "\(chat.author)'s chat. You can read it and stop it; only \(chat.author) writes in it.")
                .font(.silo(13))
                .foregroundStyle(Theme.ink3)
                .frame(maxWidth: .infinity, alignment: .leading)
            if model.isRunning {
                Button { Task { await model.stopRun() } } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "stop.fill").font(.system(size: 10))
                        Text("Stop")
                    }
                }
                .buttonStyle(.quiet(tint: Theme.vermilion))
                .controlSize(.small)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }
}
