import Observation
import PhotosUI
import SiloClient
import SwiftUI
import UniformTypeIdentifiers

/// The composer: a `surface` card with its field and one row in three groups. What goes in (the
/// paperclip, the mic), who answers (the model, the thinking level), and on the right the
/// conversation (its ring, Talk, Send). DESIGN.md § Composer.
struct ComposerView: View {
    @Environment(AppModel.self) private var model
    @State private var text = ""
    @State private var photos: [PhotosPickerItem] = []
    @State private var importing = false
    @State private var picking = false
    @State private var dictation = Dictation()
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 6) {
            if let said = model.said { SaidNotice(said: said).transition(.rise) }
            slashMenu
            if let error = dictation.error ?? model.attachError {
                ErrorLine(text: error).padding(.horizontal, 6).transition(.rise)
            }
            card
        }
        .padding(.horizontal, 10)
        .padding(.top, 6)
        .padding(.bottom, 6)
        .background {
            // Paper under the composer so the thread fades out behind it rather than showing through.
            LinearGradient(colors: [Theme.canvas.opacity(0), Theme.canvas], startPoint: .top, endPoint: .init(x: 0.5, y: 0.2))
                .ignoresSafeArea()
                .allowsHitTesting(false)
        }
        .animation(Motion.state, value: model.said)
        .onChange(of: photos) { _, items in loadPhotos(items) }
        .onChange(of: model.composerInsert, initial: true) { _, insert in
            // An idea from the Dashboard: below a draft already started, never sent for the owner.
            guard let insert, !insert.isEmpty else { return }
            model.composerInsert = nil
            if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                text = insert
            } else if !text.contains(insert) {
                text = text.trimmingCharacters(in: .whitespacesAndNewlines) + "\n\n" + insert
            }
            focused = true
        }
        .photosPicker(isPresented: $picking, selection: $photos, maxSelectionCount: 5, matching: .images)
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            Task { for url in urls { await attachFile(url) } }
        }
    }

    private var card: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.composer, style: .continuous)
        return VStack(alignment: .leading, spacing: 4) {
            attachmentChips
            if dictation.state == .recording {
                recordingBar
            } else {
                TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(Theme.ink3), axis: .vertical)
                    .font(.silo(16))
                    .foregroundStyle(Theme.ink)
                    .tint(Theme.cobalt)
                    .lineLimit(1...7)
                    .focused($focused)
                    .padding(.horizontal, 10)
                    .padding(.top, 8)
                    .padding(.bottom, 4)
                    .disabled(dictation.state == .transcribing)
            }
            toolbar
        }
        .padding(6)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(focused ? Theme.lineStrong : Theme.line))
        .floatShadow()
        .animation(Motion.quiet(0.16), value: focused)
        .animation(Motion.state, value: dictation.state == .recording)
        .onChange(of: CallModel.shared.live) { _, live in
            if live { dictation.cancel() }
        }
    }

    private var placeholder: String { model.selectedBot.map { "Ask \($0.name)…" } ?? "Ask…" }

    // MARK: the row

    private var toolbar: some View {
        HStack(spacing: 2) {
            attachMenu
            // A call has the microphone.
            if model.voiceEnabled && dictation.state != .recording && !CallModel.shared.live { micButton }
            modelMenu
            if !model.thinkingLevels.isEmpty { thinkingMenu }
            Spacer(minLength: 4)
            if model.selectedChatID != nil { ConversationRing() }
            if model.callEnabled, !CallModel.shared.live {
                Button { model.call(chat: model.selectedChatID ?? "new", screen: false) } label: {
                    Image(systemName: "waveform")
                }
                .buttonStyle(.glyph(tint: Theme.ink2, side: 36))
                .accessibilityLabel("Talk")
            }
            SendStop(sending: model.isRunning, canSend: canSend) {
                send()
            } onStop: {
                Task { await model.stopRun() }
            }
        }
    }

    private var attachMenu: some View {
        SiloMenu {
            MenuRow(title: "Photo library", symbol: "photo") { picking = true }
            MenuRow(title: "Files", symbol: "folder") { importing = true }
        } label: {
            Image(systemName: "paperclip").font(.system(size: 16)).foregroundStyle(Theme.ink2).frame(width: 36, height: 36)
        }
        .accessibilityLabel("Attach")
    }

    /// The model's own name only; the menu keeps the provider.
    private var modelMenu: some View {
        SiloMenu {
            MenuHeading(text: "Model")
            ForEach(model.models, id: \.id) { option in
                MenuRow(title: option.label.isEmpty ? shortModel(option.id) : option.label,
                        detail: option.id.split(separator: "/").first.map(String.init),
                        checked: option.id == model.activeModel) {
                    Task { await model.setModel(option.id) }
                }
            }
        } label: {
            Text(model.activeModel.isEmpty ? "Model" : shortModel(model.activeModel))
                .font(.silo(12.5, .medium))
                .foregroundStyle(Theme.ink2)
                .lineLimit(1)
                .padding(.horizontal, 8)
                .frame(height: 28)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
        }
        .disabled(model.models.isEmpty)
        .layoutPriority(-1)
        .accessibilityLabel("Model: \(model.activeModel)")
    }

    private var thinkingMenu: some View {
        SiloMenu {
            MenuHeading(text: "Thinking")
            MenuRow(title: "Default", checked: model.fittedThinking.isEmpty) { Task { await model.setThinking("") } }
            ForEach(model.thinkingLevels, id: \.self) { level in
                MenuRow(title: thinkingLabel(level), checked: model.fittedThinking == level) {
                    Task { await model.setThinking(level) }
                }
            }
        } label: {
            Image(systemName: model.fittedThinking.isEmpty ? "lightbulb" : "lightbulb.fill")
                .font(.system(size: 14))
                .foregroundStyle(model.fittedThinking.isEmpty ? Theme.ink3 : Theme.ink)
                .frame(width: 32, height: 36)
        }
        .accessibilityLabel("Thinking: \(thinkingLabel(model.fittedThinking))")
    }

    private var micButton: some View {
        Button {
            Task { await dictation.start() }
        } label: {
            ZStack {
                if dictation.state == .transcribing {
                    Spinner(size: 14)
                } else {
                    Image(systemName: "mic").font(.system(size: 15))
                }
            }
        }
        .buttonStyle(.glyph(tint: Theme.ink2, side: 36))
        .disabled(dictation.state == .transcribing)
        .accessibilityLabel("Dictate")
    }

    private var recordingBar: some View {
        HStack(spacing: 10) {
            Circle().fill(Theme.vermilion).frame(width: 8, height: 8).breathing()
            Text(formatClock(dictation.elapsed)).font(.siloMono(15)).foregroundStyle(Theme.ink).monospacedDigit()
            Spacer()
            Button("Cancel") { dictation.cancel() }.buttonStyle(.ghost).controlSize(.small)
            Button {
                guard let audio = dictation.finish() else { return }
                Task {
                    if let spoken = await model.transcribe(audio) { text = appendDictation(text, spoken) }
                    dictation.done()
                    focused = true
                }
            } label: {
                Image(systemName: "checkmark").font(.system(size: 14, weight: .semibold))
            }
            .buttonStyle(.ink)
            .controlSize(.small)
            .accessibilityLabel("Stop and transcribe")
        }
        .padding(.horizontal, 10)
        .frame(minHeight: 40)
        .transition(.opacity)
    }

    // MARK: slash menu

    /// While a bare "/name" is typed: the commands it could be, a press away.
    @ViewBuilder
    private var slashMenu: some View {
        if let query = slashQuery(text) {
            let matches = matchCommands(model.commands, query)
            if !matches.isEmpty {
                let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                VStack(spacing: 0) {
                    ForEach(matches.prefix(6), id: \.name) { command in
                        Button {
                            if command.args.isEmpty {
                                text = ""
                                Task { await model.send("/" + command.name) }
                            } else {
                                text = "/\(command.name) "
                                focused = true
                            }
                        } label: {
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text("/\(command.name)").font(.siloMono(13.5, .medium)).foregroundStyle(Theme.ink)
                                if !command.args.isEmpty {
                                    Text(command.args).font(.siloMono(12.5)).foregroundStyle(Theme.ink3)
                                }
                                Text(command.summary).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                                Spacer(minLength: 0)
                            }
                            .padding(.horizontal, 14)
                            .frame(minHeight: 40)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(RowPressStyle())
                    }
                }
                .padding(.vertical, 4)
                .background(Theme.surface, in: shape)
                .overlay(shape.stroke(Theme.line))
                .slipShadow()
                .transition(.rise)
            }
        }
    }

    // MARK: attachments

    @ViewBuilder
    private var attachmentChips: some View {
        if !model.attachments.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    ForEach(model.attachments, id: \.path) { item in
                        HStack(spacing: 6) {
                            Image(systemName: "paperclip").font(.system(size: 11))
                            Text(item.name).font(.silo(12.5)).lineLimit(1)
                            Button { model.removeAttachment(item.path) } label: {
                                Image(systemName: "xmark").font(.system(size: 9, weight: .semibold)).frame(width: 18, height: 18)
                            }
                            .buttonStyle(.press)
                            .accessibilityLabel("Remove \(item.name)")
                        }
                        .foregroundStyle(Theme.ink2)
                        .padding(.leading, 8)
                        .padding(.trailing, 4)
                        .frame(height: 28)
                        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                        .transition(.rise)
                    }
                }
                .padding(.horizontal, 4)
                .padding(.top, 4)
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

    // MARK: sending

    private var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !model.attachments.isEmpty
    }

    private func send() {
        guard canSend else { return }
        let value = text
        text = ""
        Task {
            // A refused command comes back to fix.
            if await !model.send(value), text.isEmpty { text = value }
        }
    }
}

