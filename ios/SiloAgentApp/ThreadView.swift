import Observation
import QuickLook
import SiloClient
import SwiftUI

struct ThreadView: View {
    @Environment(AppModel.self) private var model
    private static let end = "thread-end"
    /// The events to render; the chat passes `model.events`, a log page its own stream.
    var events: [Silo_V1_RunEvent]
    var busy: Bool
    /// Edit/delete/branch only apply to the selected web chat.
    var interactive = true
    /// Older pages of a long thread: how many events were put in front, whether there are more,
    /// and how to load the next one. A log page passes none.
    var prepended = 0
    var earlier = false
    var loadingEarlier = false
    var onEarlier: (() async -> Void)?
    @State private var editing: Block?
    @State private var editText = ""
    @State private var deleting: Block?
    /// Whether the reader is at the end of the thread. New output follows only while they are,
    /// so scrolling up to read while a run streams is not yanked back down.
    @State private var atBottom = true
    /// Only the reader's own scrolling can leave the end: content growing under a scroll that is
    /// still settling must not count as "scrolled up".
    @State private var dragging = false
    /// The `prepended` count the last change of events saw.
    @State private var seenPrepended = 0
    /// The blocks that were there when the thread first drew: they do not arrive.
    @State private var seenBlocks: Set<String>?

