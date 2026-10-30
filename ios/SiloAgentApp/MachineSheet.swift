import Observation
import SiloClient
import SwiftUI

/// The web's machine rail as a slip off the chat's head: the box's state with Start or Stop, then
/// the panes that look at it. The chat stays underneath.
struct MachineSheet: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 12) {
                    CrestView(index: bot.crest, size: 34)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(bot.name).font(.silo(16, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        StatusWord(look: machineLook(bot.machine))
                    }
                    Spacer(minLength: 8)
                    power(bot)
                }
                .padding(.horizontal, 18)
                .padding(.top, 20)
                .padding(.bottom, 14)
                row(.files, detail: "/workspace")
                row(.changes, detail: nil)
                if !bot.mailAddress.isEmpty { row(.mail, detail: bot.mailAddress) }
                row(.container, detail: nil)
                Text("Desktop and Console are on the web client.")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .padding(.horizontal, 18)
                    .padding(.top, 12)
                Spacer(minLength: 0)
            }
            .siloSheet(detents: [.height(bot.mailAddress.isEmpty ? 306 : 356)])
        }
    }

    @ViewBuilder
    private func power(_ bot: Silo_V1_Bot) -> some View {
        if model.startingBotID == bot.id || bot.machine == "starting" {
            Button {} label: {
                HStack(spacing: 6) {
                    Spinner(size: 13)
                    Text("Starting…")
                }
            }
            .buttonStyle(.quiet)
            .controlSize(.small)
            .disabled(true)
        } else if bot.machine == "online" {
            Button { Task { await model.stopBot() } } label: {
                HStack(spacing: 6) {
                    Text("Stop Bot")
                    Image(systemName: "power").font(.system(size: 12, weight: .medium))
                }
            }
            .buttonStyle(.quiet)
            .controlSize(.small)
        } else {
            Button { Task { await model.startBot() } } label: {
                HStack(spacing: 6) {
                    Text("Start Bot")
                    Image(systemName: "power").font(.system(size: 12, weight: .medium))
                }
            }
            .buttonStyle(.ink)
            .controlSize(.small)
        }
    }

    private func row(_ tab: BotTab, detail: String?) -> some View {
        Button {
            model.machineOpen = false
            model.sidebarOpen = false
            model.path = [.page(tab)]
        } label: {
            HStack(spacing: 12) {
                Image(systemName: tab.symbol).font(.system(size: 15)).foregroundStyle(Theme.ink2).frame(width: 22)
                Text(tab.title).font(.silo(15, .medium)).foregroundStyle(Theme.ink)
                Spacer(minLength: 8)
                if let detail {
                    Text(detail).font(.siloMono(12)).foregroundStyle(Theme.ink3).lineLimit(1).truncationMode(.middle)
                }
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 18)
            .frame(height: 50)
            .overlay(alignment: .top) { Theme.line.frame(height: 1) }
            .contentShape(Rectangle())
        }
        .buttonStyle(RowPressStyle())
    }
}
