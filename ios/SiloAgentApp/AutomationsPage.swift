import SiloClient
import SwiftUI

extension Silo_V1_Automation: @retroactive Identifiable {}

/// Scheduled background prompts. Each has a hidden log chat; every firing starts from fresh context.
struct AutomationsPage: View {
    @Environment(AppModel.self) private var model
    @State private var items: [Silo_V1_Automation] = []
    @State private var loaded = false
    @State private var editing: Silo_V1_Automation?
    @State private var creating = false
    @State private var deleting: Silo_V1_Automation?

    var body: some View {
        NavigationStack {
            List {
                ForEach(items, id: \.id) { item in
                    NavigationLink {
                        AutomationLog(automation: item)
                    } label: {
                        row(item)
                    }
                    .swipeActions(edge: .trailing) {
                        if item.kind != "heartbeat" {
                            Button(role: .destructive) { deleting = item } label: { Label("Delete", systemImage: "trash") }
                        }
                        Button { editing = item } label: { Label("Edit", systemImage: "pencil") }.tint(Theme.cobalt)
                    }
                    .swipeActions(edge: .leading) {
                        Button { Task { await run(item) } } label: { Label("Run now", systemImage: "play.fill") }.tint(Theme.emerald)
                    }
                }
            }
            .overlay {
                if loaded && items.isEmpty {
                    ContentUnavailableView("No automations", systemImage: "clock.arrow.circlepath", description: Text("Scheduled prompts the Bot runs on its own."))
                }
            }
            .navigationTitle("Automations")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button { creating = true } label: { Label("New Automation", systemImage: "plus") }
                }
            }
            .refreshable { await load() }
        }
        .task(id: model.selectedBotID) { await load() }
        .sheet(item: $editing) { item in AutomationEditor(existing: item) { await load() } }
        .sheet(isPresented: $creating) { AutomationEditor(existing: nil) { await load() } }
        .confirmationDialog("Delete this automation?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { item in
            Button("Delete automation", role: .destructive) { Task { await delete(item) } }
        } message: { _ in Text("Its run log is deleted with it.") }
    }

    private func row(_ item: Silo_V1_Automation) -> some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(item.name).font(.headline).lineLimit(1)
                    if item.kind == "heartbeat" {
                        Image(systemName: "pin.fill").font(.caption2).foregroundStyle(.secondary)
                    }
                    if item.running { ProgressView().controlSize(.mini) }
                }
                Text(describeSchedule(item.schedule)).font(.subheadline).foregroundStyle(.secondary)
                Text(status(item)).font(.caption).foregroundStyle(item.lastStatus == "error" ? Theme.vermilion : .secondary)
            }
            Spacer(minLength: 8)
            Toggle("Enabled", isOn: Binding(get: { item.enabled }, set: { value in Task { await setEnabled(item, value) } }))
                .labelsHidden()
        }
    }

    private func status(_ item: Silo_V1_Automation) -> String {
        var parts: [String] = []
        if item.enabled, !item.nextRunAt.isEmpty { parts.append("Next \(relativeTime(item.nextRunAt))") }
        if !item.lastRunAt.isEmpty { parts.append("Last \(relativeTime(item.lastRunAt))\(item.lastStatus.isEmpty ? "" : " · \(item.lastStatus)")") }
        return parts.isEmpty ? "Never run" : parts.joined(separator: " · ")
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { items = try await model.client.listAutomations(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func setEnabled(_ item: Silo_V1_Automation, _ value: Bool) async {
        guard let botID = model.selectedBotID else { return }
        do {
            _ = try await model.client.updateAutomation(botID: botID, id: item.id, name: item.name, prompt: item.prompt, schedule: item.schedule, enabled: value)
        } catch { await model.report(error) }
        await load()
    }

    private func run(_ item: Silo_V1_Automation) async {
        guard let botID = model.selectedBotID else { return }
        do { try await model.client.runAutomation(botID: botID, id: item.id) } catch { await model.report(error) }
        await load()
    }

    private func delete(_ item: Silo_V1_Automation) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteAutomation(botID: botID, id: item.id)
            items.removeAll { $0.id == item.id }
        } catch { await model.report(error) }
    }
}

/// An automation's run log: the shared thread, one "Ran …" run after another.
struct AutomationLog: View {
    @Environment(AppModel.self) private var model
    let automation: Silo_V1_Automation
    @State private var log: LogModel?

    var body: some View {
        VStack(spacing: 0) {
            if let log {
                ThreadView(events: log.events, busy: log.busy, interactive: false)
            } else {
                ProgressView().frame(maxHeight: .infinity)
            }
        }
        .navigationTitle(automation.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    Task {
                        guard let botID = model.selectedBotID else { return }
                        do { try await model.client.runAutomation(botID: botID, id: automation.id) } catch { await model.report(error) }
                    }
                } label: { Label("Run now", systemImage: "play.fill") }
            }
        }
        .onAppear {
            guard log == nil, let botID = model.selectedBotID, !automation.chatID.isEmpty else { return }
            let next = LogModel(client: model.client, botID: botID, chatID: automation.chatID)
            log = next
            next.start()
        }
        .onDisappear { log?.stop() }
    }
}

