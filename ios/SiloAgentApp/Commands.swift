import SiloClient
import SwiftUI

/// The keyboard on an iPad (or a phone with one): the menu bar's Bot and Go menus, and what holding
/// ⌘ lists. Every item is something a button already does; the keys only reach it faster.
struct SiloCommands: Commands {
    let model: AppModel
    let spaces: SpaceModel

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Chat") { model.newChat() }
                .keyboardShortcut("n")
                .disabled(model.selectedBot == nil)
        }
        CommandMenu("Bot") {
            Button("Main Chat") { model.showMainChat() }
                .keyboardShortcut("0")
                .disabled(model.selectedBot == nil)
            Button("Dashboard") { model.open(.page(.dashboard)) }
                .keyboardShortcut("d", modifiers: [.command, .shift])
                .disabled(model.selectedBot == nil)
            Button("Feed") { model.open(.page(.feed)) }
                .keyboardShortcut("f", modifiers: [.command, .shift])
                .disabled(model.selectedBot == nil)
            Divider()
            Button("Call") { model.call(chat: model.selectedChatID ?? "", screen: true) }
                .keyboardShortcut("c", modifiers: [.command, .shift])
                .disabled(model.selectedBot == nil || !model.callEnabled || CallModel.shared.live)
            Button("Stop Reply") { Task { await model.stopRun() } }
                .keyboardShortcut(".")
                .disabled(!model.isRunning)
        }
        CommandMenu("Go") {
            Button("Bots") { model.sidebarOpen.toggle() }
                .keyboardShortcut("b", modifiers: [.command, .control])
            Button("Bot's Pages") { model.railOpen.toggle() }
                .keyboardShortcut("r", modifiers: [.command, .control])
                .disabled(model.selectedBot == nil)
            Divider()
            // ⌘1…⌘9: the Bots in the sidebar's order, newest message first.
            ForEach(Array(byLastMessage(spaces.bots) { $0.lastMessageAt }.prefix(9).enumerated()), id: \.element.id) { index, bot in
                Button(bot.name) { model.openBot(bot.id) }
                    .keyboardShortcut(KeyEquivalent(Character("\(index + 1)")))
            }
        }
    }
}
