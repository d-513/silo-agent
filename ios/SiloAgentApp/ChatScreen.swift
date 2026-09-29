import Observation
import SiloClient
import SwiftUI

/// One conversation: the thread fills the screen, the composer floats above it on glass.
struct ChatScreen: View {
    @Environment(AppModel.self) private var model
    let chatID: String

    private var title: String {
        let value = model.chats.first { $0.id == chatID }?.title ?? ""
        return value.isEmpty ? "New chat" : value
    }

    var body: some View {
        ThreadView(events: model.events, busy: model.isRunning)
            .safeAreaInset(edge: .top, spacing: 0) { TaskboardStrip() }
            .safeAreaBar(edge: .bottom, spacing: 0) {
                VStack(spacing: 0) {
                    SubagentTray()
                    ComposerView()
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbarVisibility(.hidden, for: .tabBar)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button { Task { await model.collectMemories() } } label: {
                            Label("Save Memories from This Chat", systemImage: "brain")
                        }
                        Button { Task { await model.compact() } } label: {
                            Label("Compact Conversation", systemImage: "rectangle.compress.vertical")
                        }
                        .disabled(model.isRunning)
                    } label: {
                        Label("More", systemImage: "ellipsis")
                    }
                }
            }
            .task(id: chatID) { model.selectChat(chatID) }
    }
}