    var body: some View {
        let blocks = foldEvents(events, first: -prepended)
        let lastUserID = blocks.last(where: { $0.kind == .user && $0.from.isEmpty })?.id
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if earlier, let onEarlier {
                        Button {
                            // Keep the reader on what they were looking at once the page is in front.
                            let anchor = blocks.first?.id
                            Task {
                                await onEarlier()
                                if let anchor { proxy.scrollTo(anchor, anchor: .top) }
                            }
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: "arrow.up").font(.system(size: 11, weight: .medium))
                                Text(loadingEarlier ? "Loading…" : "Show earlier messages")
                            }
                            .font(.silo(12.5, .medium))
                            .foregroundStyle(Theme.ink2)
                            .padding(.horizontal, 12)
                            .frame(height: 32)
                        }
                        .buttonStyle(.press)
                        .disabled(loadingEarlier)
                        .frame(maxWidth: .infinity)
                    }
                    if blocks.isEmpty && !busy {
                        Text("Send a message to start.")
                            .font(.silo(14.5))
                            .foregroundStyle(Theme.ink3)
                            .frame(maxWidth: .infinity)
                            .padding(.top, 48)
                    }
                    let shown = blocks.filter(\.shown)
                    ForEach(Array(shown.enumerated()), id: \.element.id) { index, block in
                        BlockView(block: block)
                            .id(block.id)
                            // 16pt between items; thinking, tool and receipt rows stack 2pt apart.
                            .padding(.top, index == 0 ? 0 : block.isActivity && shown[index - 1].isActivity ? 2 : 16)
                            // What arrives rises; a replay is simply there. A message just sent glides up from the composer.
                            .rise(when: seenBlocks != nil && !(seenBlocks?.contains(block.id) ?? true), from: block.kind == .user ? 56 : 8)
                            .longPressMenu(enabled: block.kind == .user || block.kind == .assistant) {
                                menu(for: block, isLastUser: interactive && block.id == lastUserID)
                            }
                    }
                    Color.clear.frame(height: 1).id(Self.end)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollDismissesKeyboard(.interactively)
            .onScrollGeometryChange(for: Bool.self) { geometry in
                let seen = geometry.contentOffset.y + geometry.containerSize.height - geometry.contentInsets.bottom
                return seen >= geometry.contentSize.height - 80
            } action: { _, value in
                if value || dragging { atBottom = value }
            }
            .onScrollPhaseChange { _, phase in
                dragging = phase == .interacting || phase == .decelerating
            }
            .onChange(of: events.count) { old, new in
                // A page put in front is not new output.
                let paged = prepended != seenPrepended
                seenPrepended = prepended
                guard atBottom, !paged else { return }
                // A replay arrives as a burst: jump, do not animate through it.
                if new - old > 3 || old == 0 {
                    proxy.scrollTo(Self.end, anchor: .bottom)
                } else {
                    withAnimation(Motion.quiet(0.2)) { proxy.scrollTo(Self.end, anchor: .bottom) }
                }
            }
            .onChange(of: lastUserID) { _, _ in
                // The reader's own message always brings them to the end.
                atBottom = true
                withAnimation(Motion.quiet(0.2)) { proxy.scrollTo(Self.end, anchor: .bottom) }
            }
            .onAppear { proxy.scrollTo(Self.end, anchor: .bottom) }
            .task(id: events.first?.id) {
                // The replay comes in a burst: what is here a moment later did not just happen.
                seenBlocks = nil
                try? await Task.sleep(for: .milliseconds(450))
                seenBlocks = Set(foldEvents(events, first: -prepended).map(\.id))
            }
            .overlay(alignment: .bottomTrailing) {
                if !atBottom && !blocks.isEmpty {
                    Button {
                        withAnimation(Motion.quiet(0.24)) { proxy.scrollTo(Self.end, anchor: .bottom) }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(Theme.ink)
                            .frame(width: 36, height: 36)
                            .background(Theme.surface, in: Circle())
                            .overlay(Circle().stroke(Theme.line, lineWidth: 1))
                            .floatShadow()
                    }
                    .buttonStyle(.press)
                    .padding(.trailing, 16)
                    .padding(.bottom, 8)
                    .accessibilityLabel("Jump to latest")
                    .transition(.opacity)
                }
            }
        }
        .sheet(item: $editing) { block in
            EditMessageSheet(text: $editText) {
                Task { await model.editMessage(eventID: block.eventID, text: editText) }
            }
            .siloSheet(detents: [.medium, .large])
        }
        .confirmSlip(Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), title: "Delete this message?",
                     message: "It goes, and everything after it.", action: "Delete message") {
            if let block = deleting { Task { await model.deleteMessage(eventID: block.eventID) } }
        }
    }

    @ViewBuilder
    private func menu(for block: Block, isLastUser: Bool) -> some View {
        switch block.kind {
        case .user:
            MenuRow(title: "Copy", symbol: "doc.on.doc") { UIPasteboard.general.string = block.text }
            if interactive && !block.eventID.isEmpty {
                MenuRow(title: "Branch into a new chat", symbol: "arrow.triangle.branch") {
                    Task { await model.diverge(eventID: block.eventID) }
                }
                if isLastUser && !model.isRunning {
                    MenuRow(title: "Edit", symbol: "pencil") {
                        editText = block.text
                        editing = block
                    }
                    MenuRow(title: "Delete", symbol: "trash", destructive: true) { deleting = block }
                }
            }
        case .assistant:
            MenuRow(title: "Copy", symbol: "doc.on.doc") { UIPasteboard.general.string = block.text }
            if model.speechEnabled, let botID = model.selectedBotID {
                MenuRow(title: "Read aloud", symbol: "speaker.wave.2") {
                    model.readAloud.toggle(block.text, client: model.client, botID: botID)
                }
            }
        default:
            EmptyView()
        }
    }
}

struct EditMessageSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Binding var text: String
    let onSend: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: "Edit message", onCancel: { dismiss() }) {
                Button("Send") {
                    onSend()
                    dismiss()
                }
                .buttonStyle(.ink)
                .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            TextEditor(text: $text)
                .font(.silo(16))
                .foregroundStyle(Theme.ink)
                .tint(Theme.cobalt)
                .scrollContentBackground(.hidden)
                .padding(12)
        }
    }
}

struct BlockView: View {
    @Environment(AppModel.self) private var model
    let block: Block

    var body: some View {
        switch block.kind {
        case .user:
            if block.from == "onboarding" { InterviewMark() } else { userBubble }
        case .assistant:
            reply
        case .thinking:
            thinkingRow
        case .tool:
            if let path = presentedPath { PresentCard(path: path) } else { ToolBlockRow(block: block) }
        case .receipt:
            ReceiptRow(block: block)
        case .error:
            Text(block.text)
                .font(.silo(14.5))
                .foregroundStyle(Theme.vermilion)
                .textSelection(.enabled)
        case .report:
            reportCard
        case .quote:
            quoteCard
        case .compaction:
            compactionRow
        case .artifact:
            if let artifact = block.artifact { ArtifactCard(artifact: artifact, runID: block.runID) }
        case .card:
            if let card = block.card { ThreadAskCard(card: card, asked: block.asked) }
        case .offers:
            OfferGroup(offers: block.offers)
        case .boundary:
            BoundaryRow(block: block)
        case .call:
            CallMark(start: block.source == "start", seconds: block.count)
        }
    }

