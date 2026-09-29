import Observation
import PhotosUI
import SiloClient
import SwiftUI
import UniformTypeIdentifiers

struct ComposerView: View {
    @Environment(AppModel.self) private var model
    @State private var text = ""
    @State private var photos: [PhotosPickerItem] = []
    @State private var importing = false
    @State private var dictation = Dictation()
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 8) {
            controls
            attachmentChips
            if let error = dictation.error {
                Text(error).font(.footnote).foregroundStyle(Theme.vermilion)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if let error = model.attachError {
                Text(error).font(.footnote).foregroundStyle(Theme.vermilion)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            field
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.bar)
        .onChange(of: photos) { _, items in loadPhotos(items) }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            Task { for url in urls { await attachFile(url) } }
        }
    }

    // MARK: controls row

    private var controls: some View {
        HStack(spacing: 6) {
            modelMenu
            thinkingMenu
            Spacer(minLength: 0)
            if let usage = model.usage, usage.window > 0 {
                ContextMeter(usage: usage, canCompact: !model.isRunning && model.selectedChatID != nil) {
                    Task { await model.compact() }
                }
            }
            Button {
                Task { await model.collectMemories() }
            } label: {
                Image(systemName: "brain")
            }
            .buttonStyle(.borderless)
            .disabled(model.selectedChatID == nil)
            .accessibilityLabel("Save memories from this chat")
        }
        .font(.footnote)
    }

    private var modelMenu: some View {
        Menu {
            ForEach(model.models, id: \.id) { option in
                Button {
                    Task { await model.setModel(option.id) }
                } label: {
                    Label(option.label.isEmpty ? option.id : option.label,
                          systemImage: option.id == model.activeModel ? "checkmark" : "")
                }
            }
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "cpu")
                Text(modelLabel).lineLimit(1)
                Image(systemName: "chevron.up.chevron.down").font(.caption2)
            }
            .foregroundStyle(.secondary)
        }
        .disabled(model.models.isEmpty || model.selectedChatID == nil)
    }

    private var modelLabel: String {
        if let option = model.activeModelOption, !option.label.isEmpty { return option.label }
        let id = model.activeModel
        return id.isEmpty ? "Model" : String(id.split(separator: "/").last ?? Substring(id))
    }

    @ViewBuilder
    private var thinkingMenu: some View {
        if !model.thinkingLevels.isEmpty {
            Menu {
                Button {
                    Task { await model.setThinking("") }
                } label: {
                    Label("Default", systemImage: model.fittedThinking.isEmpty ? "checkmark" : "")
                }
                ForEach(model.thinkingLevels, id: \.self) { level in
                    Button {
                        Task { await model.setThinking(level) }
                    } label: {
                        Label(thinkingLabel(level), systemImage: model.fittedThinking == level ? "checkmark" : "")
                    }
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: "brain.head.profile")
                    Text(thinkingLabel(model.fittedThinking)).lineLimit(1)
                }
                .foregroundStyle(.secondary)
            }
            .disabled(model.selectedChatID == nil)
        }
    }

    // MARK: attachments

    @ViewBuilder
    private var attachmentChips: some View {
        if !model.attachments.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    ForEach(model.attachments, id: \.path) { item in
                        HStack(spacing: 4) {
                            Image(systemName: "paperclip")
                            Text(item.name).lineLimit(1)
                            Button {
                                model.removeAttachment(item.path)
                            } label: {
                                Image(systemName: "xmark.circle.fill")
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel("Remove \(item.name)")
                        }
                        .font(.caption)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(Theme.well, in: Capsule())
                    }
                }
            }
        }
    }

    private func loadPhotos(_ items: [PhotosPickerItem]) {
        guard !items.isEmpty else { return }
        photos = []
        Task {
            for item in items {
                guard let data = try? await item.loadTransferable(type: Data.self) else { continue }
                let ext = item.supportedContentTypes.first?.preferredFilenameExtension ?? "jpg"
                let stamp = Int(Date().timeIntervalSince1970)
                await model.attach(name: "photo-\(stamp)-\(Int.random(in: 100...999)).\(ext)", data: data)
            }
        }
    }

    private func attachFile(_ url: URL) async {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url) else {
            model.attachError = "Could not read \(url.lastPathComponent)"
            return
        }
        await model.attach(name: url.lastPathComponent, data: data)
    }

    // MARK: field

    private var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !model.attachments.isEmpty
    }

    private var field: some View {
        HStack(alignment: .bottom, spacing: 8) {
            Menu {
                PhotosPicker(selection: $photos, maxSelectionCount: 5, matching: .images) {
                    Label("Photo Library", systemImage: "photo")
                }
                Button {
                    importing = true
                } label: {
                    Label("Files", systemImage: "folder")
                }
            } label: {
                Image(systemName: "plus")
                    .font(.system(size: 15, weight: .semibold))
                    .frame(width: 30, height: 30)
                    .background(Theme.well, in: Circle())
            }
            .accessibilityLabel("Attach")

            if dictation.state == .recording {
                recordingBar
            } else {
            TextField("Message", text: $text, axis: .vertical)
                .lineLimit(1...6)
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.bubble))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.bubble).stroke(Theme.line, lineWidth: 1))
                .focused($focused)
            }

            if model.voiceEnabled && text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                && dictation.state != .recording && !model.isRunning {
                micButton
            }

            if model.isRunning {
                Button {
                    Task { await model.stopRun() }
                } label: {
                    Image(systemName: "stop.fill")
                        .font(.system(size: 15, weight: .bold))
                        .frame(width: 30, height: 30)
                }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.circle)
                .tint(Theme.vermilion)
                .accessibilityLabel("Stop")
            } else {
                Button {
                    send()
                } label: {
                    Image(systemName: "arrow.up")
                        .font(.system(size: 15, weight: .bold))
                        .frame(width: 30, height: 30)
                }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.circle)
                .disabled(!canSend)
                .accessibilityLabel("Send")
            }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: model.isRunning)
    }

    private var micButton: some View {
        Button {
            Task { await dictation.start() }
        } label: {
            Group {
                if dictation.state == .transcribing {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "mic")
                }
            }
            .font(.system(size: 15, weight: .semibold))
            .frame(width: 30, height: 30)
            .background(Theme.well, in: Circle())
        }
        .disabled(dictation.state == .transcribing)
        .accessibilityLabel("Dictate")
    }

    private var recordingBar: some View {
        HStack(spacing: 10) {
            Circle().fill(Theme.vermilion).frame(width: 9, height: 9)
            Text(formatClock(dictation.elapsed))
                .font(.system(.body, design: .monospaced))
            Spacer()
            Button("Cancel") { dictation.cancel() }
                .buttonStyle(.borderless)
            Button {
                guard let audio = dictation.finish() else { return }
                Task {
                    if let spoken = await model.transcribe(audio) { text = appendDictation(text, spoken) }
                    dictation.done()
                    focused = true
                }
            } label: {
                Image(systemName: "checkmark")
                    .font(.system(size: 15, weight: .bold))
                    .frame(width: 30, height: 30)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.circle)
            .accessibilityLabel("Stop and transcribe")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 5)
        .frame(maxWidth: .infinity)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.bubble))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.bubble).stroke(Theme.vermilion.opacity(0.5), lineWidth: 1))
    }

    private func send() {
        guard canSend else { return }
        let value = text
        text = ""
        Task { await model.send(value) }
    }
}

