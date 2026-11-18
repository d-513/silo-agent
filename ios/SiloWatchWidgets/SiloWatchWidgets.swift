import AppIntents
import SiloClient
import SwiftUI
import WidgetKit

// Watch-face complications: one Bot's state, and what waits on you across Bots. They read the
// watch's shared state and refresh it from the server themselves; a failed refresh keeps the last
// state, so a face never shows an error, only something a little old.

@main
struct SiloWatchWidgets: WidgetBundle {
    init() { SiloGroup.boot() }

    var body: some Widget {
        WatchBotComplication()
        NeedsYouComplication()
    }
}

struct WatchBotConfiguration: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "Bot"
    static let description = IntentDescription("Which Bot to show.")

    @Parameter(title: "Bot") var bot: BotEntity?

    init() {}
    init(bot: BotEntity?) { self.bot = bot }
}

struct WatchEntry: TimelineEntry {
    var date = Date()
    var state: SharedState
    var bot: SharedBot?

    var relevance: TimelineEntryRelevance? {
        let waiting = bot.map { $0.needs + $0.asks } ?? state.approvals.count
        return TimelineEntryRelevance(score: waiting > 0 ? 100 : bot?.look.lamp == .working ? 30 : 0)
    }
}

/// The newest state the server gives, else the last one kept.
private func freshState() async -> SharedState {
    let kept = SiloStore.load()
    guard let next = try? await SiloFeed.load(kept) else { return kept }
    SiloStore.save(next)
    return next
}

private func pick(_ entity: BotEntity?, in state: SharedState) -> SharedBot? {
    state.bots.first { $0.id == entity?.id } ?? state.bots.first { $0.needs > 0 } ?? state.lastBot
}

struct WatchBotProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> WatchEntry { WatchEntry(state: SiloStore.load(), bot: SiloStore.load().lastBot) }

    func snapshot(for configuration: WatchBotConfiguration, in context: Context) async -> WatchEntry {
        let state = SiloStore.load()
        return WatchEntry(state: state, bot: pick(configuration.bot, in: state))
    }

    func timeline(for configuration: WatchBotConfiguration, in context: Context) async -> Timeline<WatchEntry> {
        let state = await freshState()
        return Timeline(entries: [WatchEntry(state: state, bot: pick(configuration.bot, in: state))],
                        policy: .after(Date().addingTimeInterval(15 * 60)))
    }

    /// A watch face has no configuration screen of its own: these are the choices it offers.
    func recommendations() -> [AppIntentRecommendation<WatchBotConfiguration>] {
        let bots = SiloStore.load().bots
        return [AppIntentRecommendation(intent: WatchBotConfiguration(bot: nil), description: "Your Bots")]
            + bots.prefix(8).map { AppIntentRecommendation(intent: WatchBotConfiguration(bot: BotEntity($0)), description: "\($0.name)") }
    }
}

struct WatchBotComplication: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "silo.watch.bot", intent: WatchBotConfiguration.self, provider: WatchBotProvider()) { entry in
            WatchBotFace(entry: entry).containerBackground(.clear, for: .widget)
        }
        .configurationDisplayName("Bot")
        .description("A Bot's state, and when it needs you.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline, .accessoryCorner])
    }
}

private struct WatchBotFace: View {
    let entry: WatchEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        if let bot = entry.bot {
            switch family {
            case .accessoryCircular:
                ZStack {
                    AccessoryWidgetBackground()
                    CrestView(index: bot.crest, size: 30, cutout: true)
                    if bot.needs > 0 { badge(bot.needs) }
                }
                .accessibilityLabel("\(bot.name), \(bot.look.word)")
            case .accessoryCorner:
                CrestView(index: bot.crest, size: 22, cutout: true)
                    .widgetLabel { Text(bot.needs > 0 ? "\(bot.needs) need you" : bot.look.word) }
            case .accessoryInline:
                Text(bot.needs > 0 ? "\(bot.name) · \(bot.needs) need you" : "\(bot.name) · \(bot.look.word)")
            default:
                HStack(spacing: 6) {
                    CrestView(index: bot.crest, size: 26, cutout: true)
                    VStack(alignment: .leading, spacing: 0) {
                        Text(bot.name).font(.headline).lineLimit(1).widgetAccentable()
                        Text(bot.needs > 0 ? "\(bot.needs) need you" : bot.look.word).font(.caption).lineLimit(1)
                        if !bot.step.isEmpty { Text(bot.step).font(.caption2.monospacedDigit()).foregroundStyle(.secondary) }
                    }
                    Spacer(minLength: 0)
                }
            }
        } else {
            switch family {
            case .accessoryInline: Text("Silo")
            case .accessoryCircular, .accessoryCorner: Image(systemName: "iphone").widgetLabel("Silo")
            default:
                VStack(alignment: .leading) {
                    Text("Silo").font(.headline)
                    Text("Open Silo on your iPhone").font(.caption).lineLimit(2)
                }
            }
        }
    }

    private func badge(_ count: Int) -> some View {
        Text("\(count)")
            .font(.system(size: 11, weight: .bold))
            .frame(minWidth: 15, minHeight: 15)
            .background(Circle().fill(Theme.vermilion))
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topTrailing)
    }
}

// MARK: needs you

struct NeedsYouProvider: TimelineProvider {
    func placeholder(in context: Context) -> WatchEntry { WatchEntry(state: SiloStore.load()) }
    func getSnapshot(in context: Context, completion: @escaping (WatchEntry) -> Void) { completion(WatchEntry(state: SiloStore.load())) }
    func getTimeline(in context: Context, completion: @escaping (Timeline<WatchEntry>) -> Void) {
        Task {
            let state = await freshState()
            completion(Timeline(entries: [WatchEntry(state: state)], policy: .after(Date().addingTimeInterval(15 * 60))))
        }
    }
}

struct NeedsYouComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "silo.watch.needsyou", provider: NeedsYouProvider()) { entry in
            NeedsYouFace(entry: entry).containerBackground(.clear, for: .widget)
        }
        .configurationDisplayName("Needs you")
        .description("What your Bots are waiting on you for.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

private struct NeedsYouFace: View {
    let entry: WatchEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        let waiting = entry.state.approvals
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                VStack(spacing: 0) {
                    Image(systemName: waiting.isEmpty ? "checkmark" : "hand.raised.fill").font(.system(size: 14, weight: .semibold))
                    if !waiting.isEmpty { Text("\(waiting.count)").font(.system(size: 15, weight: .bold)).widgetAccentable() }
                }
            }
        case .accessoryInline:
            Text(waiting.isEmpty ? "Nothing needs you" : "\(waiting.count) need you")
        default:
            VStack(alignment: .leading, spacing: 1) {
                Text(waiting.isEmpty ? "Nothing needs you" : waiting.count == 1 ? "1 needs you" : "\(waiting.count) need you")
                    .font(.headline).widgetAccentable()
                if let first = waiting.first {
                    Text(first.botName).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    Text(first.title).font(.caption).lineLimit(2)
                } else if entry.state.signedIn {
                    Text("Your Bots are on their own.").font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("Open Silo on your iPhone").font(.caption).foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