    // MARK: the Bot's reply

    private var reply: some View {
        VStack(alignment: .leading, spacing: 6) {
            ProseView(text: block.text)
            if block.streaming {
                Circle().fill(Theme.ink).frame(width: 6, height: 6).breathing()
            } else if !block.text.isEmpty {
                HStack(spacing: 0) {
                    CopyButton(text: block.text, size: 13).frame(width: 32, height: 32).clipped()
                    if model.speechEnabled { ReadAloudButton(text: block.text) }
                }
                .padding(.leading, -8)
                .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: user

    private var userBubble: some View {
        VStack(alignment: .trailing, spacing: 6) {
            if !block.from.isEmpty {
                Text("From \(block.from)").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            } else if !block.author.isEmpty {
                // On a Bot several people use: who wrote it.
                HStack(spacing: 6) {
                    Initials(email: block.author, size: 18)
                    Text(block.author).font(.silo(12)).foregroundStyle(Theme.ink3)
                }
            }
            if !block.text.isEmpty {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    if block.spoken {
                        Image(systemName: "waveform")
                            .font(.system(size: 12))
                            .foregroundStyle(Theme.ink3)
                            .accessibilityLabel("Said on a call. This is what was heard.")
                    }
                    Text(block.text).font(.silo(16)).foregroundStyle(Theme.ink).textSelection(.enabled)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Theme.well, in: UnevenRoundedRectangle(topLeadingRadius: Theme.Radius.bubble, bottomLeadingRadius: Theme.Radius.bubble,
                                                                  bottomTrailingRadius: 6, topTrailingRadius: Theme.Radius.bubble, style: .continuous))
            }
            ForEach(Array(block.attachments.enumerated()), id: \.offset) { _, attachment in
                HStack(spacing: 6) {
                    Image(systemName: "paperclip").font(.system(size: 11))
                    Text(attachment.name).lineLimit(1)
                    Text(formatBytes(attachment.size)).foregroundStyle(Theme.ink3)
                }
                .font(.silo(12.5))
                .foregroundStyle(Theme.ink2)
                .padding(.horizontal, 10)
                .frame(height: 28)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, 40)
    }

    // MARK: thinking

    private var thinkingRow: some View {
        FoldRow(live: block.streaming) {
            HStack(spacing: 8) {
                Image(systemName: "lightbulb").font(.system(size: 13)).foregroundStyle(Theme.ink3).frame(width: 18)
                Text(block.streaming ? "Thinking" : "Thought")
                    .font(.silo(13.5, .medium))
                    .foregroundStyle(Theme.ink3)
                    .shimmering(block.streaming)
            }
        } content: {
            Text(block.text).font(.silo(14)).foregroundStyle(Theme.ink2).textSelection(.enabled)
        }
    }

    /// A finished `present` of a user-facing path shows the file itself; `bot/…` stays a quiet row.
    private var presentedPath: String? {
        guard block.name == "present", !block.running, let result = block.result, !result.isEmpty,
              !result.hasPrefix("error:") else { return nil }
        let path = parseToolArgs(block.args)["path"] ?? ""
        return path.isEmpty || isBotScratch(path) ? nil : path
    }

    // MARK: reports, quotes, compaction

    private var reportCard: some View {
        FoldRow {
            HStack(spacing: 8) {
                Image(systemName: "person.2").font(.system(size: 12)).foregroundStyle(Theme.ink3).frame(width: 18)
                Text("Subagents finished").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink2)
                Text(block.agents.map { $0.status.isEmpty ? $0.name : "\($0.name) \($0.status == "done" ? "✓" : $0.status)" }.joined(separator: " · "))
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .lineLimit(1)
            }
        } content: {
            Text(block.text).font(.silo(14)).foregroundStyle(Theme.ink2).textSelection(.enabled)
        }
    }

    /// What the chat opened from, set off by a cobalt rule: a Feed post, a Dashboard question, or a
    /// widget as it stood. The caption says which, where the Bot made it and when.
    private var quoteCard: some View {
        let from = block.quoted?.from ?? ""
        let label: String
        switch from {
        case "widget": label = "Widget on the Dashboard"
        case "ask": label = "Asked on the Dashboard"
        case "asked": label = "Asked while you were away"
        default: label = block.source == "Dashboard" ? "From the Dashboard" : "From the Feed"
        }
        let board = !from.isEmpty && from != "feed" || block.source == "Dashboard"
        let caption = [label, block.source == "Dashboard" ? "" : block.source, relativeTime(block.createdAt)]
            .filter { !$0.isEmpty }.joined(separator: " · ")
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: board ? "square.grid.2x2" : "tray").font(.system(size: 11))
                Text(caption).lineLimit(1)
            }
            .font(.silo(12))
            .foregroundStyle(Theme.ink3)
            if from == "ask", let title = block.quoted?.title, !title.isEmpty {
                Text(title).font(.silo(15, .medium)).foregroundStyle(Theme.ink)
            }
            if from == "widget", let widget = block.quoted?.widget, !widget.title.isEmpty {
                Text(widget.title).font(.silo(15, .medium)).foregroundStyle(Theme.ink)
            }
            ProseView(text: block.text)
        }
        .padding(.leading, 15)
        .padding([.trailing, .vertical], 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(alignment: .leading) { Theme.cobalt.frame(width: 3).clipShape(RoundedRectangle(cornerRadius: 1.5)) }
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }

