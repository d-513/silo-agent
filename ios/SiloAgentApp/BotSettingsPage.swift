import SiloClient
import SwiftUI

/// Name, description, SOUL, and the Bot's default model. A chat's own override still wins.
struct BotSettingsPage: View {
    @Environment(AppModel.self) private var model
    @State private var name = ""
    @State private var description = ""
    @State private var soul = ""
    @State private var botModel = ""
    @State private var crest: Int32 = 0
    /// The Bot as `GetBot` has it: the list leaves SOUL out, so the form starts from this.
    @State private var full: Silo_V1_Bot?
    @State private var saved = 0

    var body: some View {
        PageScroll(spacing: 20) {
            PanelSection(title: "General", footer: "A chat's own model choice overrides this.") {
                VStack(alignment: .leading, spacing: 14) {
                    HStack(alignment: .bottom, spacing: 8) {
                        CrestSelect(value: $crest)
                        SiloField(label: "Name", prompt: "Name", text: $name)
                    }
                    SiloField(label: "Description", prompt: "What is this Bot for?", text: $description, axis: .vertical, lines: 1...4)
                    SiloPicker(label: "Default model", options: modelOptions, selection: $botModel)
                }
                .padding(14)
            }
            PanelSection(title: "SOUL", footer: "Who the Bot is and how it behaves. Injected into every run.") {
                MachineEditor(text: $soul, mono: true, minHeight: 180).padding(14)
            }
            ProactivitySection()
            TimeZoneSection()
            if model.speechEnabled { VoiceSection() }
            if model.selectedBot?.mailAddress.isEmpty == false { MailSettingsSection() }
            PanelSection(title: "Dangerous") {
                PanelRow(title: "Delete Bot", detail: "Removes the machine, the Bot, and its workspace. This cannot be undone.", trailing: {
                    ArmedButton(title: "Delete", symbol: "trash", small: true) { Task { await delete() } }
                })
            }
        }
        .pageActions {
            SaveButton(enabled: dirty && !name.trimmingCharacters(in: .whitespaces).isEmpty) { await save() }
                .controlSize(.small)
                .padding(.trailing, 6)
        }
        .task(id: model.selectedBotID) { await load() }
        .sensoryFeedback(.success, trigger: saved)
    }

    private var modelOptions: [(value: String, label: String)] {
        [(value: "", label: "Operator default")] + model.models.map { (value: $0.id, label: $0.label.isEmpty ? $0.id : $0.label) }
    }

    private var dirty: Bool {
        guard let bot = full else { return false }
        return name != bot.name || description != bot.description_p || soul != bot.soul || botModel != bot.model || crest != bot.crest
    }

    private func load() async {
        guard let id = model.selectedBotID else { return }
        do {
            let bot = try await model.client.getBot(id)
            guard id == model.selectedBotID else { return }
            full = bot
            fill(bot)
        } catch { await model.report(error) }
    }

    private func fill(_ bot: Silo_V1_Bot) {
        name = bot.name
        description = bot.description_p
        soul = bot.soul
        botModel = bot.model
        crest = bot.crest
    }

    private func save() async -> Bool {
        guard let bot = full else { return false }
        do {
            let fresh = try await model.client.updateBot(bot, name: name, description: description, soul: soul, model: botModel, crest: crest)
            full = fresh
            model.applyBot(fresh)
            saved += 1
            return true
        } catch {
            await model.report(error)
            return false
        }
    }

    private func delete() async {
        guard let id = model.selectedBotID else { return }
        do {
            try await model.client.deleteBot(id)
            await model.botDeleted(id)
        } catch { await model.report(error) }
    }
}

