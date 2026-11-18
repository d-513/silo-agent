import SiloClient
import SwiftUI

enum WatchRoute: Hashable {
    case bot(String)
    case approval(String)
}

struct WatchRoot: View {
    @Environment(WatchModel.self) private var model

    var body: some View {
        NavigationStack {
            if model.signedIn {
                BotsList()
                    .navigationDestination(for: WatchRoute.self) { route in
                        switch route {
                        case .bot(let id): BotScreen(botID: id)
                        case .approval(let id): ApprovalScreen(approvalID: id)
                        }
                    }
            } else {
                SetUpScreen()
            }
        }
    }
}

/// Not signed in yet: the phone hands it over, so the only thing to do is open Silo there.
private struct SetUpScreen: View {
    @Environment(WatchModel.self) private var model

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                CrestView(index: 0, size: 34)
                Text("Open Silo on your iPhone").font(.silo(17, .semibold)).foregroundStyle(Theme.ink)
                Text(model.phoneNearby
                     ? "Sign in there and your watch is signed in too."
                     : "Your iPhone isn't in reach. Once it is, and Silo there is signed in, your watch follows.")
                    .font(.silo(14)).foregroundStyle(Theme.ink2)
                Button {
                    WatchLink.shared.ask()
                } label: {
                    if model.asking { ProgressView() } else { Text("Check again") }
                }
                .disabled(model.asking)
                .padding(.top, 4)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .navigationTitle("Silo")
    }
}

/// The Bots, with what waits on you first, and the one problem in the way above them.
private struct BotsList: View {
    @Environment(WatchModel.self) private var model

    var body: some View {
        List {
            if let problem = model.problem {
                Section { ProblemCard(problem: problem, shownAt: model.state.at) }
            }
            if !model.state.approvals.isEmpty {
                Section("Needs you") {
                    ForEach(model.state.approvals) { approval in
                        NavigationLink(value: WatchRoute.approval(approval.id)) { ApprovalRow(approval: approval) }
                    }
                }
            }
            Section {
                if model.state.bots.isEmpty, model.problem == nil {
                    Text(model.refreshing ? "Loading…" : "No Bots yet. Make one on your iPhone.")
                        .font(.silo(14)).foregroundStyle(Theme.ink2)
                }
                ForEach(model.state.bots) { bot in
                    NavigationLink(value: WatchRoute.bot(bot.id)) { BotRow(bot: bot) }
                }
            } header: {
                if !model.state.approvals.isEmpty { Text("Bots") }
            }
        }
        .navigationTitle("Silo")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if model.refreshing { ProgressView() }
            }
        }
        // While it is on screen: every half minute, which is what an open watch app can afford.
        .task {
            while !Task.isCancelled {
                await model.refresh()
                try? await Task.sleep(for: .seconds(30))
            }
        }
    }
}

private struct BotRow: View {
    let bot: SharedBot

    var body: some View {
        HStack(spacing: 10) {
            CrestView(index: bot.crest, size: 30, state: crestState(lamp: bot.look.lamp))
            VStack(alignment: .leading, spacing: 1) {
                Text(bot.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                HStack(spacing: 4) {
                    Text(bot.look.word).foregroundStyle(Theme.color(bot.look.tone))
                    if !bot.step.isEmpty { Text(bot.step).font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
                }
                .font(.silo(12.5))
                .lineLimit(1)
            }
            Spacer(minLength: 0)
            if bot.needs > 0 {
                Text("\(bot.needs)")
                    .font(.silo(12, .semibold)).foregroundStyle(Theme.onAccent)
                    .frame(minWidth: 18, minHeight: 18)
                    .background(Theme.vermilion, in: Capsule())
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct ApprovalRow: View {
    let approval: SharedApproval

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                CrestView(index: approval.crest, size: 16)
                Text(approval.botName).font(.silo(12)).foregroundStyle(Theme.ink2).lineLimit(1)
            }
            Text(approval.title).font(.silo(14.5, .medium)).foregroundStyle(Theme.ink).lineLimit(2)
        }
    }
}

/// Why the watch could not do what it tried, in plain words, with when the data on screen is from
/// and a way to try again.
struct ProblemCard: View {
    @Environment(WatchModel.self) private var model
    let problem: ReachProblem
    /// When what is still on screen was fetched; nil where nothing is shown under it.
    var shownAt: Date?
    /// A line before the problem's own words: "Not sent."
    var lead: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Label(problem.title, systemImage: problem.symbol)
                .font(.silo(14, .semibold))
                .foregroundStyle(Theme.vermilion)
            if let lead { Text(lead).font(.silo(13, .medium)).foregroundStyle(Theme.ink) }
            Text(problem.detail).font(.silo(13)).foregroundStyle(Theme.ink2)
            if problem.isConnection, let shownAt, shownAt > .distantPast {
                Text("Showing what Silo said \(shownAt, style: .relative) ago.")
                    .font(.silo(12)).foregroundStyle(Theme.ink3)
            }
            if problem.kind == .signedOut {
                Button("Ask my iPhone") { WatchLink.shared.ask() }
                    .disabled(model.asking || !model.phoneNearby)
                    .padding(.top, 2)
            } else if problem.retryable {
                Button {
                    Task { await model.refresh() }
                } label: {
                    if model.refreshing { ProgressView() } else { Text("Try again") }
                }
                .disabled(model.refreshing)
                .padding(.top, 2)
            }
        }
        .padding(.vertical, 4)
    }
}