    @ViewBuilder
    private var compactionRow: some View {
        let glyph = Image(systemName: "rectangle.compress.vertical").font(.system(size: 13)).foregroundStyle(Theme.ink3).frame(width: 18)
        if block.running {
            HStack(spacing: 8) {
                glyph
                Text("Summarizing earlier messages").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink3).shimmering()
            }
            .frame(minHeight: 32)
        } else if block.text.isEmpty {
            HStack(spacing: 8) {
                glyph
                Text("Summary stopped").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink3)
            }
            .frame(minHeight: 32)
        } else {
            FoldRow {
                HStack(spacing: 8) {
                    glyph
                    Text("Earlier messages summarized").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink3)
                }
            } content: {
                Text(block.text).font(.silo(14)).foregroundStyle(Theme.ink2).textSelection(.enabled)
            }
        }
    }
}

/// Where a voice call began, and where it ended with how long it ran. What was said sits between
/// them as ordinary messages.
struct CallMark: View {
    let start: Bool
    let seconds: Int

    var body: some View {
        HStack(spacing: 12) {
            Theme.line.frame(height: 1)
            HStack(spacing: 6) {
                Image(systemName: start ? "phone" : "phone.down").font(.system(size: 11))
                Text(start ? "Call started" : "Call ended · \(callClock(seconds: seconds))")
            }
            .font(.silo(12.5, .medium))
            .foregroundStyle(Theme.ink3)
            .fixedSize()
            Theme.line.frame(height: 1)
        }
        .padding(.top, 8)
    }
}

/// A decision's receipt: the shield, the decision in ink, what and where in `ink3`, a hairline
/// filling the rest of the line, and "by you".
private struct ReceiptRow: View {
    let block: Block

    var body: some View {
        let allowed = block.decision == "allow_once" || block.decision == "always"
        let label: String
        switch block.decision {
        case "allow_once": label = "Allowed once"
        case "always": label = "Always allowed"
        case "deny": label = "Denied"
        case "stopped": label = "Stopped"
        default: label = block.decision.isEmpty ? "Decided" : block.decision
        }
        let detail = [block.title, block.text].filter { !$0.isEmpty }.joined(separator: " · ")
        return HStack(spacing: 8) {
            Image(systemName: allowed ? "checkmark.shield" : "xmark.shield")
                .font(.system(size: 13))
                .foregroundStyle(allowed ? Theme.emerald : Theme.ink3)
                .frame(width: 18)
            Text(label).font(.silo(13.5, .medium)).foregroundStyle(Theme.ink).fixedSize()
            if !detail.isEmpty { Text("· \(detail)").font(.silo(13)).foregroundStyle(Theme.ink3).lineLimit(1) }
            Theme.line.frame(height: 1).frame(minWidth: 8)
            Text("by you").font(.silo(12.5)).foregroundStyle(Theme.ink3).fixedSize()
        }
        .frame(minHeight: 28)
    }
}

