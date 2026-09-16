import Observation
import SwiftUI

struct RootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        NavigationSplitView {
            BotsList()
        } content: {
            if model.selectedBot != nil {
                ChatsList()
            } else {
                ContentUnavailableView("Select a Bot", systemImage: "shippingbox")
            }
        } detail: {
            if model.selectedBot != nil {
                BotDetail()
            } else {
                ContentUnavailableView(
                    "No Bot selected",
                    systemImage: "shippingbox",
                    description: Text("Bots are listed in the sidebar.")
                )
            }
        }
        .onChange(of: model.selectedBotID) { _, newValue in
            Task { await model.chooseBot(newValue) }
        }
        .onChange(of: model.selectedChatID) { _, newValue in
            if let newValue { model.selectChat(newValue) }
        }
    }
}
