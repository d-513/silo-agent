import Observation
import SiloClient
import SwiftUI

/// The web's machine rail as a sheet off the status lamp: the Bot's state with Start or Stop, then
/// the panes that look at its machine. The chat stays underneath.
struct MachineSheet: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 12) {
                    CrestView(index: bot.crest, size: 34)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(bot.name).font(.silo(17, .semibold)).lineLimit(1)
                        StatusLine(status: bot.status)
                    }
                    Spacer(minLength: 8)
                    power(bot)
                }
                .padding(.horizontal, 20)
                .padding(.top, 22)
                .padding(.bottom, 14)

                row(.files, detail: "/workspace")
                row(.changes, detail: nil)
                if !bot.mailAddress.isEmpty { row(.mail, detail: bot.mailAddress) }
                row(.container, detail: nil)

                Text("Desktop and Console are on the web client.")
                    .font(.silo(13))
                    .foregroundStyle(Theme.ink3)
                    .padding(.horizontal, 20)
                    .padding(.top, 12)
                Spacer(minLength: 0)
            }
            .background(Theme.surface.ignoresSafeArea())
            .presentationDetents([.height(bot.mailAddress.isEmpty ? 300 : 350)])
            .presentationDragIndicator(.visible)
            .presentationBackground(Theme.surface)
        }
    }

    @ViewBuilder
    private func power(_ bot: Silo_V1_Bot) -> some View {
        if model.startingBotID == bot.id || bot.status == "starting" {
            Button {} label: { Text("Starting…") }
                .buttonStyle(.quiet)
                .disabled(true)
        } else if bot.workerConnected {
            Button { Task { await model.stopBot() } } label: { Label("Stop", systemImage: "power") }
                .buttonStyle(.quiet)
        } else {
            Button { Task { await model.startBot() } } label: { Label("Start", systemImage: "power") }
                .buttonStyle(.ink)
        }
    }

    private func row(_ tab: BotTab, detail: String?) -> some View {
        Button {
            model.machineOpen = false
            model.sidebarOpen = false
            model.path = [.page(tab)]
        } label: {
            HStack(spacing: 14) {
                Image(systemName: tab.symbol)
                    .font(.system(size: 16))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 22)
                Text(tab.title).font(.silo(15, .medium)).foregroundStyle(Theme.ink)
                Spacer(minLength: 8)
                if let detail {
                    Text(detail)
                        .font(.siloMono(12))
                        .foregroundStyle(Theme.ink3)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Image(systemName: "chevron.right").font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 20)
            .frame(height: 50)
            .overlay(alignment: .top) { Theme.line.frame(height: 1) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
