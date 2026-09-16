import Observation
import SiloClient
import SwiftUI

struct ChatsList: View {
    @Environment(AppModel.self) private var model

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
