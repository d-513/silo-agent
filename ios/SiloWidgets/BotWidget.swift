import AppIntents
import SiloClient
import SwiftUI
import WidgetKit

struct BotConfiguration: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "Bot"
    static let description = IntentDescription("Which Bot to show.")

    @Parameter(title: "Bot") var bot: BotEntity?
}

struct SiloEntry: TimelineEntry {
    var date = Date()
    var state: SharedState
    var bot: SharedBot?
}

struct BotProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> SiloEntry { SiloEntry(state: SiloStore.load(), bot: SiloStore.load().lastBot) }

    func snapshot(for configuration: BotConfiguration, in context: Context) async -> SiloEntry {
        let state = SiloStore.load()
        return SiloEntry(state: state, bot: picked(configuration.bot, in: state))
    }

    func timeline(for configuration: BotConfiguration, in context: Context) async -> Timeline<SiloEntry> {
        let state = await SiloFeed.fresh()
        SiloStore.save(state)
        let entry = SiloEntry(state: state, bot: picked(configuration.bot, in: state))
        return Timeline(entries: [entry], policy: .after(Date().addingTimeInterval(15 * 60)))
    }
}

/// One Bot: its face, what it is doing, what waits for you, and a call a press away. On the Lock
/// Screen and in StandBy too.
struct BotWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "silo.bot", intent: BotConfiguration.self, provider: BotProvider()) { entry in
            BotWidgetView(entry: entry).siloWidget()
        }
        .configurationDisplayName("Bot")
        .description("What a Bot is doing, and a call a press away.")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

struct BotWidgetView: View {
    let entry: SiloEntry
    /// Set by the debug gallery, which is no widget.
    var drawnAs: WidgetFamily?
    @Environment(\.widgetFamily) private var environmentFamily
    private var family: WidgetFamily { drawnAs ?? environmentFamily }

    var body: some View {
        if let bot = entry.bot {
            switch family {
            case .accessoryCircular: circular(bot)
            case .accessoryRectangular: rectangular(bot)
            case .accessoryInline: Label("\(bot.name) · \(bot.look.word)", systemImage: "circle.fill")
            case .systemMedium: medium(bot)
            default: small(bot)
            }
        } else if family == .systemSmall || family == .systemMedium {
            SignedOutWidget()
        } else {
            Text("Silo")
        }
    }

    private func small(_ bot: SharedBot) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top) {
                crest(bot, size: 38)
                Spacer(minLength: 0)
                if entry.state.callsOn { callButton(bot) }
            }
            Spacer(minLength: 4)
            Text(bot.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
            DoingLine(look: bot.look, step: bot.step, font: .silo(12.5))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .widgetURL(SiloLink.mainChat(bot: bot.id).url)
    }

    private func medium(_ bot: SharedBot) -> some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 0) {
                crest(bot, size: 38)
                Spacer(minLength: 4)
                Text(bot.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                DoingLine(look: bot.look, step: bot.step, font: .silo(12.5))
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            VStack(alignment: .leading, spacing: 6) {
                waits(bot)
                Spacer(minLength: 0)
                HStack(spacing: 6) {
                    if entry.state.callsOn { callButton(bot, wide: true) }
                    Link(destination: SiloLink.newChat(bot: bot.id).url) {
                        Image(systemName: "plus")
                            .font(.system(size: 14, weight: .medium))
                            .foregroundStyle(Theme.ink)
                            .frame(maxWidth: .infinity)
                            .frame(height: 36)
                            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.line))
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        }
        .widgetURL(SiloLink.mainChat(bot: bot.id).url)
    }

    @ViewBuilder
    private func waits(_ bot: SharedBot) -> some View {
        if bot.needs > 0 {
            Link(destination: SiloLink.needsYou.url) {
                HStack(spacing: 6) {
                    Circle().fill(Theme.vermilion).frame(width: 6, height: 6)
                    Text("\(bot.needs) need you").font(.silo(12.5, .medium)).foregroundStyle(Theme.vermilion)
                }
            }
        }
        if bot.asks > 0 {
            Link(destination: SiloLink.dashboard(bot: bot.id).url) {
                HStack(spacing: 5) {
                    Image(systemName: "questionmark.bubble").font(.system(size: 11))
                    Text(bot.asks == 1 ? "1 question" : "\(bot.asks) questions").font(.silo(12.5, .medium))
                }
                .foregroundStyle(Theme.cobalt)
            }
        }
        if bot.unread > 0 {
            Link(destination: SiloLink.feed(bot: bot.id).url) {
                HStack(spacing: 5) {
                    Image(systemName: "tray").font(.system(size: 11))
                    Text("\(bot.unread) new in the Feed").font(.silo(12.5)).lineLimit(1)
                }
                .foregroundStyle(Theme.cobalt)
            }
        }
        if bot.needs == 0, bot.asks == 0, bot.unread == 0, let next = bot.nextAt {
            VStack(alignment: .leading, spacing: 1) {
                Text(bot.nextName).font(.silo(12.5)).foregroundStyle(Theme.ink2).lineLimit(1)
                Text(next, format: .dateTime.weekday(.abbreviated).hour().minute()).font(.siloMono(11.5)).foregroundStyle(Theme.ink3)
            }
        }
    }

    private func crest(_ bot: SharedBot, size: CGFloat) -> some View {
        CrestView(index: bot.crest, size: size)
            .overlay(alignment: .bottomTrailing) { Lamp(status: bot.look.lamp, ground: Theme.canvas).offset(x: 2, y: 2) }
    }

    private func callButton(_ bot: SharedBot, wide: Bool = false) -> some View {
        Button(intent: CallBotIntent(bot: BotEntity(bot))) {
            HStack(spacing: 6) {
                Image(systemName: "phone.fill").font(.system(size: 13))
                if wide { Text("Call").font(.silo(13, .medium)) }
            }
            .foregroundStyle(Theme.onInk)
            .frame(maxWidth: wide ? .infinity : nil)
            .frame(width: wide ? nil : 36, height: 36)
            .background(Theme.ink, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        }
        .buttonStyle(.plain)
    }

    private func circular(_ bot: SharedBot) -> some View {
        ZStack {
            AccessoryWidgetBackground()
            CrestShape(shape: unpackCrest(bot.crest).shape).frame(width: 34, height: 34)
            CrestEyes(shape: unpackCrest(bot.crest).shape).frame(width: 34, height: 34).blendMode(.destinationOut)
        }
        .widgetURL(SiloLink.mainChat(bot: bot.id).url)
        .accessibilityLabel("\(bot.name), \(bot.look.word)")
    }

    private func rectangular(_ bot: SharedBot) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(bot.name).font(.silo(14, .semibold)).lineLimit(1)
            Text(bot.look.word + (bot.step.isEmpty ? "" : " \(bot.step)")).font(.silo(13)).lineLimit(2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .widgetURL(SiloLink.mainChat(bot: bot.id).url)
    }
}

/// Every Bot at a glance: who needs you, who is at work, who rests.
struct BotsWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "silo.bots", intent: BotConfiguration.self, provider: BotProvider()) { entry in
            BotsView(state: entry.state).siloWidget()
        }
        .configurationDisplayName("Your Bots")
        .description("Who needs you, who is at work, who rests.")
        .supportedFamilies([.systemMedium, .systemLarge])
    }
}

