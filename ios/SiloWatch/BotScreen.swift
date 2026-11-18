import SiloClient
import SwiftUI
import WatchKit

/// One Bot: what it is doing, what waits, a message dictated (or scribbled) to its main chat with
/// the reply under it, and its newest Feed posts.
struct BotScreen: View {
    @Environment(WatchModel.self) private var model
    let botID: String

    private enum Message: Equatable {
        case idle
        case sending(String)
        case done(String, WatchModel.AskOutcome)
    }

    @State private var message = Message.idle
    @State private var feed: Result<[Silo_V1_FeedPost], ReachProblem>?

    var body: some View {
        List {
            if let bot = model.bot(botID) {
                Section { head(bot) }
                let waiting = model.state.approvals.filter { $0.botID == botID }
                if !waiting.isEmpty {
                    Section("Needs you") {
                        ForEach(waiting) { approval in
                            NavigationLink(value: WatchRoute.approval(approval.id)) {
                                Text(approval.title).font(.silo(14, .medium)).lineLimit(2)
                            }
                        }
                    }
                }
                Section { compose(bot) }
                feedSection
            } else {
                Text("This Bot is no longer here.").font(.silo(14)).foregroundStyle(Theme.ink2)
            }
        }
        .navigationTitle(model.bot(botID)?.name ?? "Bot")
        .task { feed = await model.feed(botID) }
    }

    private func head(_ bot: SharedBot) -> some View {
        HStack(spacing: 10) {
            CrestView(index: bot.crest, size: 40, state: crestState(lamp: bot.look.lamp))
            VStack(alignment: .leading, spacing: 2) {
                Text(bot.look.word).font(.silo(15, .medium)).foregroundStyle(Theme.color(bot.look.tone)).lineLimit(2)
                if !bot.step.isEmpty { Text(bot.step).font(.siloMono(12)).foregroundStyle(Theme.ink3) }
                if bot.asks > 0 {
                    Text(bot.asks == 1 ? "1 question on the Dashboard" : "\(bot.asks) questions on the Dashboard")
                        .font(.silo(12)).foregroundStyle(Theme.cobalt)
                }
            }
        }
        .listRowBackground(Color.clear)
    }

    @ViewBuilder
    private func compose(_ bot: SharedBot) -> some View {
        switch message {
        case .idle:
            dictate(bot, label: "Message")
        case .sending(let text):
            VStack(alignment: .leading, spacing: 6) {
                Text(text).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(3)
                HStack(spacing: 6) {
                    ProgressView().frame(width: 18)
                    Text("Waiting for \(bot.name)…").font(.silo(13)).foregroundStyle(Theme.ink3)
                }
            }
        case .done(let text, let outcome):
            VStack(alignment: .leading, spacing: 6) {
                Text(text).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(3)
                switch outcome {
                case .replied(let reply):
                    Text(Self.prose(reply)).font(.reply(15)).foregroundStyle(Theme.ink)
                case .stillWorking:
                    Text("Sent. \(bot.name) is still on it; the reply will be in the chat on your iPhone.")
                        .font(.silo(13)).foregroundStyle(Theme.ink2)
                case .failed(let problem):
                    ProblemCard(problem: problem, lead: "Not sent.")
                }
            }
            if case .failed = outcome {
                Button("Send again") { Task { await send(text, to: bot) } }
            }
            dictate(bot, label: "New message")
        }
    }

    private func dictate(_ bot: SharedBot, label: String) -> some View {
        TextFieldLink(prompt: Text("Ask \(bot.name)…")) {
            Label(label, systemImage: "mic.fill").font(.silo(15, .medium))
        } onSubmit: { text in
            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return }
            Task { await send(trimmed, to: bot) }
        }
    }

    private func send(_ text: String, to bot: SharedBot) async {
        message = .sending(text)
        let outcome = await model.ask(bot.id, text)
        switch outcome {
        case .replied: WKInterfaceDevice.current().play(.success)
        case .failed: WKInterfaceDevice.current().play(.failure)
        case .stillWorking: WKInterfaceDevice.current().play(.click)
        }
        message = .done(text, outcome)
    }

    @ViewBuilder
    private var feedSection: some View {
        switch feed {
        case .success(let posts) where !posts.isEmpty:
            Section("Feed") {
                ForEach(posts.prefix(3), id: \.id) { post in
                    VStack(alignment: .leading, spacing: 2) {
                        if !post.title.isEmpty { Text(post.title).font(.silo(14, .semibold)).foregroundStyle(Theme.ink) }
                        Text(Self.prose(post.body)).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(6)
                        if let at = isoDate(post.createdAt) {
                            Text(at, style: .relative).font(.silo(11.5)).foregroundStyle(Theme.ink3)
                        }
                    }
                }
            }
        case .failure(let problem) where problem.kind != .cancelled:
            Section("Feed") {
                Label(problem.title, systemImage: problem.symbol).font(.silo(12.5)).foregroundStyle(Theme.ink3)
            }
        default:
            EmptyView()
        }
    }

    /// Markdown's inline marks; the blocks a watch cannot draw read as their text.
    static func prose(_ text: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
    }
}

/// One approval: the app's own words for the call, and Allow once or Deny. Always is a rule, set
/// in the app.
struct ApprovalScreen: View {
    @Environment(WatchModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let approvalID: String

    @State private var busy = false
    @State private var problem: ReachProblem?

    var body: some View {
        ScrollView {
            if let approval = model.state.approvals.first(where: { $0.id == approvalID }) {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 6) {
                        CrestView(index: approval.crest, size: 20)
                        Text(approval.botName).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                    }
                    Text(approval.title).font(.silo(16, .semibold)).foregroundStyle(Theme.ink)
                    if !approval.summary.isEmpty {
                        Text(approval.summary).font(.silo(13.5)).foregroundStyle(Theme.ink2)
                    }
                    if let problem { ProblemCard(problem: problem, lead: "Not answered.") }
                    Button {
                        Task { await answer(approval, allow: true) }
                    } label: {
                        Text("Allow once").font(.silo(15, .semibold)).frame(maxWidth: .infinity)
                    }
                    .tint(Theme.emerald)
                    .disabled(busy)
                    Button(role: .destructive) {
                        Task { await answer(approval, allow: false) }
                    } label: {
                        Text("Deny").font(.silo(15, .semibold)).frame(maxWidth: .infinity)
                    }
                    .tint(Theme.vermilion)
                    .disabled(busy)
                    if busy { ProgressView().frame(maxWidth: .infinity) }
                }
            } else {
                VStack(alignment: .leading, spacing: 6) {
                    Label("Already answered", systemImage: "checkmark.circle").foregroundStyle(Theme.emerald)
                    Text("Nothing waits on this any more.").font(.silo(13)).foregroundStyle(Theme.ink2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .navigationTitle("Needs you")
    }

    private func answer(_ approval: SharedApproval, allow: Bool) async {
        busy = true
        problem = await model.decide(approval, allow: allow)
        busy = false
        if problem == nil {
            WKInterfaceDevice.current().play(allow ? .success : .directionDown)
            dismiss()
        } else {
            WKInterfaceDevice.current().play(.failure)
        }
    }
}
