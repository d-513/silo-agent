import SiloClient
import SwiftUI

extension Silo_V1_Automation: @retroactive Identifiable {}

/// Scheduled background prompts: the Bot's pinned ones on a `well` band, then the Scheduled panel.
/// Each has a hidden log chat; every firing starts from fresh context.
struct AutomationsPage: View {
    @Environment(AppModel.self) private var model
    @State private var items: [Silo_V1_Automation] = []
    @State private var loaded = false
    @State private var arrived: Set<String> = []
    @State private var editing: Silo_V1_Automation?
    @State private var creating = false
    @State private var deleting: Silo_V1_Automation?
    @State private var ran = 0

    var body: some View {
        PageScroll {
            if !loaded {
                SkeletonRows(count: 3, height: 64)
            } else if items.isEmpty {
                EmptyWell(symbol: "clock.arrow.circlepath", title: "No automations", message: "Scheduled prompts the Bot runs on its own.")
            } else {
                let pinned = items.filter(\.pinned)
                let custom = items.filter { !$0.pinned }
                if !pinned.isEmpty {
                    Panel {
                        ForEach(pinned, id: \.id) { row($0) }
                        Text("Every Bot has one, and it cannot be deleted. It does not run until you give it a schedule.")
                            .font(.silo(12.5))
                            .foregroundStyle(Theme.ink3)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 10)
                    }
                }
                PanelSection(title: "Scheduled") {
                    if custom.isEmpty {
                        Text("None yet. Ask \(model.selectedBot?.name ?? "the Bot") in a chat — “every weekday at 9, summarize my inbox” — or add one here.")
                            .font(.silo(13))
                            .foregroundStyle(Theme.ink3)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(14)
                    } else {
                        ForEach(custom, id: \.id) { row($0).rise(when: arrived.contains($0.id)) }
                    }
                }
            }
        }
        .pageActions {
            Button { creating = true } label: { Image(systemName: "plus") }
                .buttonStyle(.glyph)
                .accessibilityLabel("New automation")
        }
        .sensoryFeedback(.impact(weight: .light), trigger: ran)
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(item: $editing) { item in AutomationEditor(existing: item) { await load() } }
        .sheet(isPresented: $creating) { AutomationEditor(existing: nil) { await load() } }
        .confirmSlip(item: $deleting, title: { _ in "Delete this automation?" }, message: "Its run log is deleted with it.",
                     action: { _ in "Delete automation" }) { item in
            Task { await delete(item) }
        }
    }

    private func row(_ item: Silo_V1_Automation) -> some View {
        HStack(spacing: 0) {
            NavigationLink {
                AutomationLog(automation: item)
            } label: {
                HStack(spacing: 12) {
                    AutomationMark(automation: item).markWell(lifted: item.pinned)
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            Text(item.name).font(.silo(13.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                            if item.running { Lamp(status: look(item).lamp) }
                            if item.lastStatus == "error" {
                                Text("Last run failed").font(.silo(12)).foregroundStyle(Theme.vermilion).lineLimit(1)
                            }
                        }
                        Text(automationLine(item, look: look(item))).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(2)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.leading, 14)
                .padding(.trailing, 8)
                .padding(.vertical, 12)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            Toggle("", isOn: Binding(get: { item.enabled }, set: { value in Task { await setEnabled(item, value) } }))
                .labelsHidden()
                .fixedSize()
                .accessibilityLabel(item.enabled ? "Pause \(item.name)" : "Resume \(item.name)")
            menu(item)
        }
        .padding(.trailing, 2)
        .background(item.pinned ? Theme.well : .clear)
    }

    private func menu(_ item: Silo_V1_Automation) -> some View {
        SiloMenu {
            MenuRow(title: "Run now", symbol: "play") { Task { await run(item) } }
            MenuRow(title: "Edit", symbol: "pencil") { editing = item }
            if !item.pinned {
                MenuDivider()
                MenuRow(title: "Delete", symbol: "trash", destructive: true) { deleting = item }
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(Theme.ink2)
                .frame(width: 36, height: 44)
        }
        .accessibilityLabel("More for \(item.name)")
    }

    private func look(_ item: Silo_V1_Automation) -> StatusLook {
        runLook(model.selectedBot.flatMap { chatDoing($0, item.chatID) }.map(Doing.init) ?? .started)
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listAutomations(botID: botID)
            arrived = loaded ? Set(list.map(\.id)).subtracting(items.map(\.id)) : []
            withAnimation(Motion.state) { items = list }
        } catch { await model.report(error) }
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
        do {
            try await model.client.runAutomation(botID: botID, id: item.id)
            ran += 1
        } catch { await model.report(error) }
        await load()
    }

    private func delete(_ item: Silo_V1_Automation) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteAutomation(botID: botID, id: item.id)
            withAnimation(Motion.state) { items.removeAll { $0.id == item.id } }
        } catch { await model.report(error) }
    }
}

