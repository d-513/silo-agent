import Observation
import SiloClient
import SwiftUI

/// One chat row with the rename / delete actions (swipe or long-press).
struct ChatRow: View {
    @Environment(AppModel.self) private var model
    let chat: Silo_V1_Chat
    @State private var renaming = false
    @State private var renameText = ""
    @State private var deleting = false

    var body: some View {
        NavigationLink(value: BotRoute.chat(chat.id)) {
            VStack(alignment: .leading, spacing: 2) {
                Text(chat.title.isEmpty ? "New chat" : chat.title)
                    .lineLimit(1)
                if !chat.updatedAt.isEmpty {
                    Text(relativeTime(chat.updatedAt))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) { deleting = true } label: { Label("Delete", systemImage: "trash") }
            Button { begin() } label: { Label("Rename", systemImage: "pencil") }.tint(Theme.cobalt)
        }
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

/// Every chat with this Bot.
struct ChatsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        List(model.chats, id: \.id) { chat in
            ChatRow(chat: chat)
        }
        .overlay {
            if model.chats.isEmpty {
                ContentUnavailableView("No chats", systemImage: "bubble.left", description: Text("Start one to talk to this Bot."))
            }
        }
        .navigationTitle("Chats")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { Task { await model.newChat() } } label: { Label("New Chat", systemImage: "square.and.pencil") }
            }
        }
        .refreshable { await model.refreshChats() }
    }
}
