import Observation
import SiloClient
import SwiftUI

struct ThreadView: View {
    @Environment(AppModel.self) private var model
    /// The events to render; the chat passes `model.events`, a log page its own stream.
    var events: [Silo_V1_RunEvent]
    var busy: Bool
    /// Edit/delete/branch only apply to the selected web chat.
    var interactive = true
    @State private var editing: Block?
    @State private var editText = ""
    @State private var deleting: Block?

    var body: some View {
        let blocks = foldEvents(events)
        let lastUserID = blocks.last(where: { $0.kind == .user && $0.from.isEmpty })?.id
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 14) {
                    if blocks.isEmpty && !busy {
                        Text("Send a message to start.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                            .padding(.top, 48)
                    }
                    ForEach(blocks) { block in
                        BlockView(block: block)
                            .id(block.id)
                            .contextMenu { menu(for: block, isLastUser: interactive && block.id == lastUserID) }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: events.count) { _, _ in
                guard let last = blocks.last else { return }
                withAnimation(.easeOut(duration: 0.15)) {
                    proxy.scrollTo(last.id, anchor: .bottom)
                }
            }
            .onAppear {
                guard let last = blocks.last else { return }
                proxy.scrollTo(last.id, anchor: .bottom)
            }
        }
        .sheet(item: $editing) { block in
            EditMessageSheet(text: $editText) {
                Task { await model.editMessage(eventID: block.eventID, text: editText) }
            }
        }
        .confirmationDialog("Delete this message?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { block in
            Button("Delete message and everything after", role: .destructive) {
                Task { await model.deleteMessage(eventID: block.eventID) }
            }
        }
    }

    @ViewBuilder
    private func menu(for block: Block, isLastUser: Bool) -> some View {
        switch block.kind {
        case .user:
            Button { UIPasteboard.general.string = block.text } label: { Label("Copy", systemImage: "doc.on.doc") }
            if interactive && !block.eventID.isEmpty {
                Button { Task { await model.diverge(eventID: block.eventID) } } label: {
                    Label("Branch into new chat", systemImage: "arrow.triangle.branch")
                }
                if isLastUser && !model.isRunning {
                    Button { editText = block.text; editing = block } label: { Label("Edit", systemImage: "pencil") }
                    Button(role: .destructive) { deleting = block } label: { Label("Delete", systemImage: "trash") }
                }
            }
        case .assistant:
            Button { UIPasteboard.general.string = block.text } label: { Label("Copy", systemImage: "doc.on.doc") }
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
        NavigationStack {
            TextEditor(text: $text)
                .padding(12)
                .navigationTitle("Edit message")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Send") {
                            onSend()
                            dismiss()
                        }
                        .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    }
                }
        }
        .presentationDetents([.medium, .large])
    }
}

struct BlockView: View {
    let block: Block

    var body: some View {
        switch block.kind {
        case .user:
            userBubble
        case .assistant:
            ProseView(text: block.text)
        case .thinking:
            thinkingRow
        case .tool:
            toolRow
        case .receipt:
            receiptRow
        case .error:
            Text(block.text)
                .font(.callout)
                .foregroundStyle(Theme.vermilion)
                .textSelection(.enabled)
        case .report:
            reportCard
        case .quote:
            quoteCard
        case .compaction:
            compactionRow
        case .artifact:
            if let artifact = block.artifact { ArtifactCard(artifact: artifact) }
        }
    }

    // MARK: user