struct AutomationEditor: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let existing: Silo_V1_Automation?
    let onSaved: () async -> Void

    @State private var name = ""
    @State private var prompt = ""
    @State private var enabled = true
    @State private var schedule = Schedule()
    @State private var saving = false
    @State private var failure: String?

    var body: some View {
        NavigationStack {
            Form {
                if existing?.kind != "heartbeat" {
                    Section("Name") { TextField("Name", text: $name) }
                }
                Section {
                    TextEditor(text: $prompt).frame(minHeight: 120)
                } header: {
                    Text("Prompt")
                } footer: {
                    Text("Every run starts fresh, so make the prompt self-contained. The Bot keeps state in files or memory.")
                }
                Section("Schedule") { ScheduleEditor(schedule: $schedule) }
                Section { Toggle("Enabled", isOn: $enabled) }
                if let failure {
                    Section { Text(failure).foregroundStyle(Theme.vermilion) }
                }
            }
            .navigationTitle(existing == nil ? "New automation" : "Edit automation")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(saving ? "Saving…" : "Save") { Task { await save() } }
                        .disabled(saving || prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                                  || (existing?.kind != "heartbeat" && name.trimmingCharacters(in: .whitespaces).isEmpty))
                }
            }
        }
        .presentationDetents([.large])
        .onAppear {
            guard let existing else { return }
            name = existing.name
            prompt = existing.prompt
            enabled = existing.enabled
            schedule = Schedule(cron: existing.schedule)
        }
    }

    private func save() async {
        guard let botID = model.selectedBotID else { return }
        saving = true
        defer { saving = false }
        do {
            if let existing {
                _ = try await model.client.updateAutomation(botID: botID, id: existing.id, name: name.isEmpty ? existing.name : name, prompt: prompt, schedule: schedule.toCron, enabled: enabled)
            } else {
                _ = try await model.client.createAutomation(botID: botID, name: name, prompt: prompt, schedule: schedule.toCron, enabled: enabled)
            }
            await onSaved()
            dismiss()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

/// The friendly schedule picker over cron (`Schedule`); anything unfamiliar stays "custom".
struct ScheduleEditor: View {
    @Binding var schedule: Schedule

    private static let modes: [(Schedule.Mode, String)] = [
        (.none, "Off (run manually)"), (.minutes, "Every few minutes"), (.hours, "Hourly"),
        (.daily, "Daily"), (.weekly, "Weekly"), (.monthly, "Monthly"), (.custom, "Custom cron"),
    ]

    var body: some View {
        Picker("Repeat", selection: $schedule.mode) {
            ForEach(Self.modes, id: \.0) { Text($0.1).tag($0.0) }
        }
        switch schedule.mode {
        case .none:
            EmptyView()
        case .minutes:
            Picker("Every", selection: $schedule.every) {
                ForEach(Schedule.minuteSteps, id: \.self) { Text("\($0) minutes").tag($0) }
            }
        case .hours:
            Picker("Every", selection: $schedule.every) {
                ForEach(Schedule.hourSteps, id: \.self) { Text($0 == 1 ? "hour" : "\($0) hours").tag($0) }
            }
            Stepper("At minute :\(String(format: "%02d", schedule.minute))", value: $schedule.minute, in: 0...59)
        case .daily:
            timeRow
        case .weekly:
            HStack {
                ForEach(0..<7, id: \.self) { day in
                    let on = schedule.days.contains(day)
                    Button {
                        if on { schedule.days.removeAll { $0 == day } } else { schedule.days.append(day) }
                    } label: {
                        Text(String(Schedule.dayShort[day].prefix(1)))
                            .font(.subheadline.weight(.semibold))
                            .frame(width: 34, height: 34)
                            .background(on ? Theme.cobalt : Theme.well, in: Circle())
                            .foregroundStyle(on ? Color.white : Color.primary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(Schedule.dayShort[day])
                }
            }
            timeRow
        case .monthly:
            Stepper("Day of month: \(schedule.dom)", value: $schedule.dom, in: 1...31)
            timeRow
        case .custom:
            TextField("* * * * *", text: $schedule.cron)
                .font(.system(.body, design: .monospaced))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
        Text(schedule.summary).font(.footnote).foregroundStyle(.secondary)
    }

    private var timeRow: some View {
        DatePicker("At", selection: Binding(
            get: { Calendar.current.date(bySettingHour: schedule.hour, minute: schedule.minute, second: 0, of: Date()) ?? Date() },
            set: {
                let parts = Calendar.current.dateComponents([.hour, .minute], from: $0)
                schedule.hour = parts.hour ?? 9
                schedule.minute = parts.minute ?? 0
            }
        ), displayedComponents: .hourAndMinute)
    }
}