/// Send and Stop are one button: the arrow lifts out while the square rotates in (320ms settle),
/// and the fill crossfades over 240ms. Nothing jumps.
private struct SendStop: View {
    let sending: Bool
    let canSend: Bool
    let onSend: () -> Void
    let onStop: () -> Void
    @Environment(\.accessibilityReduceMotion) private var still

    var body: some View {
        let lift = sending && !still
        Button {
            if sending { onStop() } else { onSend() }
        } label: {
            ZStack {
                Image(systemName: "arrow.up")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(canSend && !sending ? Theme.onInk : Theme.ink3)
                    .offset(y: lift ? -10 : 0)
                    .scaleEffect(lift ? 0.6 : 1)
                    .opacity(sending ? 0 : 1)
                RoundedRectangle(cornerRadius: 3, style: .continuous)
                    .fill(Theme.onAccent)
                    .frame(width: 11, height: 11)
                    .rotationEffect(.degrees(sending || still ? 0 : -90))
                    .scaleEffect(sending || still ? 1 : 0.6)
                    .opacity(sending ? 1 : 0)
            }
            .animation(Motion.settle(0.32), value: sending)
            .frame(width: 36, height: 36)
            .background(sending ? Theme.vermilion : canSend ? Theme.ink : Theme.pressed,
                        in: RoundedRectangle(cornerRadius: 11, style: .continuous))
            .animation(Motion.quiet(0.24), value: sending)
            .animation(Motion.quiet(0.24), value: canSend)
        }
        .buttonStyle(PressStyle(scale: 0.94))
        .disabled(!sending && !canSend)
        .sensoryFeedback(.impact(weight: .light), trigger: sending)
        .accessibilityLabel(sending ? "Stop this reply" : "Send message")
    }
}