/// A tool call: a 32pt line flush with the reply. The icon, the verb in `ink3`, the app in `ink2`,
/// the action in mono, then a chevron; the tense says it is done, so a finished row carries no mark.
/// Connector calls made from Python sit above it. DESIGN.md § Thread.
private struct ToolBlockRow: View {
    let block: Block

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(block.calls) { call in
                ToolLine(symbol: "powerplug", verb: verb(running: call.running || call.waiting, call.outcome), app: call.name.isEmpty ? "connector" : call.title,
                         action: "", running: call.running, waiting: call.waiting, stopped: call.outcome != nil, filter: call.filter) {
                    if let result = call.result, !result.isEmpty { CodeWell(code: result, language: nil) }
                }
            }
            if block.name != "call" {
                ToolLine(symbol: toolSymbol(block.name), verb: verb(running: block.running || block.waiting, block.outcome),
                         app: toolApp(block.name), action: toolAction(name: block.name, args: block.args),
                         running: block.running, waiting: block.waiting, stopped: block.outcome != nil, filter: block.filter) {
                    ToolBody(block: block)
                }
            }
        }
    }

    private func verb(running: Bool, _ outcome: ToolOutcome?) -> String {
        if outcome == .denied { return "Skipped" }
        if outcome == .stopped { return "Stopped" }
        return running ? "Using" : "Used"
    }
}

private struct ToolLine<Content: View>: View {
    let symbol: String
    let verb: String
    let app: String
    let action: String
    let running: Bool
    let waiting: Bool
    let stopped: Bool
    let filter: Filtered?
    @ViewBuilder let content: Content

    var body: some View {
        FoldRow(live: running) {
            HStack(spacing: 8) {
                Image(systemName: symbol).font(.system(size: 13)).foregroundStyle(Theme.ink2).frame(width: 18)
                if running {
                    Text("\(verb) \(app)").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink3).shimmering().lineLimit(1).fixedSize()
                } else {
                    HStack(spacing: 4) {
                        Text(verb).foregroundStyle(Theme.ink3)
                        Text(app).foregroundStyle(stopped ? Theme.ink3 : Theme.ink2)
                    }
                    .font(.silo(13.5, .medium))
                    .lineLimit(1)
                    .fixedSize()
                }
                if waiting {
                    HStack(spacing: 6) {
                        Circle().fill(Theme.vermilion).frame(width: 7, height: 7).breathing()
                        Text("Waiting for you").font(.silo(12.5)).foregroundStyle(Theme.vermilion).fixedSize()
                    }
                }
                if let filter {
                    Text(filter.word).font(.silo(12.5)).foregroundStyle(filter.verdict == "withheld" ? Theme.vermilion : Theme.ink3).fixedSize()
                }
                if !action.isEmpty {
                    Text(action).font(.siloMono(12.5)).foregroundStyle(Theme.ink3).lineLimit(1).truncationMode(.tail)
                }
            }
        } content: {
            VStack(alignment: .leading, spacing: 8) {
                if let filter, filter.verdict == "withheld", !filter.original.isEmpty {
                    Text("Withheld from the Bot").font(.silo(12.5, .medium)).foregroundStyle(Theme.ink3)
                    CodeWell(code: filter.original, language: nil)
                }
                content
            }
        }
    }
}

/// What a tool did, in the form it is read in: never raw JSON if there is a better way.
private struct ToolBody: View {
    let block: Block

