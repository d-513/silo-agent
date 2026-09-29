import Observation
import QuickLook
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
            if let path = presentedPath { PresentCard(path: path) } else { toolRow }
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
            if let artifact = block.artifact { ArtifactCard(artifact: artifact, runID: block.runID) }
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
        FoldRow {
            HStack(spacing: 6) {
                if block.streaming { ProgressView().controlSize(.mini) }
                Text(block.streaming ? "Thinking…" : "Thought")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } content: {
            Text(block.text)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        }
    }

    // MARK: tools

    private var toolRow: some View {
        FoldRow {
            HStack(spacing: 6) {
                toolGlyph
                Text(toolTitle)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(.secondary)
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
        } content: {
            VStack(alignment: .leading, spacing: 8) {
                if !block.args.isEmpty {
                    CodeWell(code: prettyJSON(block.args), language: nil)
                }
                ForEach(block.calls) { call in
                    HStack(spacing: 6) {
                        callGlyph(call)
                        Text(call.title).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                    }
                    if let result = call.result, !result.isEmpty {
                        CodeWell(code: result, language: nil)
                    }
                }
                if let result = block.result, !result.isEmpty {
                    CodeWell(code: result, language: nil)
                }
            }
        }
    }

    /// A finished `present` of a user-facing path shows the file itself; `bot/…` stays a quiet row.
    private var presentedPath: String? {
        guard block.name == "present", !block.running, let result = block.result, !result.isEmpty,
              !result.hasPrefix("error:") else { return nil }
        let path = parseToolArgs(block.args)["path"] ?? ""
        return path.isEmpty || isBotScratch(path) ? nil : path
    }

    private var toolTitle: String {
        if block.name == "call", let first = block.calls.first { return first.title }
        let action = toolAction(name: block.name, args: block.args)
        return action.isEmpty ? block.name : "\(block.name) · \(action)"
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
        FoldRow {
            VStack(alignment: .leading, spacing: 2) {
                Text("Subagents reported").font(.subheadline.weight(.semibold))
                Text(block.agents.map { $0.status.isEmpty ? $0.name : "\($0.name) · \($0.status)" }.joined(separator: ", "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } content: {
            Text(block.text)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
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
                FoldRow {
                    Label("Earlier messages summarized", systemImage: "rectangle.compress.vertical")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } content: {
                    Text(block.text)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
            }
        }
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
                    Image(systemName: artifact.artifactType == "skill" ? "book.closed" : fileSymbol)
                        .font(.title3)
                        .foregroundStyle(Theme.cobalt)
                        .frame(width: 32)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(artifact.title.isEmpty ? artifact.name : artifact.title)
                            .font(.subheadline.weight(.semibold))
                            .multilineTextAlignment(.leading)
                            .lineLimit(2)
                        Text(subtitle)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                    if loading { ProgressView().controlSize(.small) }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if pending {
                Button {
                    saving = true
                    Task {
                        await model.saveSkill(artifact, runID: runID)
                        saving = false
                    }
                } label: {
                    Label("Save skill", systemImage: "tray.and.arrow.down").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(saving)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .siloCard()
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
                Image(systemName: "doc").foregroundStyle(Theme.cobalt)
                VStack(alignment: .leading, spacing: 1) {
                    Text(name).font(.subheadline.weight(.semibold)).lineLimit(1)
                    Text([file.map { formatBytes($0.size) }, "in Files"].compactMap { $0 }.joined(separator: " · "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                if file != nil {
                    Button {
                        Task { await open() }
                    } label: {
                        Image(systemName: "arrow.up.forward.square")
                    }
                    .accessibilityLabel("Open")
                }
            }
            if let failure {
                Text(failure).font(.footnote).foregroundStyle(Theme.vermilion)
            } else if let file {
                content(file)
            } else {
                RoundedRectangle(cornerRadius: 8).fill(Theme.well).frame(height: 120)
                    .overlay { ProgressView() }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .siloCard()
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
                Text("Open to see more.").font(.caption).foregroundStyle(.secondary)
            }
        } else if file.binary {
            Text("Preview not available. Open to view.").font(.footnote).foregroundStyle(.secondary)
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


/// A quiet expandable row: a small chevron and a secondary label, no tint, content indented below.
struct FoldRow<Label: View, Content: View>: View {
    @State private var open = false
    @ViewBuilder let label: Label
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.snappy) { open.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(open ? 90 : 0))
                        .frame(width: 10)
                    label
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                content
                    .padding(.top, 6)
                    .padding(.leading, 16)
                    .transition(.opacity)
            }
        }
    }
}