/// The Bot's time zone: follow the owner's device (each message carries it), or fixed where the
/// owner put it. Automations' schedules run in this zone; the machine itself is always UTC.
struct TimeZoneSection: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let bot = model.selectedBot {
            PanelSection(title: "Time zone", footer: bot.timeZonePinned
                ? "The Bot stays in this zone wherever you are. Automations run on its clock."
                : "The Bot moves with you: each message carries your device's zone. Automations run on its clock.") {
                Segmented(options: [(false, "Follow my device"), (true, "Fixed")], selection: pinned(bot))
                    .padding(10)
                if bot.timeZonePinned {
                    NavigationLink {
                        SiloPage(title: "Time zone") {
                            ZonePicker(selected: bot.timeZone) { zone in Task { await set(zone: zone, pinned: true) } }
                        }
                    } label: {
                        PanelRow(title: "Zone", chevron: true, trailing: {
                            Text(zoneLabel(bot.timeZone)).font(.silo(13.5)).foregroundStyle(Theme.ink2).lineLimit(1)
                        })
                    }
                    .buttonStyle(RowPressStyle())
                } else {
                    PanelRow(title: "Now", trailing: {
                        Text(zoneLabel(bot.timeZone.isEmpty ? TimeZone.current.identifier : bot.timeZone))
                            .font(.silo(13.5))
                            .foregroundStyle(Theme.ink2)
                            .lineLimit(1)
                    })
                }
            }
        }
    }

    private func pinned(_ bot: Silo_V1_Bot) -> Binding<Bool> {
        Binding(get: { bot.timeZonePinned }, set: { pinned in
            Task { await set(zone: pinned ? (bot.timeZone.isEmpty ? TimeZone.current.identifier : bot.timeZone) : TimeZone.current.identifier, pinned: pinned) }
        })
    }

    private func set(zone: String, pinned: Bool) async {
        guard let bot = model.selectedBot else { return }
        do { model.applyBot(try await model.client.setBotTimeZone(bot, zone: zone, pinned: pinned)) } catch { await model.report(error) }
    }
}

/// "Europe/Warsaw (GMT+2)": the name and its offset now.
func zoneLabel(_ id: String) -> String {
    guard let zone = TimeZone(identifier: id) else { return id.isEmpty ? "Not set" : id }
    let city = id.split(separator: "/").last.map { $0.replacingOccurrences(of: "_", with: " ") } ?? id
    return "\(city) (\(zone.abbreviation() ?? "GMT"))"
}

private struct ZonePicker: View {
    @Environment(\.dismiss) private var dismiss
    let selected: String
    let pick: (String) -> Void
    @State private var query = ""

    private var zones: [String] {
        let all = TimeZone.knownTimeZoneIdentifiers
        guard !query.isEmpty else { return all }
        return all.filter { $0.localizedCaseInsensitiveContains(query.replacingOccurrences(of: " ", with: "_")) }
    }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                SiloField(prompt: "City or region", text: $query)
                    .autocorrectionDisabled()
                if zones.isEmpty {
                    EmptyWell(symbol: "globe", title: "No zone by that name")
                } else {
                    LazyVStack(spacing: 0) {
                        ForEach(zones, id: \.self) { zone in
                            if zone != zones.first { Theme.line.frame(height: 1) }
                            row(zone)
                        }
                    }
                    .background(Theme.surface, in: shape)
                    .clipShape(shape)
                    .overlay(shape.stroke(Theme.line, lineWidth: 1))
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 14)
            .padding(.bottom, 32)
            .frame(maxWidth: 760)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
    }

    private func row(_ zone: String) -> some View {
        PanelRow(title: zone.split(separator: "/").last.map { $0.replacingOccurrences(of: "_", with: " ") } ?? zone, detail: zone, action: {
            pick(zone)
            dismiss()
        }, trailing: {
            Text(TimeZone(identifier: zone)?.abbreviation() ?? "").font(.siloMono(12)).foregroundStyle(Theme.ink3)
            if zone == selected {
                Image(systemName: "checkmark").font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.cobalt)
            }
        })
    }
}

/// How much the Bot does unasked: one choice that sets the Heartbeat's schedule and whether a
/// background run may write into the main chat. It applies on its own, apart from Save.
private struct ProactivitySection: View {
    @Environment(AppModel.self) private var model
    @State private var busy = false

    private static let levels: [(id: String, label: String, note: String)] = [
        ("off", "Off", "Only when you ask. No Heartbeat."),
        ("quiet", "Quiet", "A morning check-in. News goes to the Feed; questions wait on the Dashboard."),
        ("active", "Active", "Check-ins through the day, never at night. It may also write to you in the main chat."),
    ]