/// How full the model's context window was on the last turn; tapping compacts the conversation.
struct ContextMeter: View {
    let usage: Usage
    let canCompact: Bool
    let onCompact: () -> Void
    @State private var showing = false

    private var tone: Color {
        usage.fraction >= 0.9 ? Theme.vermilion : usage.fraction >= 0.7 ? .primary : .secondary
    }

    var body: some View {
        Button {
            showing = true
        } label: {
            HStack(spacing: 5) {
                ZStack {
                    Circle().stroke(tone.opacity(0.2), lineWidth: 2)
                    Circle().trim(from: 0, to: max(0.02, usage.fraction))
                        .stroke(tone, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                        .rotationEffect(.degrees(-90))
                }
                .frame(width: 16, height: 16)
                Text("\(Int((usage.fraction * 100).rounded()))%")
                    .font(.system(.caption, design: .monospaced))
            }
            .foregroundStyle(tone)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Context \(Int((usage.fraction * 100).rounded())) percent full")
        .popover(isPresented: $showing) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Context window").font(.headline)
                ProgressView(value: usage.fraction).tint(tone)
                LabeledContent("Used", value: formatTokens(usage.used))
                LabeledContent("Model limit", value: formatTokens(usage.window))
                if usage.cacheRead > 0 { LabeledContent("From cache", value: formatTokens(usage.cacheRead)) }
                Text("Near the limit the conversation is summarized automatically; the thread keeps everything.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Button("Compact now") {
                    showing = false
                    onCompact()
                }
                .buttonStyle(.borderedProminent)
                .disabled(!canCompact)
            }
            .padding()
            .frame(minWidth: 260)
            .presentationCompactAdaptation(.popover)
        }
    }
}