/// The context ring, and behind it what the conversation needs that is not about this message: the
/// window, Compact now, Save memories, Follow live steps.
private struct ConversationRing: View {
    @Environment(AppModel.self) private var model
    @AppStorage("autoExpand") private var follow = false

    private var fraction: Double { model.usage?.fraction ?? 0 }
    private var tone: Color { fraction >= 0.9 ? Theme.vermilion : fraction >= 0.7 ? Theme.ink : Theme.ink3 }

    var body: some View {
        SiloMenu {
            panel
        } label: {
            ZStack {
                Circle().stroke(Theme.pressed, lineWidth: 2)
                Circle().trim(from: 0, to: fraction > 0 ? max(0.02, fraction) : 0)
                    .stroke(tone, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                    .rotationEffect(.degrees(-90))
            }
            .frame(width: 16, height: 16)
            .frame(width: 34, height: 36)
        }
        .accessibilityLabel("Context \(Int((fraction * 100).rounded())) percent full")
    }

    @ViewBuilder
    private var panel: some View {
        if let usage = model.usage, usage.window > 0 {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Context window").font(.silo(13, .medium)).foregroundStyle(Theme.ink)
                    Spacer()
                    Text("\(Int((usage.fraction * 100).rounded()))%").font(.siloMono(12.5)).foregroundStyle(Theme.ink3)
                }
                GeometryReader { box in
                    ZStack(alignment: .leading) {
                        Capsule().fill(Theme.pressed)
                        Capsule().fill(tone).frame(width: box.size.width * usage.fraction)
                    }
                }
                .frame(height: 4)
                HStack(spacing: 12) {
                    figure("Used", formatTokens(usage.used))
                    figure("Limit", formatTokens(usage.window))
                    if usage.cacheRead > 0 { figure("Cache", formatTokens(usage.cacheRead)) }
                }
                Text("Near the limit the conversation is summarized automatically; the thread keeps everything.")
                    .font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
            }
            .padding(12)
            MenuDivider()
        }
        MenuRow(title: "Compact now", symbol: "rectangle.compress.vertical", enabled: !model.isRunning) { Task { await model.compact() } }
        MenuRow(title: "Save memories", symbol: "brain") { Task { await model.collectMemories() } }
        Toggle(isOn: $follow) {
            HStack(spacing: 10) {
                Image(systemName: "list.bullet.below.rectangle").font(.system(size: 14)).foregroundStyle(Theme.ink2).frame(width: 18)
                Text("Follow live steps").font(.silo(14.5)).foregroundStyle(Theme.ink)
            }
        }
        .toggleStyle(.silo)
        .padding(.horizontal, 12)
        .frame(minHeight: 42)
    }

    private func figure(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(label).font(.silo(11.5)).foregroundStyle(Theme.ink3)
            Text(value).font(.siloMono(12.5)).foregroundStyle(Theme.ink)
        }
    }
}

/// What a slash command said back, over the composer; a command that destroys something asks here
/// first. The cross puts it away.
private struct SaidNotice: View {
    @Environment(AppModel.self) private var model
    let said: Said

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Text(said.text)
                .font(.silo(13.5))
                .foregroundStyle(Theme.ink2)
                .frame(maxWidth: .infinity, alignment: .leading)
                .textSelection(.enabled)
            if let label = said.confirmLabel {
                Button(label) { Task { await model.confirmSaid() } }
                    .buttonStyle(.ink)
                    .controlSize(.small)
            }
            Button { model.said = nil } label: {
                Image(systemName: "xmark").font(.system(size: 11, weight: .semibold))
            }
            .buttonStyle(.glyph(tint: Theme.ink3, side: 26))
            .accessibilityLabel("Dismiss")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}
