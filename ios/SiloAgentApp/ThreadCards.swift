import SafariServices
import SiloClient
import SwiftUI

// What the Bot puts in a chat besides words: a question to answer in place, setups it offers,
// and the marks a slash command or the interview leave.

/// A question the Bot asked in this chat (ask_owner), or one asked from elsewhere while the owner
/// was away. The Bot speaks in the upper half; the reply tray below opens the answer sheet. It is
/// a question, not an alarm: cobalt, no ribbon.
struct ThreadAskCard: View {
    @Environment(AppModel.self) private var model
    let card: Silo_V1_DashboardCard
    let asked: Bool
    @State private var answering = false

    private var open: Bool { card.status == "open" }
    /// The title, unless it is just the summary's first line.
    private var heading: String {
        let first = card.summary.split(separator: "\n").first.map(String.init) ?? ""
        return first.trimmingCharacters(in: .whitespaces) == card.title.trimmingCharacters(in: .whitespaces) ? "" : card.title
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                Label(asked ? "Asked while you were away" : "Question for you", systemImage: "questionmark.bubble")
                    .font(.silo(12.5, .medium))
                    .foregroundStyle(open ? Theme.cobalt : Theme.ink3)
                if !heading.isEmpty {
                    Text(heading).font(.silo(15, .semibold)).foregroundStyle(Theme.ink)
                }
                Text(card.summary.isEmpty ? card.title : card.summary)
                    .font(.reply())
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)

            HStack(spacing: 10) {
                if open {
                    Button(card.submitLabel.isEmpty ? "Answer" : card.submitLabel) { answering = true }
                        .buttonStyle(.ink)
                    Button("Not now") { Task { await model.dismissCard(card) } }
                        .buttonStyle(.quiet)
                } else {
                    Image(systemName: card.status == "submitted" ? "checkmark" : "moon.zzz")
                    Text(card.status == "submitted" ? "Answered" : "Put away")
                }
                Spacer(minLength: 0)
            }
            .font(.silo(13))
            .foregroundStyle(Theme.ink3)
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.well)
        }
        .background(Theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
        .sheet(isPresented: $answering) {
            AskSheet(card: card, inThread: true) {}
        }
    }
}

/// What the Bot offered to add in one turn: each offer with why and what it filled in. Set up adds
/// a library connector here; a channel or a drive is finished on the web client, whose forms they
/// need. Not now tells the Bot once the turn's offers are all answered.
struct OfferGroup: View {
    @Environment(AppModel.self) private var model
    let offers: [Silo_V1_SetupOffer]

    var body: some View {
        let open = offers.filter { $0.status == "open" }
        VStack(alignment: .leading, spacing: 10) {
            ForEach(offers, id: \.id) { offer in OfferCard(offer: offer) }
            if open.count > 1 {
                Button("Not now for these") {
                    Task { await model.resolveOffer(open[0], outcome: "skipped", rest: true) }
                }
                .buttonStyle(.quiet)
            }
        }
    }
}

private struct OfferCard: View {
    @Environment(AppModel.self) private var model
    let offer: Silo_V1_SetupOffer
    @State private var setting = false

    private var symbol: String {
        switch offer.kind {
        case "channel": "dot.radiowaves.left.and.right"
        case "drive": "externaldrive"
        default: "puzzlepiece.extension"
        }
    }

    private var kindLabel: String {
        switch offer.kind {
        case "channel": "Channel"
        case "drive": "Drive"
        default: "Connector"
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Image(systemName: symbol)
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 32, height: 32)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                VStack(alignment: .leading, spacing: 1) {
                    Text(offer.label.isEmpty ? offer.target : offer.label).font(.silo(15, .semibold)).foregroundStyle(Theme.ink)
                    Text(kindLabel).font(.silo(12)).foregroundStyle(Theme.ink3)
                }
                Spacer(minLength: 0)
            }
            if !offer.why.isEmpty {
                Text(offer.why).font(.silo(14)).foregroundStyle(Theme.ink2)
            }
            if !offer.options.isEmpty {
                VStack(alignment: .leading, spacing: 3) {
                    ForEach(offer.options.sorted { $0.key < $1.key }, id: \.key) { key, value in
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            Text(key.replacingOccurrences(of: "_", with: " ")).font(.silo(12.5)).foregroundStyle(Theme.ink3)
                            Text(value).font(.siloMono(12.5)).foregroundStyle(Theme.ink2).lineLimit(2)
                        }
                    }
                }
            }
            HStack(spacing: 10) {
                switch offer.status {
                case "open":
                    Button("Set up") { setting = true }.buttonStyle(.ink)
                    Button("Not now") { Task { await model.resolveOffer(offer, outcome: "skipped") } }.buttonStyle(.quiet)
                case "done":
                    Label("Set up", systemImage: "checkmark").font(.silo(13)).foregroundStyle(Theme.emerald)
                default:
                    Label("Not now", systemImage: "moon.zzz").font(.silo(13)).foregroundStyle(Theme.ink3)
                }
                Spacer(minLength: 0)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .siloCard()
        .sheet(isPresented: $setting) { OfferSetupSheet(offer: offer) }
    }
}

