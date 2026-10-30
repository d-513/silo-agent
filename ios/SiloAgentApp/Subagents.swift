import Observation
import SiloClient
import SwiftUI

/// The chat's shared checklist, a strip at the top of the thread: folded it is a `well` bar with
/// who owns what and a progress hairline, open it lifts to `surface` and lists every task.
struct TaskboardStrip: View {
    @Environment(AppModel.self) private var model
    @State private var open = false
    var mine: String?
    var items: [Silo_V1_TaskItem]? = nil

    var body: some View {
        let list = items ?? model.board
        if !list.isEmpty {
            let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
            VStack(spacing: 0) {
                head(list)
                if open {
                    CappedScroll(maxHeight: 300) { tasks(list) }
                        .transition(.opacity)
                }
            }
            .background(open ? Theme.surface : Theme.well, in: shape)
            .clipShape(shape)
            .overlay { if open { shape.stroke(Theme.line) } }
            .padding(.horizontal, 12)
            .padding(.top, 8)
        }
    }

    private func head(_ list: [Silo_V1_TaskItem]) -> some View {
        let done = list.filter(\.done).count
        var owners: [String] = []
        for task in list where !task.assignee.isEmpty && !owners.contains(task.assignee) { owners.append(task.assignee) }
        return Button {
            withAnimation(Motion.fold) { open.toggle() }
        } label: {
            HStack(spacing: 8) {
                FoldChevron(open: open)
                Image(systemName: "checklist").font(.system(size: 13)).foregroundStyle(Theme.ink2)
                Text("Tasks").font(.silo(13, .medium)).foregroundStyle(Theme.ink)
                Text("\(done)/\(list.count)").font(.siloMono(12)).foregroundStyle(Theme.ink3).monospacedDigit()
                HStack(spacing: 4) {
                    ForEach(owners, id: \.self) { AgentChip(name: $0) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .clipped()
            }
            .padding(.horizontal, 12)
            .frame(height: 36)
            .contentShape(Rectangle())
            .overlay(alignment: .bottom) {
                GeometryReader { box in
                    ZStack(alignment: .leading) {
                        Theme.line
                        Theme.emerald
                            .frame(width: box.size.width * CGFloat(done) / CGFloat(max(1, list.count)))
                            .animation(Motion.quiet(0.3), value: done)
                    }
                }
                .frame(height: 2)
            }
        }
        .buttonStyle(.press)
        .accessibilityValue("\(done) of \(list.count) done")
    }

    private func tasks(_ list: [Silo_V1_TaskItem]) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(list, id: \.n) { item in
                let dim = mine.map { item.assignee.lowercased() != $0.lowercased() } ?? false
                HStack(alignment: .top, spacing: 8) {
                    TaskCheck(done: item.done, size: 16).padding(.top, 2)
                    Text("#\(item.n)").font(.siloMono(12)).foregroundStyle(Theme.ink3).frame(width: 28, alignment: .leading).padding(.top, 2)
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            if !item.assignee.isEmpty { AgentChip(name: item.assignee) }
                            Text(item.text)
                                .font(.silo(13))
                                .foregroundStyle(item.done ? Theme.ink2 : Theme.ink)
                                .strikethrough(item.done, color: Theme.ink3)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        if item.done, !item.doneBy.isEmpty || !item.note.isEmpty {
                            Text(doneLine(item)).font(.silo(12)).foregroundStyle(Theme.ink3)
                        } else if !item.done, !item.note.isEmpty {
                            Text(item.note).font(.silo(12)).foregroundStyle(Theme.ink3)
                        }
                    }
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .opacity(dim ? 0.55 : 1)
            }
            if items == nil {
                HStack {
                    Spacer(minLength: 0)
                    ArmedButton(title: "Clear board", armedTitle: "Tap again to clear", symbol: "trash", small: true) {
                        Task { await model.clearBoard() }
                    }
                }
                .padding(.horizontal, 6)
            }
        }
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func doneLine(_ item: Silo_V1_TaskItem) -> String {
        [item.doneBy.isEmpty ? "done" : "done by \(item.doneBy)", item.note].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// A name on the Taskboard: subagents get no colors of their own.
private struct AgentChip: View {
    let name: String

    var body: some View {
        Text(name)
            .font(.siloMono(11.5))
            .foregroundStyle(Theme.ink2)
            .lineLimit(1)
            .padding(.horizontal, 6)
            .frame(height: 20)
            .background(Theme.pressed, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
            .frame(maxWidth: 144, alignment: .leading)
    }
}

/// Running subagent cards between the thread and the composer, plus a menu of the finished ones.
struct SubagentTray: View {
    @Environment(AppModel.self) private var model
    @State private var opened: Silo_V1_Subagent?

    var body: some View {
        let running = model.subagents.filter(\.running)
        let finished = model.subagents.filter { !$0.running }
        if !model.subagents.isEmpty {
            HStack(spacing: 6) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(running, id: \.id) { agent in
                            card(agent).rise()
                        }
                    }
                    .padding(1)
                }
                if running.count > 1 {
                    Button {
                        Task { await model.stopAllSubagents() }
                    } label: {
                        Image(systemName: "stop").font(.system(size: 12, weight: .medium))
                    }
                    .buttonStyle(.glyph(tint: Theme.ink2, side: 36))
                    .accessibilityLabel("Stop all subagents")
                }
                if !finished.isEmpty {
                    SiloMenu {
                        ForEach(finished, id: \.id) { agent in
                            MenuRow(title: agent.name, detail: finishedLook(agent.status).word,
                                    symbol: agent.status == "done" ? "checkmark" : "exclamationmark.triangle") {
                                opened = agent
                            }
                        }
                    } label: {
                        HStack(spacing: 5) {
                            Text("\(finished.count) finished").font(.silo(12.5))
                            Image(systemName: "chevron.up").font(.system(size: 9, weight: .semibold))
                        }
                        .foregroundStyle(Theme.ink2)
                        .padding(.horizontal, 8)
                        .frame(height: 36)
                    }
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .sheet(item: $opened) { agent in
                SubagentPage(agent: agent)
            }
        }
    }

    /// The run on the Bot's activity that speaks for this subagent; an approval it is paused on
    /// counts even before the activity says so.
    private func look(_ agent: Silo_V1_Subagent) -> (look: StatusLook, step: String) {
        let row = model.selectedBot?.activity.first {
            $0.kind == "subagent" && $0.leadChatID == model.selectedChatID && $0.title == agent.name
        }
        if model.waitingRunIDs.contains(agent.runID) { return (runLook(Doing(state: "needs_you")), "") }
        return (runLook(row.map(Doing.init) ?? .started), progress(row))
    }

    private func card(_ agent: Silo_V1_Subagent) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        let now = look(agent)
        return ZStack(alignment: .topTrailing) {
            Button {
                opened = agent
            } label: {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(agent.name).font(.silo(13.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                        Spacer(minLength: 0)
                        TimelineView(.periodic(from: .now, by: 1)) { clock in
                            Text(When.elapsed(agent.createdAt, now: clock.date))
                                .font(.siloMono(11.5))
                                .foregroundStyle(Theme.ink3)
                                .monospacedDigit()
                        }
                        .padding(.trailing, 22)
                    }
                    DoingLine(look: now.look, step: now.step)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .frame(width: 232, alignment: .leading)
                .background(Theme.surface, in: shape)
                .overlay(shape.stroke(Theme.line))
            }
            .buttonStyle(.press)
            Button {
                Task { await model.stopSubagent(agent.id) }
            } label: {
                Image(systemName: "xmark").font(.system(size: 11, weight: .medium))
            }
            .buttonStyle(.glyph(tint: Theme.ink2, side: 30))
            .accessibilityLabel("Stop \(agent.name)")
        }
    }
}

extension Silo_V1_Subagent: @retroactive Identifiable {}

func shortModel(_ id: String) -> String {
    id.split(separator: "/").last.map(String.init) ?? id
}

/// A subagent's read-only log with its brief and Stop: only the lead talks to it.
struct SubagentPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let agent: Silo_V1_Subagent
    @State private var log: LogModel?
    @State private var showBrief = false

    /// The freshest copy of this agent (the tray list refreshes while it runs).
    private var current: Silo_V1_Subagent {
        model.subagents.first { $0.id == agent.id } ?? agent
    }

    private var busy: Bool { current.running || log?.busy == true }

    private var look: StatusLook {
        guard busy else { return finishedLook(current.status) }
        if model.waitingRunIDs.contains(current.runID) { return runLook(Doing(state: "needs_you")) }
        let row = model.selectedBot?.activity.first {
            $0.kind == "subagent" && $0.leadChatID == current.parentChatID && $0.title == current.name
        }
        return runLook(row.map(Doing.init) ?? .started)
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: agent.name, cancel: "Done", onCancel: { dismiss() }) {
                if busy {
                    Button {
                        Task { await model.stopSubagent(agent.id) }
                    } label: {
                        HStack(spacing: 5) {
                            Image(systemName: "stop.fill").font(.system(size: 9))
                            Text("Stop")
                        }
                    }
                    .buttonStyle(.quiet)
                }
            }
            strip
            if let log {
                ThreadView(events: log.events, busy: log.busy, interactive: false, prepended: log.prepended,
                           earlier: log.earlier, loadingEarlier: log.loadingEarlier, onEarlier: { await log.loadEarlier() })
            } else {
                SkeletonRows(count: 3).padding(16)
                Spacer(minLength: 0)
            }
        }
        .background(Theme.canvas)
        .siloSheet()
        .onAppear {
            guard log == nil, let botID = model.selectedBotID else { return }
            let model = LogModel(client: model.client, botID: botID, chatID: agent.chatID)
            log = model
            model.start()
        }
        .onDisappear { log?.stop() }
    }