    private var userBubble: some View {
        VStack(alignment: .trailing, spacing: 6) {
            if !block.from.isEmpty {
                Text("From \(block.from)")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
            }
            if !block.text.isEmpty {
                Text(block.text)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.bubble))
                    .textSelection(.enabled)
            }
            ForEach(Array(block.attachments.enumerated()), id: \.offset) { _, attachment in
                Label {
                    Text(attachment.name).lineLimit(1)
                    Text(formatBytes(attachment.size)).foregroundStyle(.secondary)
                } icon: {
                    Image(systemName: "paperclip")
                }
                .font(.caption)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Theme.well, in: Capsule())
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, 40)
    }

    // MARK: thinking

    private var thinkingRow: some View {
        DisclosureGroup {
            Text(block.text)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .padding(.top, 4)
        } label: {
            HStack(spacing: 6) {
                if block.streaming { ProgressView().controlSize(.mini) }
                Text(block.streaming ? "Thinking…" : "Thought")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: tools

    private var toolRow: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                if !block.args.isEmpty {
                    CodeWell(code: prettyJSON(block.args), language: nil)
                }
                ForEach(block.calls) { call in
                    HStack(spacing: 6) {
                        callGlyph(call)
                        Text(call.title).font(.system(.caption, design: .monospaced))
                    }
                    if let result = call.result, !result.isEmpty {
                        CodeWell(code: result, language: nil)
                    }
                }
                if let result = block.result, !result.isEmpty {
                    CodeWell(code: result, language: nil)
                }
            }
            .padding(.top, 6)
        } label: {
            HStack(spacing: 6) {
                toolGlyph
                Text(toolTitle)
                    .font(.system(.caption, design: .monospaced).weight(.medium))
                    .lineLimit(1)
                if block.waiting {
                    Text("Waiting for you")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(Theme.vermilion)
                } else if let outcome = block.outcome {
                    Text(outcome == .denied ? "Denied" : "Stopped")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(outcome == .denied ? Theme.vermilion : .secondary)
                }
            }
        }
    }

    private var toolTitle: String {
        if block.name == "call", let first = block.calls.first { return first.title }
        return block.name
    }

    @ViewBuilder
    private var toolGlyph: some View {
        if block.waiting {
            Image(systemName: "hand.raised.fill").foregroundStyle(Theme.vermilion)
        } else if block.running {
            ProgressView().controlSize(.mini)
        } else if block.outcome != nil {
            Image(systemName: "xmark.circle").foregroundStyle(block.outcome == .denied ? Theme.vermilion : .secondary)
        } else {
            Image(systemName: "checkmark.circle").foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func callGlyph(_ call: NestedCall) -> some View {
        if call.waiting {
            Image(systemName: "hand.raised.fill").foregroundStyle(Theme.vermilion).font(.caption)
        } else if call.running {
            ProgressView().controlSize(.mini)
        } else if call.outcome != nil {
            Image(systemName: "xmark.circle").foregroundStyle(.secondary).font(.caption)
        } else {
            Image(systemName: "arrow.turn.down.right").foregroundStyle(.secondary).font(.caption)
        }
    }

    // MARK: receipts, reports, quotes, compaction

    private var receiptRow: some View {
        let label: String
        let symbol: String
        switch block.decision {
        case "allow_once": (label, symbol) = ("Allowed once", "checkmark.circle")
        case "always": (label, symbol) = ("Always allowed", "checkmark.circle.fill")
        case "deny": (label, symbol) = ("Denied", "xmark.circle")
        case "stopped": (label, symbol) = ("Stopped", "stop.circle")
        default: (label, symbol) = (block.decision.isEmpty ? "Decided" : block.decision, "circle")
        }
        return HStack(spacing: 6) {
            Image(systemName: symbol)
            Text([label, block.title, block.text].filter { !$0.isEmpty }.joined(separator: " · "))
                .lineLimit(2)
        }
        .font(.caption)
        .foregroundStyle(block.decision == "deny" ? Theme.vermilion : .secondary)
        .frame(maxWidth: .infinity, alignment: .center)
    }

    private var reportCard: some View {
        DisclosureGroup {
            Text(block.text)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .padding(.top, 4)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text("Subagents reported").font(.subheadline.weight(.semibold))
                Text(block.agents.map { $0.status.isEmpty ? $0.name : "\($0.name) · \($0.status)" }.joined(separator: ", "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .siloCard()
    }

    private var quoteCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !block.source.isEmpty {
                Label(block.source, systemImage: "quote.opening")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
            }
            ProseView(text: block.text)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .siloCard()
    }

    private var compactionRow: some View {
        Group {
            if block.running {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.mini)
                    Text("Summarizing earlier messages…")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            } else if block.text.isEmpty {
                Label("Summary stopped", systemImage: "rectangle.compress.vertical")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                DisclosureGroup {
                    Text(block.text)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .padding(.top, 4)
                } label: {
                    Label("Earlier messages summarized", systemImage: "rectangle.compress.vertical")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

/// A file or skill the Bot handed over. Save/download/preview arrive with the artifact phase.
struct ArtifactCard: View {
    let artifact: ArtifactInfo

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: artifact.artifactType == "skill" ? "book.closed" : "doc")
                .font(.title3)
                .foregroundStyle(Theme.cobalt)
                .frame(width: 32)
            VStack(alignment: .leading, spacing: 2) {
                Text(artifact.title.isEmpty ? artifact.name : artifact.title)
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(2)
                Text([artifact.artifactType == "skill" ? "Skill" : "File", artifact.size.map(formatBytes)]
                    .compactMap { $0 }.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .siloCard()
    }
}

struct ProseView: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Prose.parse(text)) { part in
                switch part {
                case .text(let value):
                    Text(attributed(value))
                        .font(.reply())
                        .textSelection(.enabled)
                case .code(let value, let language):
                    CodeWell(code: value, language: language)
                }
            }
        }
    }

    private func attributed(_ value: String) -> AttributedString {
        (try? AttributedString(markdown: value)) ?? AttributedString(value)
    }
}

struct CodeWell: View {
    let code: String
    let language: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let language, !language.isEmpty {
                Text(language.uppercased())
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 10)
                    .padding(.top, 6)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(.footnote, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(10)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
    }
}
