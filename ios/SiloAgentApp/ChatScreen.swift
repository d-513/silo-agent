import Observation
import SiloClient
import SwiftUI

/// The chat the app opens on. The crest opens the Bot's sidebar, the lamp its machine; the title
/// carries the chat's own actions. A run paused on an approval puts its slip where the composer was.
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
        Group {
            if model.selectedChatID == nil && model.events.isEmpty {
                DraftIntro()
            } else {
                ThreadView(events: model.events, busy: model.isRunning, interactive: !model.readOnly,
                           prepended: model.prepended, earlier: model.earlier, loadingEarlier: model.loadingEarlier,
                           onEarlier: { await model.loadEarlier() })
            }
        }
        .background(Theme.canvas.ignoresSafeArea())
        .safeAreaInset(edge: .top, spacing: 0) { TaskboardStrip() }
        .safeAreaBar(edge: .bottom, spacing: 0) {
            VStack(spacing: 0) {
                SubagentTray()
                if let approval = model.chatApproval {
                    ApprovalSlip(approval: approval, here: true)
                        .padding(.horizontal, 10)
                        .padding(.bottom, 6)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                } else if model.readOnly, let chat = model.selectedChat {
                    ReadOnlyBar(chat: chat)
                } else {
                    ComposerView()
                }
            }
            .animation(.snappy(duration: 0.3), value: model.chatApproval?.id)
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            // A width-capped title, so a long one never pushes New chat and the lamp into overflow.
            ToolbarItem(placement: .principal) {
                if model.selectedChatID != nil {
                    Menu { titleMenu } label: { titleLabel(chevron: true) }
                        .menuIndicator(.hidden)
                } else {
                    titleLabel(chevron: false)
                }
            }
            .sharedBackgroundVisibility(.hidden)
            if sizeClass != .regular {
                ToolbarItem(placement: .topBarLeading) {
                    Button { model.sidebarOpen = true } label: {
                        if let bot = model.selectedBot { CrestView(index: bot.crest, size: 22) }
                    }
                    .accessibilityLabel("Open the sidebar")
                }
                .sharedBackgroundVisibility(.hidden)
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button { model.newChat() } label: { Image(systemName: "square.and.pencil").foregroundStyle(Theme.ink) }
                    .accessibilityLabel("New chat")
                    .disabled(model.selectedChatID == nil)
            }
            .sharedBackgroundVisibility(.hidden)
            ToolbarItem(placement: .topBarTrailing) {
                Button { model.machineOpen = true } label: {
                    StatusDot(status: model.selectedBot?.status ?? "stopped", size: 10)
                        .frame(width: 22, height: 22)
                }
                .accessibilityLabel("Machine, \(statusLabel(model.selectedBot?.status ?? "stopped"))")
            }
            .sharedBackgroundVisibility(.hidden)
        }
        .alert("Rename chat", isPresented: $renaming) {
            TextField("Title", text: $renameText)
            Button("Cancel", role: .cancel) {}
            Button("Rename") {
                if let id = model.selectedChatID { Task { await model.renameChat(id, title: renameText) } }
            }
        }
        .confirmationDialog("Delete this chat?", isPresented: $deleting, titleVisibility: .visible) {
            Button("Delete chat", role: .destructive) {
                if let id = model.selectedChatID { Task { await model.deleteChat(id) } }
            }
        } message: {
            Text("The conversation and its history are removed.")
        }
    }

    private func titleLabel(chevron: Bool) -> some View {
        HStack(spacing: 4) {
            Text(title)
                .font(.silo(16, .semibold))
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
            if chevron {
                Image(systemName: "chevron.down")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(Theme.ink3)
            }
        }
        .frame(maxWidth: 210)
    }

    @ViewBuilder
    private var titleMenu: some View {
        // The main chat is never renamed or deleted; someone else's chat is only read.
        if let chat = model.selectedChat, chat.mine {
            if !chat.main {
                Button {
                    renameText = chat.title
                    renaming = true
                } label: { Label("Rename", systemImage: "pencil") }
            }
            Button { Task { await model.collectMemories() } } label: {
                Label("Save Memories from This Chat", systemImage: "brain")
            }
            Button { Task { await model.compact() } } label: {
                Label("Compact Conversation", systemImage: "rectangle.compress.vertical")
            }
            .disabled(model.isRunning)
            if !chat.main {
                Button(role: .destructive) { deleting = true } label: { Label("Delete Chat", systemImage: "trash") }
            }
        } else if let chat = model.selectedChat {
            Text(chat.author.isEmpty ? "The Bot's own chat" : "\(chat.author)'s chat")
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
                if !bot.workerConnected {
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
                Button { Task { await model.stopRun() } } label: { Label("Stop", systemImage: "stop.fill") }
                    .buttonStyle(.quiet)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }
}
