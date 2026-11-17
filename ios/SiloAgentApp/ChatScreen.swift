import Observation
import SiloClient
import SwiftUI

/// The chat under the Bot's top bar and its tabs: the taskboard, the thread, and the composer. A
/// run paused on an approval puts its slip where the composer was. Renaming, closing and deleting a
/// chat are on its tab; saving memories and compacting on the composer.
struct ChatScreen: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(spacing: 0) {
            TaskboardStrip()
            Group {
                if model.selectedChatID == nil && model.events.isEmpty {
                    DraftIntro()
                } else {
                    ThreadView(events: model.events, busy: model.isRunning, interactive: !model.readOnly,
                               prepended: model.prepended, earlier: model.earlier, loadingEarlier: model.loadingEarlier,
                               onEarlier: { await model.loadEarlier() })
                }
            }
            .frame(maxHeight: .infinity)
            VStack(spacing: 0) {
                SubagentTray()
                if let approval = model.chatApproval {
                    ApprovalSlip(approval: approval, here: true)
                        .padding(.horizontal, 10)
                        .padding(.bottom, 6)
                        .transition(.slip)
                } else if model.readOnly, let chat = model.selectedChat {
                    ReadOnlyBar(chat: chat)
                } else {
                    ComposerView()
                }
            }
            .animation(Motion.settle(), value: model.chatApproval?.id)
        }
        .background(Theme.canvas.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
    }
}

/// A new chat before its first message: who you are talking to, and nothing else to do but type.
private struct DraftIntro: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            VStack(alignment: .leading, spacing: 12) {
                Spacer()
                CrestView(index: bot.crest, size: 44, state: crestState(bot))
                Text(bot.name).font(.silo(26, .medium)).foregroundStyle(Theme.ink)
                if !bot.description_p.isEmpty {
                    Text(bot.description_p)
                        .font(.silo(15))
                        .foregroundStyle(Theme.ink2)
                        .lineLimit(4)
                }
                if bot.machine != "online" {
                    Text("Its machine is stopped. Sending a message starts it.")
                        .font(.silo(13))
                        .foregroundStyle(Theme.ink3)
                }
                if bot.people.count > 1 {
                    // The trust line: on a Bot several people use, managers read every chat.
                    Text("\(bot.name)'s managers can read this chat. What you tell \(bot.name), it may remember for everyone who uses it.")
                        .font(.silo(13))
                        .foregroundStyle(Theme.ink3)
                }
                Spacer()
                Spacer()
            }
            .padding(.horizontal, 28)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .rise()
        }
    }
}

/// In place of the composer on someone else's chat (or the Bot's own): who writes here, and Stop.
private struct ReadOnlyBar: View {
    @Environment(AppModel.self) private var model
    let chat: Silo_V1_Chat

    var body: some View {
        HStack(spacing: 12) {
            Text(chat.author.isEmpty
                 ? "\(model.selectedBot?.name ?? "The Bot")'s own chat. You can read it and stop it."
                 : "\(chat.author)'s chat. You can read it and stop it; only \(chat.author) writes in it.")
                .font(.silo(13))
                .foregroundStyle(Theme.ink3)
                .frame(maxWidth: .infinity, alignment: .leading)
            if model.isRunning {
                Button { Task { await model.stopRun() } } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "stop.fill").font(.system(size: 10))
                        Text("Stop")
                    }
                }
                .buttonStyle(.quiet(tint: Theme.vermilion))
                .controlSize(.small)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }
}
