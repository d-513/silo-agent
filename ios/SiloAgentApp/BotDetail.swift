import Observation
import SiloClient
import SwiftUI

struct BotDetail: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            VStack(spacing: 0) {
                header(bot)
                Divider()
                content
            }
            .navigationTitle(bot.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    tabMenu
                }
            }
            .safeAreaInset(edge: .bottom) {
                if let error = model.errorMessage {
                    Text(error)
                        .font(.footnote)
                        .foregroundStyle(.red)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(10)
                        .background(.bar)
                        .onTapGesture { model.errorMessage = nil }
                }
            }
        } else {
            ContentUnavailableView("No Bot selected", systemImage: "shippingbox")
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.tab {
        case .chat:
            if model.selectedChatID != nil {
                ThreadView()
                ComposerView()
            } else {
                ContentUnavailableView(
                    "No chat selected",
                    systemImage: "bubble.left",
                    description: Text("Pick a chat in the middle column, or start a new one.")
                )
            }
        default:
            PlaceholderPane(tab: model.tab)
        }
    }

    private func header(_ bot: Silo_V1_Bot) -> some View {
        HStack(spacing: 10) {
            CrestView(index: bot.crest, size: 26)
            VStack(alignment: .leading, spacing: 1) {
                Text(bot.name)
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(1)
                StatusLine(status: bot.status)
            }
            Spacer(minLength: 8)
            machineButton(bot)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.bar)
    }

    @ViewBuilder
    private func machineButton(_ bot: Silo_V1_Bot) -> some View {
        if model.startingBotID == bot.id || bot.status == "starting" {
            Button {
            } label: {
                Label("Starting…", systemImage: "power")
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
            .disabled(true)
        } else if bot.workerConnected {
            Button {
                Task { await model.stopBot() }
            } label: {
                Label("Stop Bot", systemImage: "power")
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        } else {
            Button {
                Task { await model.startBot() }
            } label: {
                Label("Start Bot", systemImage: "power")
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
        }
    }

    private var tabMenu: some View {
        Menu {
            ForEach(BotTab.allCases) { tab in
                Button {
                    model.tab = tab
                } label: {
                    Label(tab.title, systemImage: model.tab == tab ? "checkmark" : tab.symbol)
                }
            }
        } label: {
            Label(model.tab.title, systemImage: model.tab.symbol)
        }
    }
}