/// `describe(schedule) · Next …`, or what stands in for it: what it is on while it runs, Paused, no
/// schedule at all.
private func automationLine(_ item: Silo_V1_Automation, look: StatusLook) -> String {
    let next: String
    if item.running {
        next = look.word
    } else if !item.enabled {
        next = "Paused"
    } else if item.schedule.isEmpty {
        next = "No schedule — never runs on its own"
    } else {
        next = item.nextRunAt.isEmpty ? "" : "Next \(When.next(item.nextRunAt))"
    }
    return [describeSchedule(item.schedule), next].filter { !$0.isEmpty }.joined(separator: " · ")
}

/// An automation's run log: the shared thread, one "Ran …" run after another.
struct AutomationLog: View {
    @Environment(AppModel.self) private var model
    let automation: Silo_V1_Automation
    @State private var log: LogModel?
    @State private var ran = 0

    private var busy: Bool { log?.busy == true || automation.running }
    private var look: StatusLook {
        runLook(model.selectedBot.flatMap { chatDoing($0, automation.chatID) }.map(Doing.init) ?? .started)
    }

    var body: some View {
        SiloPage(title: automation.name) {
            VStack(spacing: 0) {
                strip
                if let log {
                    ThreadView(events: log.events, busy: log.busy, interactive: false, prepended: log.prepended,
                               earlier: log.earlier, loadingEarlier: log.loadingEarlier, onEarlier: { await log.loadEarlier() })
                } else if automation.chatID.isEmpty {
                    EmptyWell(title: "No runs yet.", message: "Run it now, or wait for its schedule.")
                        .padding(16)
                    Spacer(minLength: 0)
                } else {
                    SkeletonRows(count: 3).padding(16)
                    Spacer(minLength: 0)
                }
            }
            .pageActions {
                Button {
                    Task {
                        guard let botID = model.selectedBotID else { return }
                        do {
                            try await model.client.runAutomation(botID: botID, id: automation.id)
                            ran += 1
                        } catch { await model.report(error) }
                    }
                } label: {
                    HStack(spacing: 5) {
                        Image(systemName: "play.fill").font(.system(size: 10))
                        Text("Run now")
                    }
                }
                .buttonStyle(.ink)
                .controlSize(.small)
                .padding(.trailing, 6)
            }
            .sensoryFeedback(.impact(weight: .light), trigger: ran)
        }
        .onAppear {
            guard log == nil, let botID = model.selectedBotID, !automation.chatID.isEmpty else { return }
            let next = LogModel(client: model.client, botID: botID, chatID: automation.chatID)
            log = next
            next.start()
        }
        .onDisappear { log?.stop() }
    }

