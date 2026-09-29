import Observation
import SiloClient
import SwiftUI

/// A Bot's home: who it is, its machine, recent chats, then every page grouped like Settings.
struct BotHub: View {
    @Environment(AppModel.self) private var model

    private static let recent = 3

    var body: some View {
        if let bot = model.selectedBot {
            List {
                header(bot)
                needsYou

                Section("Chats") {
                    Button {
                        Task { await model.newChat() }
                    } label: {
                        Label("New Chat", systemImage: "square.and.pencil")
                            .foregroundStyle(Theme.cobalt)
                    }
                    ForEach(model.chats.prefix(Self.recent), id: \.id) { chat in
                        ChatRow(chat: chat)
                    }
                    if model.chats.count > Self.recent {
                        NavigationLink(value: BotRoute.chats) {
                            Text("All Chats").foregroundStyle(.secondary)
                                .badge(model.chats.count)
                        }
                    }
                }

                Section {
                    row(.automations, Theme.cobalt)
                    row(.memories, Theme.tile(.purple))
                    row(.feed, Theme.tile(.orange), badge: bot.feedUnread)
                }
                Section("Workspace") {
                    row(.files, Theme.emerald)
                    row(.drives, Theme.tile(.teal))
                }
                Section("Connect") {
                    row(.connectors, Theme.tile(.purple))
                    row(.channels, Theme.cobalt)
                    row(.skills, Theme.tile(.orange))
                }
                Section("Safety") {
                    row(.secrets, Theme.tile(.graphite))
                    row(.rules, Theme.tile(.wine))
                }
                Section {
                    row(.container, Theme.tile(.graphite))
                    row(.settings, Theme.tile(.graphite))
                } footer: {
                    Text("Desktop and Console stay on the web client.")
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle("")
            .navigationBarTitleDisplayMode(.inline)
            .refreshable {
                await model.refreshBots()
                await model.refreshChats()
            }
        } else {
            ContentUnavailableView("No Bot selected", systemImage: "shippingbox")
        }
    }

    // MARK: header

    private func header(_ bot: Silo_V1_Bot) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 14) {
                    CrestView(index: bot.crest, size: 56)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(bot.name)
                            .font(.title2.weight(.semibold))
                            .lineLimit(1)
                        StatusLine(status: bot.status)
                    }
                    Spacer(minLength: 8)
                    machineButton(bot)
                }
                if !bot.description_p.isEmpty {
                    Text(bot.description_p)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                }
            }
            .padding(.vertical, 4)
        }
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 4, trailing: 4))
    }

    @ViewBuilder
    private func machineButton(_ bot: Silo_V1_Bot) -> some View {
        if model.startingBotID == bot.id || bot.status == "starting" {
            Button {} label: { Text("Starting…") }
                .buttonStyle(.glass)
                .disabled(true)
        } else if bot.workerConnected {
            Button { Task { await model.stopBot() } } label: { Text("Stop") }
                .buttonStyle(.glass)
        } else {
            Button { Task { await model.startBot() } } label: { Text("Start") }
                .buttonStyle(.glassProminent)
        }
    }

    // MARK: needs you

    /// Brings a swiped-away approval back; the run stays paused until it is decided.
    @ViewBuilder
    private var needsYou: some View {
        if let first = model.approvals.first, first.id == model.dismissedApprovalID {
            Section {
                Button {
                    model.dismissedApprovalID = nil
                } label: {
                    HStack {
                        Label(model.approvals.count > 1 ? "\(model.approvals.count) waiting for you" : "Needs you",
                              systemImage: "hand.raised.fill")
                            .foregroundStyle(Theme.vermilion)
                            .font(.headline)
                        Spacer()
                        Text("Review").foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    // MARK: rows

    private func row(_ tab: BotTab, _ color: Color, badge: Int32 = 0) -> some View {
        NavigationLink(value: BotRoute.page(tab)) {
            Label {
                Text(tab.title)
            } icon: {
                IconTile(symbol: tab.symbol, color: color)
            }
        }
        .badge(Int(badge))
    }
}

/// A Settings-style rounded-square glyph. Colors come from the crest palette so they stay on-brand.
struct IconTile: View {
    let symbol: String
    let color: Color

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: 15, weight: .semibold))
            .foregroundStyle(.white)
            .frame(width: 30, height: 30)
            .background(color.gradient, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            .accessibilityHidden(true)
    }
}
