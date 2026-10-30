import AppIntents
import SiloClient
import SwiftUI
import WidgetKit

/// One card of a Bot's Dashboard, as it can be picked for the Home Screen.
struct BoardCardEntity: AppEntity, Identifiable, Hashable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Dashboard widget"
    static let defaultQuery = BoardCardQuery()

    var id: String
    var title: String
    var botID: String

    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(title)") }
}

struct BoardCardQuery: EntityQuery {
    @IntentParameterDependency<BoardConfiguration>(\.$bot) var chosen

    func entities(for identifiers: [String]) async throws -> [BoardCardEntity] {
        try await suggestedEntities().filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [BoardCardEntity] {
        let state = SiloStore.load()
        guard let id = chosen?.bot.id ?? state.lastBot?.id, !state.server.isEmpty else { return [] }
        let board = try await SiloClient(host: state.server).listDashboard(botID: id)
        // Only what fits a tile: an app needs its hatch, and a table its width.
        return board.widgets.filter { !["app", "table", "ideas"].contains($0.kind) }
            .map { BoardCardEntity(id: $0.id, title: $0.title.isEmpty ? $0.kind : $0.title, botID: id) }
    }
}

struct BoardConfiguration: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "Dashboard widget"
    static let description = IntentDescription("Puts a widget from a Bot's Dashboard on your Home Screen.")

    @Parameter(title: "Bot") var bot: BotEntity?
    @Parameter(title: "Widget") var card: BoardCardEntity?
}

struct BoardEntry: TimelineEntry {
    var date = Date()
    var title = ""
    var kind = ""
    var data: [String: Any] = [:]
    var age = ""
    var bot: SharedBot?
}

struct BoardProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> BoardEntry {
        BoardEntry(title: "Open issues", kind: "stat", data: ["value": 12, "caption": "this week"])
    }

    func snapshot(for configuration: BoardConfiguration, in context: Context) async -> BoardEntry { await load(configuration) }

    func timeline(for configuration: BoardConfiguration, in context: Context) async -> Timeline<BoardEntry> {
        Timeline(entries: [await load(configuration)], policy: .after(Date().addingTimeInterval(20 * 60)))
    }

    private func load(_ configuration: BoardConfiguration) async -> BoardEntry {
        let state = SiloStore.load()
        guard let bot = picked(configuration.bot, in: state), !state.server.isEmpty,
              let board = try? await SiloClient(host: state.server).listDashboard(botID: bot.id) else { return BoardEntry() }
        let wanted = configuration.card?.id
        guard let card = board.widgets.first(where: { $0.id == wanted }) ?? board.widgets.first(where: { !["app", "table", "ideas"].contains($0.kind) }) else {
            return BoardEntry(bot: bot)
        }
        return BoardEntry(title: card.title, kind: card.kind, data: WidgetJSON.object(card.dataJson), age: widgetAge(card.dataAt), bot: bot)
    }
}

/// The widgets a Bot keeps current, on the Home Screen: code keeps them fresh, so this costs no
/// model a token.
struct BoardWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "silo.board", intent: BoardConfiguration.self, provider: BoardProvider()) { entry in
            BoardView(entry: entry).siloWidget()
        }
        .configurationDisplayName("Dashboard widget")
        .description("A widget from a Bot's Dashboard.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

struct BoardView: View {
    let entry: BoardEntry
    /// Set by the debug gallery, which is no widget.
    var drawnAs: WidgetFamily?
    @Environment(\.widgetFamily) private var environmentFamily
    private var family: WidgetFamily { drawnAs ?? environmentFamily }

    private var limit: Int { family == .systemLarge ? 9 : family == .systemMedium ? 4 : 3 }

    var body: some View {
        if entry.kind.isEmpty {
            if let bot = entry.bot {
                VStack(alignment: .leading, spacing: 4) {
                    CrestView(index: bot.crest, size: 28)
                    Spacer(minLength: 0)
                    Text("Nothing on the board yet").font(.silo(14, .medium)).foregroundStyle(Theme.ink2)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            } else {
                SignedOutWidget()
            }
        } else {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    Text(entry.title).font(.silo(13, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                    Spacer(minLength: 0)
                    if !entry.age.isEmpty { Text(entry.age).font(.siloMono(11)).foregroundStyle(Theme.ink3) }
                }
                content
                Spacer(minLength: 0)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .widgetURL(entry.bot.map { SiloLink.dashboard(bot: $0.id).url })
        }
    }

    @ViewBuilder
    private var content: some View {
        let data = entry.data
        switch entry.kind {
        case "stat":
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(WidgetJSON.display(data["value"])).font(.silo(34, .medium)).foregroundStyle(Theme.ink).lineLimit(1).minimumScaleFactor(0.5)
                Text(WidgetJSON.text(data["unit"])).font(.siloMono(13)).foregroundStyle(Theme.ink3)
            }
            Text(WidgetJSON.text(data["caption"])).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(2)
        case "progress":
            let value = WidgetJSON.number(data["value"]) ?? 0
            let top = max(WidgetJSON.number(data["max"]) ?? 100, 1)
            Text(WidgetJSON.format(value)).font(.silo(28, .medium)).foregroundStyle(Theme.ink)
            GeometryReader { box in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.pressed)
                    Capsule().fill(Theme.ink).frame(width: box.size.width * min(1, value / top))
                }
            }
            .frame(height: 3)
            Text(WidgetJSON.text(data["caption"])).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(2)
        case "note":
            Text(WidgetJSON.text(data["text"])).font(.reply(14)).foregroundStyle(Theme.ink).lineLimit(family == .systemSmall ? 5 : 8)
        case "status":
            rows { item in
                let tone = widgetTone(item["state"])
                HStack(spacing: 7) {
                    Circle().fill(tone == .down ? Theme.vermilion : tone == .ok ? Theme.lamp : Theme.ink3).frame(width: 7, height: 7)
                    Text(WidgetJSON.text(item["name"])).font(.siloMono(12.5)).foregroundStyle(Theme.ink).lineLimit(1)
                }
            }
        default:
            rows { item in
                HStack(spacing: 6) {
                    Text(WidgetJSON.text(item["title"] ?? item["text"] ?? item["name"] ?? item["label"]))
                        .font(.silo(13)).foregroundStyle(Theme.ink).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(WidgetJSON.display(item["value"] ?? item["meta"] ?? item["time"])).font(.siloMono(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
            }
        }
    }

    private func rows(@ViewBuilder _ row: @escaping ([String: Any]) -> some View) -> some View {
        let items = WidgetJSON.items(entry.data["items"]).prefix(limit)
        return VStack(alignment: .leading, spacing: 5) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in row(item) }
        }
    }
}