struct BotsView: View {
    let state: SharedState
    /// Set by the debug gallery, which is no widget.
    var drawnAs: WidgetFamily?
    @Environment(\.widgetFamily) private var environmentFamily
    private var family: WidgetFamily { drawnAs ?? environmentFamily }

    var body: some View {
        if state.bots.isEmpty {
            SignedOutWidget()
        } else {
            let rows = order(state.bots).prefix(family == .systemLarge ? 8 : 3)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, bot in
                    if index > 0 { Theme.line.frame(height: 1).padding(.leading, 40) }
                    Link(destination: SiloLink.mainChat(bot: bot.id).url) {
                        HStack(spacing: 10) {
                            CrestView(index: bot.crest, size: 28)
                            VStack(alignment: .leading, spacing: 1) {
                                Text(bot.name).font(.silo(14, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                                DoingLine(look: bot.look, step: bot.step, font: .silo(12))
                            }
                            Spacer(minLength: 0)
                        }
                        .frame(maxHeight: .infinity)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }

    private func order(_ bots: [SharedBot]) -> [SharedBot] {
        func rank(_ bot: SharedBot) -> Int {
            switch bot.look.lamp {
            case .needsYou: return 0
            case .working, .online, .starting: return 1
            default: return bot.asks > 0 ? 1 : 2
            }
        }
        return bots.enumerated().sorted { (rank($0.element), $0.offset) < (rank($1.element), $1.offset) }.map(\.element)
    }
}

/// The oldest approval, answered right here: the Bot's run goes on without opening the app.
struct NeedsYouWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "silo.needsyou", intent: BotConfiguration.self, provider: BotProvider()) { entry in
            NeedsYouView(state: entry.state).siloWidget()
        }
        .configurationDisplayName("Needs you")
        .description("Allow or deny what a Bot is waiting on, from the Home Screen.")
        .supportedFamilies([.systemMedium])
    }
}

struct NeedsYouView: View {
    let state: SharedState

    var body: some View {
        if let approval = state.approvals.first {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 7) {
                    CrestView(index: approval.crest, size: 20)
                    Lamp(status: .needsYou)
                    Text(state.approvals.count > 1 ? "\(approval.botName) · 1 of \(state.approvals.count)" : "\(approval.botName) needs you")
                        .font(.silo(12.5, .medium)).foregroundStyle(Theme.vermilion).lineLimit(1)
                }
                Text(approval.title).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                Text(approval.summary).font(.silo(12.5)).foregroundStyle(Theme.ink2).lineLimit(1)
                Spacer(minLength: 0)
                HStack(spacing: 8) {
                    Button(intent: DecideApprovalIntent(approvalID: approval.id, decision: "allow_once")) {
                        Text("Allow once").font(.silo(13, .medium)).foregroundStyle(Theme.onInk)
                            .frame(maxWidth: .infinity).frame(height: 34)
                            .background(Theme.ink, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                    }
                    Button(intent: DecideApprovalIntent(approvalID: approval.id, decision: "deny")) {
                        Text("Deny").font(.silo(13, .medium)).foregroundStyle(Theme.vermilion)
                            .frame(maxWidth: .infinity).frame(height: 34)
                            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.line))
                    }
                }
                .buttonStyle(.plain)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            .widgetURL(SiloLink.mainChat(bot: approval.botID).url)
        } else if state.signedIn {
            VStack(spacing: 6) {
                Image(systemName: "checkmark.shield").font(.system(size: 22)).foregroundStyle(Theme.emerald)
                Text("Nothing needs you").font(.silo(14, .medium)).foregroundStyle(Theme.ink2)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            SignedOutWidget()
        }
    }
}
