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
        VStack(spacing: 6) {
            controls
            attachmentChips
            if let error = dictation.error {
                Text(error).font(.silo(.footnote)).foregroundStyle(Theme.vermilion)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if let error = model.attachError {
                Text(error).font(.silo(.footnote)).foregroundStyle(Theme.vermilion)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            field
        }
        .padding(.horizontal, 12)
        .padding(.top, 6)
        .padding(.bottom, 6)
        .background {
            // Paper under the composer so the thread fades out behind it rather than showing through.
            LinearGradient(colors: [Theme.canvas.opacity(0), Theme.canvas], startPoint: .top, endPoint: .init(x: 0.5, y: 0.25))
                .ignoresSafeArea()
        }
        .onChange(of: photos) { _, items in loadPhotos(items) }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            Task { for url in urls { await attachFile(url) } }
        }
    }

    // MARK: controls row

    private var controls: some View {
        HStack(spacing: 6) {
            pickMenu
            Spacer(minLength: 0)
            // The meter only speaks up once the window is filling; compacting is also in the title menu.
            if let usage = model.usage, usage.window > 0, usage.fraction >= 0.7 {
                ContextMeter(usage: usage, canCompact: !model.isRunning && model.selectedChatID != nil) {
                    Task { await model.compact() }
                }
            }
        }
        .padding(.horizontal, 4)
    }

    /// The model and the thinking level as one quiet chip.
    private var pickMenu: some View {
        Menu {
            Section("Model") {
                ForEach(model.models, id: \.id) { option in
                    Button {
                        Task { await model.setModel(option.id) }
                    } label: {
                        Label(option.label.isEmpty ? option.id : option.label,
                              systemImage: option.id == model.activeModel ? "checkmark" : "")
                    }
                }
            }
            if !model.thinkingLevels.isEmpty {
                Section("Thinking") {
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
                }
            }
        } label: {
            HStack(spacing: 5) {
                Text(pickLabel).lineLimit(1)
                Image(systemName: "chevron.up.chevron.down").font(.system(size: 8, weight: .semibold))
            }
            .font(.silo(12.5, .medium))
            .foregroundStyle(Theme.ink2)
            .padding(.horizontal, 10)
            .frame(height: 26)
            .background(Theme.well, in: Capsule())
        }
        .disabled(model.models.isEmpty)
    }

    private var pickLabel: String {
        let id = model.activeModel
        let name = id.isEmpty ? "Model" : shortModel(id)
        return model.thinkingLevels.isEmpty ? name : "\(name) · \(thinkingLabel(model.fittedThinking))"
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
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink2)
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
                    .font(.system(size: 17, weight: .medium))
                    .foregroundStyle(Theme.ink)
                    .frame(width: 44, height: 44)
                    .background(Theme.surface, in: Circle())
                    .overlay(Circle().stroke(Theme.lineStrong))
                    .contentShape(Circle())
            }
            .accessibilityLabel("Attach")

            Group {
                if dictation.state == .recording {
                    recordingBar
                } else {
                    TextField(placeholder, text: $text, axis: .vertical)
                        .font(.silo(16))
                        .foregroundStyle(Theme.ink)
                        .tint(Theme.cobalt)
                        .lineLimit(1...6)
                        .focused($focused)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 11)
                }
            }
            .frame(minHeight: 44)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 22, style: .continuous).stroke(Theme.lineStrong))

            trailing
        }
        .sensoryFeedback(.impact(weight: .light), trigger: model.isRunning)
    }

    private var placeholder: String {
        model.selectedBot.map { "Message \($0.name)" } ?? "Message"
    }

    @ViewBuilder
    private var trailing: some View {
        if model.isRunning {
            Button {
                Task { await model.stopRun() }
            } label: {
                Image(systemName: "stop.fill")
                    .font(.system(size: 14, weight: .bold))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 44, height: 44)
                    .background(Theme.vermilion, in: Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Stop")
        } else if canSend {
            Button {
                send()
            } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 44, height: 44)
                    .background(Theme.ink, in: Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Send")
        } else if model.voiceEnabled && dictation.state != .recording {
            micButton
        }
    }

    private var micButton: some View {
        Button {
            Task { await dictation.start() }
        } label: {
            Group {
                if dictation.state == .transcribing {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "mic").font(.system(size: 17, weight: .medium))
                }
            }
            .foregroundStyle(Theme.ink)
            .frame(width: 44, height: 44)
            .background(Theme.surface, in: Circle())
            .overlay(Circle().stroke(Theme.lineStrong))
        }
        .buttonStyle(.plain)
        .disabled(dictation.state == .transcribing)
        .accessibilityLabel("Dictate")
    }

    private var recordingBar: some View {
        HStack(spacing: 10) {
            Circle().fill(Theme.vermilion).frame(width: 9, height: 9)
            Text(formatClock(dictation.elapsed))
                .font(.siloMono(.body))
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
            .buttonStyle(.plain)
            .foregroundStyle(Theme.onInk)
            .background(Theme.ink, in: Circle())
            .accessibilityLabel("Stop and transcribe")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 5)
        .frame(maxWidth: .infinity)
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
                    .font(.siloMono(.caption))
            }
            .foregroundStyle(tone)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Context \(Int((usage.fraction * 100).rounded())) percent full")
        .popover(isPresented: $showing) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Context window").font(.silo(.headline))
                ProgressView(value: usage.fraction).tint(tone)
                LabeledContent("Used", value: formatTokens(usage.used))
                LabeledContent("Model limit", value: formatTokens(usage.window))
                if usage.cacheRead > 0 { LabeledContent("From cache", value: formatTokens(usage.cacheRead)) }
                Text("Near the limit the conversation is summarized automatically; the thread keeps everything.")
                    .font(.silo(.footnote))
                    .foregroundStyle(.secondary)
                Button("Compact now") {
                    showing = false
                    onCompact()
                }
                .buttonStyle(.ink)
                .disabled(!canCompact)
            }
            .padding()
            .frame(minWidth: 260)
            .presentationCompactAdaptation(.popover)
        }
    }
}
