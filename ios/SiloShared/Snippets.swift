import AppIntents
import SiloClient
import SwiftUI

// The cards Siri and Shortcuts show in place of a line of text: a Bot's face with its reply or its
// state, and what waits on you with Allow once and Deny on it. Buttons run intents and the card
// redraws from what the server says next (`reload`), so nothing here keeps its own copy.

/// What waits on you, answered on the card. The words are the app's own description of the call
/// (`describeApproval`), never a model's, and the only yes is Allow once: Always stays in the app.
struct ApprovalsSnippet: SnippetIntent {
    static let title: LocalizedStringResource = "Approvals"
    static let isDiscoverable = false

    func perform() async throws -> some IntentResult & ShowsSnippetView {
        .result(view: ApprovalsCard(approvals: SiloStore.load().approvals))
    }
}

/// A Bot's reply to "Ask a Bot", under its face.
struct ReplySnippet: SnippetIntent {
    static let title: LocalizedStringResource = "Reply"
    static let isDiscoverable = false

    @Parameter(title: "Bot") var botID: String
    @Parameter(title: "Reply") var text: String

    init() {}
    init(botID: String, text: String) {
        self.botID = botID
        self.text = text
    }

    func perform() async throws -> some IntentResult & ShowsSnippetView {
        let bot = SiloStore.load().bots.first { $0.id == botID }
        return .result(view: ReplyCard(bot: bot, text: text))
    }
}

/// A Bot as "Check on a Bot" finds it: what it is doing, what waits, what runs next, and a call or
/// the app a press away.
struct BotStatusSnippet: SnippetIntent {
    static let title: LocalizedStringResource = "Bot"
    static let isDiscoverable = false

    @Parameter(title: "Bot") var botID: String

    init() {}
    init(botID: String) { self.botID = botID }

    func perform() async throws -> some IntentResult & ShowsSnippetView {
        let state = SiloStore.load()
        return .result(view: BotCard(bot: state.bots.first { $0.id == botID }, callsOn: state.callsOn))
    }
}

// MARK: views

private struct CardHead: View {
    let bot: SharedBot?
    var line: String = ""
    var tone: Color = Theme.ink3

    var body: some View {
        HStack(spacing: 10) {
            CrestView(index: bot?.crest ?? 0, size: 30)
            VStack(alignment: .leading, spacing: 1) {
                Text(bot?.name ?? "Silo").font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                if !line.isEmpty { Text(line).font(.silo(12.5)).foregroundStyle(tone).lineLimit(1) }
            }
            Spacer(minLength: 0)
        }
    }
}

private struct ReplyCard: View {
    let bot: SharedBot?
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            CardHead(bot: bot)
            Text(Self.prose(text)).font(.reply(15.5)).foregroundStyle(Theme.ink).lineLimit(14)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(16)
    }

    /// Markdown's inline marks, without the blocks a card cannot draw.
    static func prose(_ text: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
    }
}

private struct BotCard: View {
    let bot: SharedBot?
    let callsOn: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let bot {
                CardHead(bot: bot, line: [bot.look.word, bot.step].filter { !$0.isEmpty }.joined(separator: " "),
                         tone: bot.needs + bot.asks > 0 ? Theme.vermilion : Theme.ink2)
                let waits = [bot.needs > 0 ? "\(bot.needs) to approve" : "", bot.asks > 0 ? "\(bot.asks) to answer" : "",
                             bot.unread > 0 ? "\(bot.unread) unread" : ""].filter { !$0.isEmpty }
                if !waits.isEmpty {
                    Text(waits.joined(separator: " · ")).font(.silo(13)).foregroundStyle(Theme.ink2)
                }
                if !bot.nextName.isEmpty, let at = bot.nextAt {
                    Text("Next: \(bot.nextName), \(at, style: .relative)").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                }
                HStack(spacing: 8) {
                    if callsOn {
                        Button(intent: CallBotIntent(bot: BotEntity(bot))) { pill("Call", symbol: "phone.fill", filled: true) }
                    }
                    Button(intent: OpenBotIntent(bot: BotEntity(bot), place: bot.needs + bot.asks > 0 ? .needsYou : .chat)) {
                        pill(bot.needs + bot.asks > 0 ? "See what waits" : "Open chat", symbol: "arrow.up.right", filled: !callsOn)
                    }
                }
                .buttonStyle(.plain)
            } else {
                CardHead(bot: nil, line: "Open Silo and sign in first.")
            }
        }
        .padding(16)
    }
}

private struct ApprovalsCard: View {
    let approvals: [SharedApproval]

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if approvals.isEmpty {
                HStack(spacing: 8) {
                    Image(systemName: "checkmark.circle").foregroundStyle(Theme.emerald)
                    Text("Nothing is waiting on you.").font(.silo(14)).foregroundStyle(Theme.ink2)
                }
            }
            ForEach(approvals.prefix(3)) { approval in
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 8) {
                        CrestView(index: approval.crest, size: 22)
                        Text(approval.botName).font(.silo(12.5, .medium)).foregroundStyle(Theme.ink2).lineLimit(1)
                    }
                    Text(approval.title).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(2)
                    if !approval.summary.isEmpty {
                        Text(approval.summary).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(3)
                    }
                    HStack(spacing: 8) {
                        Button(intent: DecideApprovalIntent(approvalID: approval.id, decision: "allow_once")) {
                            pill("Allow once", symbol: nil, filled: true)
                        }
                        Button(intent: DecideApprovalIntent(approvalID: approval.id, decision: "deny")) {
                            pill("Deny", symbol: nil, filled: false, tint: Theme.vermilion)
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
            if approvals.count > 3 {
                Text("\(approvals.count - 3) more in Silo").font(.silo(12.5)).foregroundStyle(Theme.ink3)
            }
        }
        .padding(16)
    }
}

private func pill(_ title: LocalizedStringKey, symbol: String?, filled: Bool, tint: Color = Theme.ink) -> some View {
    HStack(spacing: 6) {
        if let symbol { Image(systemName: symbol).font(.system(size: 12, weight: .semibold)) }
        Text(title).font(.silo(13.5, .medium))
    }
    .foregroundStyle(filled ? Theme.onInk : tint)
    .frame(maxWidth: .infinity)
    .frame(height: 38)
    .background(filled ? Theme.ink : Theme.pressed, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
}