    /// The same mark on a `surface` well beside what the automation is doing, or when it runs.
    private var strip: some View {
        HStack(spacing: 10) {
            AutomationMark(automation: automation).markWell(lifted: true)
            VStack(alignment: .leading, spacing: 1) {
                if busy {
                    StatusWord(look: look)
                } else {
                    Text(automationLine(automation, look: look)).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
                if !automation.lastRunAt.isEmpty {
                    Text(lastRun).font(.silo(12)).foregroundStyle(automation.lastStatus == "error" ? Theme.vermilion : Theme.ink3).lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    private var lastRun: String {
        let status = automation.lastStatus.isEmpty ? "" : " · \(automation.lastStatus)"
        return "Last \(relativeTime(automation.lastRunAt))\(status)"
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
    @State private var failure: String?
    @FocusState private var writing: Bool

    private var ready: Bool {
        !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && (existing?.pinned == true || !name.trimmingCharacters(in: .whitespaces).isEmpty)
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: existing == nil ? "New automation" : "Edit automation", onCancel: { dismiss() }) {
                SaveButton(enabled: ready) { await save() }
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    if existing?.pinned != true {
                        SiloField(label: "Name", prompt: "Morning digest", text: $name)
                    }
                    ScheduleEditor(schedule: $schedule)
                    promptField
                    Toggle(isOn: $enabled) {
                        Text(enabled ? "Active — runs on its schedule" : "Paused — keeps its schedule, does not run").font(.silo(13.5))
                    }
                    if let failure { ErrorLine(text: failure) }
                }
                .padding(16)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(Theme.surface)
        .siloSheet()
        .onAppear {
            guard let existing else { return }
            name = existing.name
            prompt = existing.prompt
            enabled = existing.enabled
            schedule = Schedule(cron: existing.schedule)
        }
    }

    private var promptField: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Prompt").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            TextEditor(text: $prompt)
                .font(.silo(16))
                .foregroundStyle(Theme.ink)
                .tint(Theme.cobalt)
                .scrollContentBackground(.hidden)
                .focused($writing)
                .padding(8)
                .frame(minHeight: 132)
                .siloInputFrame(focused: writing)
            Text("Every run starts fresh, so make the prompt self-contained. The Bot keeps state in files or memory.")
                .font(.silo(12))
                .foregroundStyle(Theme.ink3)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func save() async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        do {
            if let existing {
                _ = try await model.client.updateAutomation(botID: botID, id: existing.id, name: name.isEmpty ? existing.name : name, prompt: prompt, schedule: schedule.toCron, enabled: enabled)
            } else {
                _ = try await model.client.createAutomation(botID: botID, name: name, prompt: prompt, schedule: schedule.toCron, enabled: enabled)
            }
            await onSaved()
            dismiss()
            return true
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
            return false
        }
    }
}

/// The friendly schedule picker over cron (`Schedule`); anything unfamiliar stays "custom".
struct ScheduleEditor: View {
    @Environment(AppModel.self) private var model
    @Binding var schedule: Schedule

    private static let modes: [(value: Schedule.Mode, label: String)] = [
        (.none, "No schedule"), (.minutes, "Every few minutes"), (.hours, "Every few hours"),
        (.daily, "Every day"), (.weekly, "On chosen days"), (.monthly, "Every month"), (.custom, "Custom (cron)"),
    ]

    /// Monday first, as the web has it.
    private static let week = [1, 2, 3, 4, 5, 6, 0]

    /// Moving between shapes keeps the steps real: a step of 30 means nothing in hours.
    private var mode: Binding<Schedule.Mode> {
        Binding(get: { schedule.mode }, set: { new in
            let cron = schedule.toCron
            schedule.mode = new
            if new == .minutes { schedule.every = 30 }
            if new == .hours { schedule.every = 1 }
            if new == .custom, schedule.cron.isEmpty { schedule.cron = cron.isEmpty ? "0 9 * * *" : cron }
        })
    }

    private var hint: String {
        let zone = model.selectedBot?.timeZone ?? ""
        return "\(schedule.summary) · \(zone.isEmpty ? "the Bot's time zone" : "\(zone) time")"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            SiloPicker(label: "Schedule", options: Self.modes, selection: mode)
            switch schedule.mode {
            case .none:
                EmptyView()
            case .minutes:
                SiloPicker(options: Schedule.minuteSteps.map { (value: $0, label: "every \($0) min") }, selection: $schedule.every)
            case .hours:
                HStack(spacing: 8) {
                    SiloPicker(options: Schedule.hourSteps.map { (value: $0, label: $0 == 1 ? "every hour" : "every \($0) hours") }, selection: $schedule.every)
                    joiner("at minute")
                    NumberMenu(range: 0...59, value: $schedule.minute, padded: true)
                }
            case .daily:
                timeRow
            case .weekly:
                days
                timeRow
            case .monthly:
                HStack(spacing: 8) {
                    joiner("on day")
                    NumberMenu(range: 1...31, value: $schedule.dom)
                    Spacer(minLength: 0)
                }
                timeRow
            case .custom:
                SiloField(prompt: "0 9 * * 1-5", text: $schedule.cron, mono: true)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            Text(hint).font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
        }
        .animation(Motion.state, value: schedule.mode)
    }

    private func joiner(_ text: String) -> some View {
        Text(text).font(.silo(13)).foregroundStyle(Theme.ink2).fixedSize()
    }

    private var days: some View {
        HStack(spacing: 4) {
            ForEach(Self.week, id: \.self) { day in
                let on = schedule.days.contains(day)
                let shape = RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
                Button {
                    withAnimation(Motion.quiet(0.2)) {
                        if on { schedule.days.removeAll { $0 == day } } else { schedule.days.append(day) }
                    }
                } label: {
                    Text(Schedule.dayShort[day])
                        .font(.silo(12.5, .medium))
                        .foregroundStyle(on ? Theme.ink : Theme.ink2)
                        .frame(maxWidth: .infinity)
                        .frame(height: 34)
                        .background(on ? Theme.cobaltPale : Theme.well, in: shape)
                        .overlay { if on { shape.strokeBorder(Theme.cobalt, lineWidth: 1) } }
                }
                .buttonStyle(.press)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .sensoryFeedback(.selection, trigger: schedule.days)
    }

    private var timeRow: some View {
        HStack(spacing: 8) {
            joiner("at")
            NumberMenu(range: 0...23, value: $schedule.hour, padded: true)
            Text(":").font(.siloMono(15)).foregroundStyle(Theme.ink3)
            NumberMenu(range: 0...59, value: $schedule.minute, padded: true)
            Spacer(minLength: 0)
        }
    }
}

extension Silo_V1_Automation {
    /// One of the automations every Bot has exactly one of (the Heartbeat, the Dashboard's
    /// curator): its name is fixed and it cannot be deleted.
    var pinned: Bool { kind == "heartbeat" || kind == "dashboard" }
}

/// What an automation is at a glance, on lucide's 24pt grid and 2pt stroke: the Heartbeat a pulse
/// trace (a flat line while it has no schedule or is off), the Dashboard's curator the board itself,
/// a wide widget over two small ones, any other a timer.
struct AutomationMark: View {
    let automation: Silo_V1_Automation
    var size: CGFloat = 15

    var body: some View {
        Group {
            switch automation.kind {
            case "heartbeat":
                drawn(Pulse(alive: automation.enabled && !automation.schedule.isEmpty))
            case "dashboard":
                drawn(BoardMark())
            default:
                Image(systemName: "timer").font(.system(size: size - 1, weight: .medium))
            }
        }
        .foregroundStyle(Theme.ink2)
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }

    private func drawn(_ shape: some Shape) -> some View {
        shape.stroke(style: StrokeStyle(lineWidth: size / 12, lineCap: .round, lineJoin: .round))
    }

    private struct Pulse: Shape {
        let alive: Bool

        func path(in rect: CGRect) -> Path {
            var path = Path()
            let unit = rect.width / 24
            let points: [CGPoint] = alive
                ? [.init(x: 2, y: 12), .init(x: 6.5, y: 12), .init(x: 8.5, y: 7.5), .init(x: 12, y: 18.5), .init(x: 14.5, y: 10), .init(x: 16, y: 12), .init(x: 22, y: 12)]
                : [.init(x: 2, y: 12), .init(x: 22, y: 12)]
            path.addLines(points.map { CGPoint(x: $0.x * unit, y: $0.y * unit) })
            return path
        }
    }

    private struct BoardMark: Shape {
        func path(in rect: CGRect) -> Path {
            var path = Path()
            let unit = rect.width / 24
            for tile in [CGRect(x: 3, y: 3, width: 18, height: 7.5), CGRect(x: 3, y: 13.5, width: 7.5, height: 7.5), CGRect(x: 13.5, y: 13.5, width: 7.5, height: 7.5)] {
                let scaled = CGRect(x: tile.minX * unit, y: tile.minY * unit, width: tile.width * unit, height: tile.height * unit)
                path.addRoundedRect(in: scaled, cornerSize: CGSize(width: 2 * unit, height: 2 * unit), style: .continuous)
            }
            return path
        }
    }
}

private extension View {
    /// The mark's 32pt well: `well` in a row, lifted to `surface` on the `well` band and the log.
    func markWell(lifted: Bool) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
        return frame(width: 32, height: 32)
            .background(lifted ? Theme.surface : Theme.well, in: shape)
            .overlay { if lifted { shape.stroke(Theme.line) } }
    }
}