    /// Its status line, its model, and the brief folded under them.
    private var strip: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(Motion.fold) { showBrief.toggle() }
            } label: {
                HStack(spacing: 8) {
                    StatusWord(look: look)
                    Spacer(minLength: 8)
                    Text(shortModel(current.model)).font(.siloMono(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                    Text("Brief").font(.silo(13, .medium)).foregroundStyle(Theme.ink2)
                    FoldChevron(open: showBrief)
                }
                .padding(.horizontal, 14)
                .frame(height: 40)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            if showBrief {
                CappedScroll(maxHeight: 320) { brief }
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
                    .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
                    .padding(.horizontal, 12)
                    .padding(.bottom, 10)
                    .transition(.opacity)
            }
        }
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    private var brief: some View {
        VStack(alignment: .leading, spacing: 12) {
            briefPart("Goal", current.goal)
            if !current.context.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                briefPart("Context", current.context)
            }
            VStack(alignment: .leading, spacing: 4) {
                SectionLabel(text: "Model")
                Text(current.model).font(.siloMono(12.5)).foregroundStyle(Theme.ink2).textSelection(.enabled)
            }
            if !current.result.isEmpty {
                briefPart("Result", current.result)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func briefPart(_ title: String, _ text: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            SectionLabel(text: title)
            ProseView(text: text)
        }
    }
}