/// Setting an offer up. A library connector is added to the Bot from here (its sign-in, if it has
/// one, stays on the web); a channel or a drive opens the web client, where its form lives.
private struct OfferSetupSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let offer: Silo_V1_SetupOffer
    @State private var busy = false
    @State private var error: String?
    @State private var added: Silo_V1_BotConnector?
    @State private var web: IdentifiedURL?

    var body: some View {
        NavigationStack {
            Form {
                if offer.kind == "connector" {
                    connector
                } else {
                    elsewhere
                }
                if let error {
                    Section { Text(error).foregroundStyle(Theme.vermilion) }
                }
            }
            .navigationTitle(offer.label.isEmpty ? offer.target : offer.label)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(added == nil ? "Cancel" : "Done") { dismiss() } }
            }
            .disabled(busy)
        }
        .presentationDetents([.medium, .large])
        .sheet(item: $web) { item in SafariView(url: item.url).ignoresSafeArea() }
    }

    @ViewBuilder
    private var connector: some View {
        if let added {
            Section {
                Label("Added to \(model.selectedBot?.name ?? "the Bot")", systemImage: "checkmark")
                if added.authStatus == "needs_auth" {
                    Text("It still needs its sign-in or settings. Finish that on the web client.")
                        .font(.silo(13)).foregroundStyle(Theme.ink2)
                }
            }
        } else {
            Section {
                Text("Adds \(offer.label.isEmpty ? offer.target : offer.label) from the library to \(model.selectedBot?.name ?? "this Bot") with its default settings.")
                    .font(.silo(14))
            } footer: {
                Text("A connector that signs in or needs a key asks for it on the web client.")
            }
            Section {
                Button("Add to Bot") { Task { await addConnector() } }
            }
        }
    }

    @ViewBuilder
    private var elsewhere: some View {
        Section {
            Text(offer.kind == "channel"
                 ? "A channel is set up with the web client's form: it signs in and picks its conversation there."
                 : "A drive is set up with the web client's form: it signs in to the provider there.")
                .font(.silo(14))
        }
        Section {
            if let url = webURL {
                Button("Open on the web") { web = IdentifiedURL(url: url) }
            }
            Button("I set it up") {
                Task {
                    await model.resolveOffer(offer, outcome: "done")
                    dismiss()
                }
            }
        }
    }

    /// The chat on the web client, where the same offer card opens Silo's own form.
    private var webURL: URL? {
        guard let botID = model.selectedBotID else { return nil }
        var path = "\(model.serverURL)/bots/\(botID)/run"
        if !offer.chatID.isEmpty { path += "/\(offer.chatID)" }
        return URL(string: path)
    }

    private func addConnector() async {
        guard let botID = model.selectedBotID else { return }
        busy = true
        error = nil
        defer { busy = false }
        do {
            let library = try await model.client.listConnectors()
            guard let preset = library.first(where: { $0.identifier == offer.target }) else {
                error = "\(offer.label.isEmpty ? offer.target : offer.label) is not available on this server any more."
                return
            }
            added = try await model.client.attachConnector(botID: botID, connectorID: preset.id)
            await model.resolveOffer(offer, outcome: "done")
        } catch {
            self.error = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

/// A slash command's mark: the context started over here, or messages were deleted for good.
struct BoundaryRow: View {
    let block: Block

    var body: some View {
        HStack(spacing: 10) {
            Theme.line.frame(height: 1)
            Label(block.source == "wiped"
                  ? "\(block.count) \(block.count == 1 ? "message" : "messages") wiped"
                  : "Context cleared",
                  systemImage: block.source == "wiped" ? "trash" : "eraser")
                .font(.silo(12, .medium))
                .foregroundStyle(Theme.ink3)
                .fixedSize()
            Theme.line.frame(height: 1)
        }
        .padding(.vertical, 4)
    }
}

/// Where the interview starts: its opening turn is Silo's prompt, not the owner's words.
struct InterviewMark: View {
    var body: some View {
        HStack(spacing: 10) {
            Theme.line.frame(height: 1)
            Label("Getting to know you", systemImage: "sparkles")
                .font(.silo(12, .medium))
                .foregroundStyle(Theme.ink3)
                .fixedSize()
            Theme.line.frame(height: 1)
        }
        .padding(.vertical, 4)
    }
}