    var body: some View {
        let a = parseToolArgs(block.args)
        VStack(alignment: .leading, spacing: 8) {
            switch block.name {
            case "exec_python":
                if let code = a["code"], !code.isEmpty { CodeWell(code: code, language: "python") }
            case "terminal":
                if let command = a["command"], !command.isEmpty { CodeWell(code: command, language: "bash") }
            case "patch":
                if let path = a["path"], !path.isEmpty { Text(path).font(.siloMono(12.5)).foregroundStyle(Theme.ink2) }
                DiffWell(old: a["old_text"] ?? "", new: a["new_text"] ?? "")
            case "write":
                if let path = a["path"], !path.isEmpty { Text(path).font(.siloMono(12.5)).foregroundStyle(Theme.ink2) }
                if let content = a["content"], !content.isEmpty { CodeWell(code: content, language: nil) }
            case "read", "grep", "delete", "present":
                EmptyView()
            default:
                if !block.args.isEmpty { CodeWell(code: prettyJSON(block.args), language: nil) }
            }
            if let result = block.result, !result.isEmpty {
                CodeWell(code: result, language: nil)
            }
        }
    }
}

/// A patch's old and new text: `vermilion` and `emerald` as text only, never tinted lines.
private struct DiffWell: View {
    let old: String
    let new: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                    Text(line).font(.siloMono(12.5)).foregroundStyle(line.hasPrefix("+") ? Theme.emerald : Theme.vermilion)
                }
            }
            .padding(10)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
    }

    private var lines: [String] {
        (old.isEmpty ? [] : old.components(separatedBy: "\n").map { "-\($0)" })
            + (new.isEmpty ? [] : new.components(separatedBy: "\n").map { "+\($0)" })
    }
}

/// A file or skill the Bot handed over. Tap to preview (QuickLook also shares/saves it); a
/// pending skill has **Save skill**, which copies it into the personal library.
struct ArtifactCard: View {
    @Environment(AppModel.self) private var model
    let artifact: ArtifactInfo
    let runID: String
    @State private var preview: URL?
    @State private var loading = false
    @State private var saving = false

    private var saved: Bool { artifact.isSaved || model.savedSkillPaths.contains(artifact.path) }
    private var pending: Bool { artifact.isPending && !saved }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                Task { await open() }
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: artifact.artifactType == "skill" ? "square.stack.3d.up" : fileSymbol)
                        .font(.system(size: 18))
                        .foregroundStyle(Theme.ink2)
                        .frame(width: 36, height: 36)
                        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(artifact.title.isEmpty ? artifact.name : artifact.title)
                            .font(.silo(14.5, .medium))
                            .foregroundStyle(Theme.ink)
                            .multilineTextAlignment(.leading)
                            .lineLimit(2)
                        Text(subtitle)
                            .font(.silo(12.5))
                            .foregroundStyle(Theme.ink3)
                    }
                    Spacer(minLength: 0)
                    if loading { Spinner(size: 15) }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            if pending {
                Button {
                    saving = true
                    Task {
                        await model.saveSkill(artifact, runID: runID)
                        saving = false
                    }
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "tray.and.arrow.down").font(.system(size: 13))
                        Text("Save skill")
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.ink)
                .disabled(saving)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
        .quickLookPreview($preview)
    }

    private var subtitle: String {
        var parts = [artifact.artifactType == "skill" ? "Skill" : "File"]
        if let size = artifact.size { parts.append(formatBytes(size)) }
        if saved { parts.append("Saved") }
        return parts.joined(separator: " · ")
    }

    private var fileSymbol: String {
        switch (artifact.downloadName as NSString).pathExtension.lowercased() {
        case "pdf": return "doc.richtext"
        case "png", "jpg", "jpeg", "gif", "webp", "heic": return "photo"
        case "csv", "xlsx", "xls": return "tablecells"
        case "zip", "tar", "gz": return "doc.zipper"
        default: return "doc"
        }
    }

    private func open() async {
        guard !loading else { return }
        loading = true
        preview = await model.fetchArtifact(artifact)
        loading = false
    }
}

struct CodeWell: View {
    let code: String
    let language: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let language, !language.isEmpty {
                Text(language.uppercased())
                    .font(.silo(11, .medium))
                    .tracking(0.88)
                    .foregroundStyle(Theme.ink3)
                    .padding(.horizontal, 10)
                    .padding(.top, 8)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.siloMono(12.5))
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .padding(10)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
    }
}


