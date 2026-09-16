import Observation
import SiloClient
import SwiftUI

struct BotsList: View {
    @Environment(AppModel.self) private var model
    @State private var showingNew = false

    var body: some View {
        @Bindable var model = model
        List(selection: $model.selectedBotID) {
            ForEach(model.bots, id: \.id) { bot in
                BotRow(bot: bot)
                    .tag(bot.id)
            }
        }
        .overlay {
            if model.bots.isEmpty && model.botsError == nil {
                ContentUnavailableView(
                    "No Bots yet",
                    systemImage: "shippingbox",
                    description: Text("A Bot is its own machine. It does not share files with the others.")
                )
            }
        }
        .navigationTitle("Bots")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    showingNew = true
                } label: {
                    Label("New Bot", systemImage: "plus")
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            if let error = model.botsError {
                Text(error)
                    .font(.footnote)
                    .foregroundStyle(.red)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(10)
                    .background(.bar)
            }
        }
        .refreshable { await model.refreshBots() }
        .sheet(isPresented: $showingNew) {
            NewBotView()
        }
    }
}

struct BotRow: View {
    let bot: Silo_V1_Bot

    var body: some View {
        HStack(spacing: 12) {
            CrestView(index: bot.crest, size: 36)
            VStack(alignment: .leading, spacing: 3) {
                Text(bot.name)
                    .font(.body.weight(.medium))
                if !bot.description_p.isEmpty {
                    Text(bot.description_p)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                StatusLine(status: bot.status)
            }
            Spacer(minLength: 0)
            if bot.status == "needs_you" {
                Image(systemName: "exclamationmark.circle.fill")
                    .foregroundStyle(.red)
                    .accessibilityLabel("Needs you")
            }
        }
        .padding(.vertical, 2)
    }
}
