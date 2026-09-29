import Observation
import SiloClient
import SwiftUI

struct ChatsList: View {
    @Environment(AppModel.self) private var model
    @State private var renaming: Silo_V1_Chat?
    @State private var renameText = ""
    @State private var deleting: Silo_V1_Chat?

    var body: some View {
        @Bindable var model = model
        List(selection: $model.selectedChatID) {
            ForEach(model.chats, id: \.id) { chat in
                VStack(alignment: .leading, spacing: 2) {
                    Text(chat.title.isEmpty ? "New chat" : chat.title)
                        .lineLimit(1)
                    if !chat.updatedAt.isEmpty {
                        Text(chat.updatedAt)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
                .tag(chat.id)
                .swipeActions(edge: .trailing) {
                    Button(role: .destructive) {
                        deleting = chat
                    } label: {
                        Label("Delete", systemImage: "trash")
                    }
                    Button {
                        renameText = chat.title
                        renaming = chat
                    } label: {
                        Label("Rename", systemImage: "pencil")
                    }
                    .tint(Theme.cobalt)
                }
                .contextMenu {
                    Button {
                        renameText = chat.title
                        renaming = chat
                    } label: {
                        Label("Rename", systemImage: "pencil")
                    }
                    Button(role: .destructive) {
                        deleting = chat
                    } label: {
                        Label("Delete", systemImage: "trash")
                    }
                }
            }
        }
        .overlay {
            if model.chats.isEmpty {
                ContentUnavailableView(
                    "No chats",
                    systemImage: "bubble.left",
                    description: Text("Start one to talk to this Bot.")
                )
            }
        }
        .alert("Rename chat", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Title", text: $renameText)
            Button("Cancel", role: .cancel) {}
            Button("Rename") {
                if let chat = renaming { Task { await model.renameChat(chat.id, title: renameText) } }
            }
        }
        .confirmationDialog(
            "Delete this chat?",
            isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
            titleVisibility: .visible,
            presenting: deleting
        ) { chat in
            Button("Delete chat", role: .destructive) {
                Task { await model.deleteChat(chat.id) }
            }
        } message: { _ in
            Text("The conversation and its history are removed.")
        }
        .refreshable { await model.refreshChats() }
        .navigationTitle(model.selectedBot?.name ?? "Chats")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    Task { await model.newChat() }
                } label: {
                    Label("New Chat", systemImage: "square.and.pencil")
                }
            }
        }
    }
}