    var body: some View {
        let bot = model.selectedBot
        let current = Self.levels.first { $0.id == bot?.proactivity } ?? Self.levels[0]
        let custom = bot?.heartbeatCustom == true
            ? " The Heartbeat runs on a schedule set by hand (Automations). Choosing a level puts it back on that level's schedule."
            : ""
        let choice = Binding<String>(get: { current.id }, set: { level in
            Task { await choose(level) }
        })
        PanelSection(title: "Proactivity", footer: current.note + custom) {
            Segmented(options: Self.levels.map { (value: $0.id, label: $0.label) }, selection: choice)
                .disabled(busy)
                .padding(10)
        }
    }

    private func choose(_ level: String) async {
        guard let bot = model.selectedBot, !busy, level != bot.proactivity || bot.heartbeatCustom else { return }
        busy = true
        defer { busy = false }
        do { model.applyBot(try await model.client.setProactivity(bot, level: level)) } catch { await model.report(error) }
    }
}

/// How the Bot sounds, for Read aloud and calls: a speech model from the operator's list, one of its
/// voices, and a speed. It saves on its own, apart from Save, and Hear it tries a choice first.
/// Web: `web/src/BotVoice.tsx`.
private struct VoiceSection: View {
    @Environment(AppModel.self) private var model
    /// The model in the picker; empty follows the operator's default.
    @State private var chosen = ""
    @State private var list: Silo_V1_ListVoicesResponse?
    @State private var busy = false
    /// A voice typed for a provider that lists none.
    @State private var typed = ""
    @State private var heard = false

    /// What a voice can be set to; 0 is the model's own.
    private static let speeds: [Double] = [0.85, 0, 1.15, 1.3]

    var body: some View {
        if let bot = model.selectedBot {
            PanelSection(title: "Voice", footer: "How \(bot.name) sounds when it reads a reply aloud or talks with you on a call.") {
                VStack(alignment: .leading, spacing: 14) {
                    speechRow(bot)
                    voiceRow(bot)
                    speedRow(bot)
                }
                .padding(14)
            }
            .task(id: bot.id) {
                chosen = bot.speechModel
                typed = bot.voice
                await load(bot.id)
            }
        }
    }

