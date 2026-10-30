#if DEBUG
import SiloClient
import SwiftUI
import WidgetKit

/// The widgets and call controls drawn in the app, for a look at them from a simulator, which has
/// no way to place one on its Home Screen from outside. `SILO_DEV_PATH=gallery`.
struct WidgetGallery: View {
    @Environment(\.dismiss) private var dismiss

    private static let bots: [SharedBot] = [
        SharedBot(id: "1", name: "Scout", crest: packCrest(shape: 0, color: 9), look: StatusLook(lamp: .needsYou, word: "Needs you", tone: .vermilion), needs: 2, asks: 1, unread: 3),
        SharedBot(id: "2", name: "Atlas", crest: packCrest(shape: 2, color: 7), look: StatusLook(lamp: .working, word: "Write the report", tone: .emerald), step: "2/5", nextName: "Morning brief", nextAt: Date().addingTimeInterval(7200)),
        SharedBot(id: "3", name: "Mira", crest: packCrest(shape: 5, color: 5), look: StatusLook(lamp: .none, word: "Last active 2 h ago", tone: .ink3), nextName: "Weekly digest", nextAt: Date().addingTimeInterval(90000)),
    ]

    private var state: SharedState {
        var state = SharedState()
        state.email = "you@silo"
        state.callsOn = true
        state.bots = Self.bots
        state.approvals = [SharedApproval(id: "a", botID: "1", botName: "Scout", crest: Self.bots[0].crest, title: "Read a secret", summary: "vendor_password, for the invoice portal")]
        return state
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack(spacing: 12) {
                    frame(.systemSmall) { BotWidgetView(entry: SiloEntry(state: state, bot: Self.bots[1]), drawnAs: .systemSmall) }
                    frame(.systemSmall) { BoardView(entry: BoardEntry(title: "Open issues", kind: "stat", data: ["value": 128, "unit": "open", "caption": "12 fewer than last week"], age: "4m"), drawnAs: .systemSmall) }
                }
                frame(.systemMedium) { BotWidgetView(entry: SiloEntry(state: state, bot: Self.bots[0]), drawnAs: .systemMedium) }
                frame(.systemMedium) { NeedsYouView(state: state) }
                frame(.systemMedium) { BotsView(state: state, drawnAs: .systemMedium) }
                callCard
            }
            .padding(16)
        }
        .background(Theme.well.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
    }

    private func frame(_ family: WidgetFamily, @ViewBuilder content: () -> some View) -> some View {
        let size: CGSize = family == .systemSmall ? CGSize(width: 170, height: 170) : CGSize(width: 364, height: 170)
        return content()
            .padding(16)
            .frame(width: size.width, height: size.height)
            .background(Theme.canvas, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
    }

    private var callCard: some View {
        let state = CallActivityAttributes.ContentState(tone: .bot, words: "Scout is speaking", caption: "I found three flights under your budget.", muted: false, routeSymbol: "speaker.wave.2.fill", speaker: true, startedAt: Date().addingTimeInterval(-74), ended: false)
        return VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                CrestView(index: Self.bots[0].crest, size: 42)
                VStack(alignment: .leading, spacing: 1) {
                    Text("Scout").font(.silo(15, .semibold)).foregroundStyle(Theme.onHatch)
                    Text(state.words).font(.silo(13)).foregroundStyle(Theme.onHatch.opacity(0.75))
                }
                Spacer()
                Text(state.startedAt, style: .timer).font(.siloMono(13)).foregroundStyle(Theme.onHatch.opacity(0.8)).frame(width: 60)
            }
            Text(state.caption).font(.reply(15)).foregroundStyle(Theme.onHatch)
            CallButtons(state: state)
        }
        .padding(16)
        .background(Theme.hatch, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
    }
}
#endif
