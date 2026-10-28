import SiloClient
import SwiftUI

/// Name, description, SOUL, and the Bot's default model. A chat's own override still wins.
struct BotSettingsPage: View {
    @Environment(AppModel.self) private var model
    @State private var name = ""
    @State private var description = ""
    @State private var soul = ""
    @State private var botModel = ""
    /// The Bot as `GetBot` has it: the list leaves SOUL out, so the form starts from this.
    @State private var full: Silo_V1_Bot?
    @State private var saving = false
    @State private var confirmDelete = false

    var body: some View {
        Form {
            Section("Name") { TextField("Name", text: $name) }
            Section("Description") { TextField("What is this Bot for?", text: $description, axis: .vertical) }
            Section {
                TextEditor(text: $soul).frame(minHeight: 140)
            } header: {
                Text("SOUL")
            } footer: {
                Text("Who the Bot is and how it behaves. Injected into every run.")
            }
            Section {
                Picker("Default model", selection: $botModel) {
                    Text("Operator default").tag("")
                    ForEach(model.models, id: \.id) { Text($0.label.isEmpty ? $0.id : $0.label).tag($0.id) }
                }
            } footer: {
                Text("A chat's own model choice overrides this.")
            }
            ProactivitySection()
            TimeZoneSection()
            if model.selectedBot?.mailAddress.isEmpty == false { MailSettingsSection() }
            Section {
                Button("Delete Bot", role: .destructive) { confirmDelete = true }
            } footer: {
                Text("Removes the machine, the Bot, and its workspace.")
            }
        }
        .toolbar {
            if dirty {
                ToolbarItem(placement: .confirmationAction) {
                    Button(saving ? "Saving…" : "Save") { Task { await save() } }
                        .disabled(saving || name.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
        .task(id: model.selectedBotID) { await load() }
        .confirmationDialog("Delete this Bot?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete Bot and its data", role: .destructive) { Task { await delete() } }
        } message: {
            Text("This cannot be undone.")
        }
    }

    private var dirty: Bool {
        guard let bot = full else { return false }
        return name != bot.name || description != bot.description_p || soul != bot.soul || botModel != bot.model
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
    }

    private func save() async {
        guard let bot = full else { return }
        saving = true
        defer { saving = false }
        do {
            let saved = try await model.client.updateBot(bot, name: name, description: description, soul: soul, model: botModel)
            full = saved
            model.applyBot(saved)
        } catch { await model.report(error) }
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
            Section {
                Picker("Time zone", selection: Binding(get: { bot.timeZonePinned }, set: { pinned in
                    Task { await set(zone: pinned ? (bot.timeZone.isEmpty ? TimeZone.current.identifier : bot.timeZone) : TimeZone.current.identifier, pinned: pinned) }
                })) {
                    Text("Follow my device").tag(false)
                    Text("Fixed").tag(true)
                }
                .pickerStyle(.segmented)
                if bot.timeZonePinned {
                    NavigationLink {
                        ZonePicker(selected: bot.timeZone) { zone in Task { await set(zone: zone, pinned: true) } }
                    } label: {
                        LabeledContent("Zone", value: zoneLabel(bot.timeZone))
                    }
                } else {
                    LabeledContent("Now", value: zoneLabel(bot.timeZone.isEmpty ? TimeZone.current.identifier : bot.timeZone))
                }
            } header: {
                Text("Time zone")
            } footer: {
                Text(bot.timeZonePinned
                     ? "The Bot stays in this zone wherever you are. Automations run on its clock."
                     : "The Bot moves with you: each message carries your device's zone. Automations run on its clock.")
            }
        }
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
        List(zones, id: \.self) { zone in
            Button {
                pick(zone)
                dismiss()
            } label: {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(zone.split(separator: "/").last.map { $0.replacingOccurrences(of: "_", with: " ") } ?? zone).foregroundStyle(.primary)
                        Text(zone).font(.silo(.caption)).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Text(TimeZone(identifier: zone)?.abbreviation() ?? "").font(.siloMono(.caption)).foregroundStyle(.secondary)
                    if zone == selected { Image(systemName: "checkmark").foregroundStyle(Theme.cobalt) }
                }
            }
        }
        .searchable(text: $query, prompt: "City or region")
        .navigationTitle("Time Zone")
        .navigationBarTitleDisplayMode(.inline)
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
        Section {
            Picker("Proactivity", selection: Binding(get: { current.id }, set: { level in Task { await choose(level) } })) {
                ForEach(Self.levels, id: \.id) { Text($0.label).tag($0.id) }
            }
            .pickerStyle(.segmented)
            .disabled(busy)
        } header: {
            Text("Proactivity")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                Text(current.note)
                if bot?.heartbeatCustom == true {
                    Text("The Heartbeat runs on a schedule set by hand (Automations). Choosing a level puts it back on that level's schedule.")
                }
            }
        }
    }

    private func choose(_ level: String) async {
        guard let bot = model.selectedBot, !busy, level != bot.proactivity || bot.heartbeatCustom else { return }
        busy = true
        defer { busy = false }
        do { model.applyBot(try await model.client.setProactivity(bot, level: level)) } catch { await model.report(error) }
    }
}