    private func speechRow(_ bot: Silo_V1_Bot) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            SiloPicker(label: "Speech model", options: modelOptions, selection: Binding(get: { chosen }, set: { speech in
                chosen = speech
                typed = ""
                Task {
                    await save(speechModel: speech, voice: "")
                    await load(bot.id)
                }
            }))
            .disabled(busy)
            hint("From the operator's allowed speech models. Voices differ from one to the next.")
        }
    }

    private func voiceRow(_ bot: Silo_V1_Bot) -> some View {
        let voices = list?.voices ?? []
        return VStack(alignment: .leading, spacing: 6) {
            if list == nil {
                SkeletonRows(count: 1)
            } else if voices.isEmpty {
                SiloField(label: "Voice", prompt: "Provider's default", text: $typed, mono: true)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .disabled(busy)
                    .task(id: typed) {
                        // Saves a moment after the last key, as the web does on leaving the field.
                        let voice = typed.trimmingCharacters(in: .whitespaces)
                        guard voice != bot.voice else { return }
                        try? await Task.sleep(for: .seconds(1))
                        guard !Task.isCancelled else { return }
                        await save(voice: voice)
                    }
                hint("This model does not list its voices: type one from its provider's documentation, or leave it empty.")
            } else {
                SiloPicker(label: "Voice", options: voiceOptions(bot), selection: Binding(get: { current(bot) }, set: { id in
                    Task { await save(voice: id) }
                }))
                .disabled(busy)
                hint("Automatic picks one for \(bot.name)'s language.")
            }
            HStack(spacing: 10) {
                Button { hear(bot) } label: { hearLabel(bot) }
                    .buttonStyle(.quiet)
                    .controlSize(.small)
                    .disabled(effectiveModel.isEmpty)
                if heard, let failure = model.readAloud.error {
                    Text(failure).font(.silo(12.5)).foregroundStyle(Theme.vermilion).lineLimit(2)
                }
            }
            .padding(.top, 2)
        }
    }

    private func speedRow(_ bot: Silo_V1_Bot) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Speed").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            ChipRadio(options: Self.speeds.map { (value: $0, label: $0 == 0 ? "Normal" : "\($0)×") },
                      selection: Binding(get: { Self.speeds.contains(bot.speechSpeed) ? bot.speechSpeed : 0 }, set: { speed in
                          Task { await save(speed: speed) }
                      }))
                .disabled(busy)
        }
    }

    private func hint(_ text: String) -> some View {
        Text(text).font(.silo(12.5)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
    }

    private func hearLabel(_ bot: Silo_V1_Bot) -> some View {
        let reader = model.readAloud
        let mine = reader.text == line(bot)
        return HStack(spacing: 6) {
            if mine && reader.state == .loading {
                Spinner(size: 13)
            } else if mine && reader.state == .playing {
                Image(systemName: "stop.fill").font(.system(size: 10))
            } else {
                Image(systemName: "speaker.wave.2").font(.system(size: 13))
            }
            Text(mine && reader.state == .playing ? "Stop" : "Hear it")
        }
    }

    private var effectiveModel: String { chosen.isEmpty ? (list?.defaultModel ?? "") : chosen }

    private var modelOptions: [(value: String, label: String)] {
        let fallback = (list?.defaultModel ?? "").split(separator: "/").dropFirst().joined(separator: "/")
        let auto = fallback.isEmpty ? "Operator default" : "Operator default (\(fallback))"
        return [(value: "", label: auto)] + (list?.models ?? []).map {
            (value: $0.id, label: ($0.label.isEmpty ? $0.id : $0.label) + ($0.live ? " · live" : ""))
        }
    }

    /// The Bot's own voice while this model has it, else Automatic.
    private func current(_ bot: Silo_V1_Bot) -> String {
        let voices = list?.voices ?? []
        return voices.isEmpty || voices.contains { $0.id == bot.voice } ? bot.voice : ""
    }

    /// Automatic, then the Bot's own language first.
    private func voiceOptions(_ bot: Silo_V1_Bot) -> [(value: String, label: String)] {
        let voices = list?.voices ?? []
        let fallback = list?.defaultVoice ?? ""
        let name = voices.first { $0.id == fallback }?.label ?? fallback
        let language = bot.language == "mixed" ? "" : bot.language
        let sorted = voices.filter { $0.language == language } + voices.filter { $0.language != language }
        return [(value: "", label: name.isEmpty ? "Automatic" : "Automatic (\(name))")] + sorted.map {
            (value: $0.id, label: $0.language.isEmpty ? $0.label : "\($0.label) · \($0.language)")
        }
    }

    private func line(_ bot: Silo_V1_Bot) -> String { "Hi, I'm \(bot.name). This is how I sound when I talk with you." }

    private func hear(_ bot: Silo_V1_Bot) {
        let reader = model.readAloud
        if reader.state != .idle, reader.text == line(bot) {
            reader.stop()
            return
        }
        heard = true
        let voice = current(bot).isEmpty ? (list?.defaultVoice ?? "") : current(bot)
        reader.speak(line(bot), client: model.client, botID: bot.id, model: effectiveModel, voice: voice,
                     speed: bot.speechSpeed == 0 ? 1 : bot.speechSpeed)
    }

    private func load(_ botID: String) async {
        do {
            list = try await model.client.listVoices(botID: botID, model: chosen)
        } catch {
            // The picker still works: a voice can be typed.
            list = list ?? Silo_V1_ListVoicesResponse()
            await model.report(error)
        }
    }

    private func save(speechModel: String? = nil, voice: String? = nil, speed: Double? = nil) async {
        guard let bot = model.selectedBot else { return }
        busy = true
        defer { busy = false }
        do {
            model.applyBot(try await model.client.setBotVoice(
                botID: bot.id, speechModel: speechModel ?? bot.speechModel, voice: voice ?? bot.voice, speed: speed ?? bot.speechSpeed))
        } catch { await model.report(error) }
    }
}