/// `present` of a user-facing path: the file itself in a card (image inline, text in a well).
struct PresentCard: View {
    @Environment(AppModel.self) private var model
    let path: String
    @State private var file: Silo_V1_ReadFileResponse?
    @State private var failure: String?
    @State private var preview: URL?

    private var name: String {
        if let file, !file.name.isEmpty { return file.name }
        return path.split(separator: "/").last.map(String.init) ?? path
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Image(systemName: "doc")
                    .font(.system(size: 16))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 36, height: 36)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                VStack(alignment: .leading, spacing: 1) {
                    Text(name).font(.silo(14.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                    Text([file.map { formatBytes($0.size) }, "in Files"].compactMap { $0 }.joined(separator: " · "))
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink3)
                }
                Spacer(minLength: 0)
                if file != nil {
                    Button {
                        Task { await open() }
                    } label: {
                        Image(systemName: "arrow.up.forward.square")
                    }
                    .buttonStyle(.glyph)
                    .accessibilityLabel("Open")
                }
            }
            if let failure {
                ErrorLine(text: failure)
            } else if let file {
                content(file)
            } else {
                RoundedRectangle(cornerRadius: Theme.Radius.sm).fill(Theme.well).frame(height: 120).breathing()
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
        .quickLookPreview($preview)
        .task(id: path) {
            guard let botID = model.selectedBotID else { return }
            do {
                file = try await model.client.readFile(botID: botID, path: path)
            } catch {
                failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
            }
        }
    }

    @ViewBuilder
    private func content(_ file: Silo_V1_ReadFileResponse) -> some View {
        if !file.data.isEmpty, let image = UIImage(data: file.data) {
            Image(uiImage: image)
                .resizable()
                .scaledToFit()
                .clipShape(RoundedRectangle(cornerRadius: 8))
        } else if !file.binary, !file.content.isEmpty {
            CodeWell(code: String(file.content.prefix(4000)), language: nil)
            if file.truncated || file.content.count > 4000 {
                Text("Open to see more.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
            }
        } else if file.binary {
            Text("Preview not available. Open to view.").font(.silo(13)).foregroundStyle(Theme.ink3)
        }
    }

    private func open() async {
        guard let file else { return }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(name)
        let data = file.data.isEmpty ? Data(file.content.utf8) : file.data
        guard (try? data.write(to: url)) != nil else { return }
        preview = url
    }
}


/// A row that folds: the label, a chevron turning 90° over 260ms, and the body hanging under the
/// icon on a 1pt rule. With Follow live steps on, a live row opens while it runs and folds shut when
/// done, unless the reader touched it: their choice stays.
struct FoldRow<Label: View, Content: View>: View {
    var live = false
    @ViewBuilder let label: Label
    @ViewBuilder let content: Content
    @AppStorage("autoExpand") private var follow = false
    @State private var picked: Bool?

    private var open: Bool { picked ?? (follow && live) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(Motion.fold) { picked = !open }
            } label: {
                HStack(spacing: 6) {
                    label
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(Theme.ink3)
                        .rotationEffect(.degrees(open ? 90 : 0))
                        .animation(Motion.quiet(0.26), value: open)
                }
                .frame(minHeight: 32)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                content
                    .padding(.top, 4)
                    .padding(.bottom, 6)
                    .padding(.leading, 14)
                    .overlay(alignment: .leading) { Theme.line.frame(width: 1).padding(.leading, 8) }
                    .transition(.opacity)
            }
        }
        .animation(Motion.fold, value: open)
    }
}

extension Block {
    /// A line of the Bot's work rather than something said.
    var isActivity: Bool { kind == .thinking || kind == .tool || kind == .receipt || kind == .compaction }

    /// Rows the thread leaves out: an offer that was made is its card, so its tool row would only
    /// repeat it.
    var shown: Bool {
        !(kind == .tool && name == "offer_setup" && !running && !(result ?? "").isEmpty && !(result ?? "").hasPrefix("error:"))
    }
}
