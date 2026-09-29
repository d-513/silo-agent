import Observation
import SiloClient
import SwiftUI

/// The chat's shared checklist: folded it is a progress line, open it lists every task.
struct TaskboardStrip: View {
    @Environment(AppModel.self) private var model
    @State private var open = false
    @State private var clearing = false
    var mine: String?
    var items: [Silo_V1_TaskItem]? = nil

    var body: some View {
        let list = items ?? model.board
        if !list.isEmpty {
            let done = list.filter(\.done).count
            VStack(spacing: 0) {
                Button {
                    withAnimation(.snappy) { open.toggle() }
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: "chevron.right")
                            .font(.caption2.weight(.semibold))
                            .rotationEffect(.degrees(open ? 90 : 0))
                            .foregroundStyle(.secondary)
                        Image(systemName: "checklist").foregroundStyle(.secondary)
                        Text("Tasks").font(.subheadline.weight(.medium))
                        Text("\(done)/\(list.count)")
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                        Spacer()
                        ProgressView(value: Double(done), total: Double(list.count))
                            .tint(Theme.emerald)
                            .frame(width: 60)
                    }
                    .padding(.horizontal, 12)
                    .frame(height: 38)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                if open {
                    Divider()
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(list, id: \.n) { item in
                            let dim = mine.map { !item.assignee.isEmpty && item.assignee.lowercased() != $0.lowercased() } ?? false
                            HStack(alignment: .top, spacing: 8) {
                                Image(systemName: item.done ? "checkmark.square.fill" : "square")
                                    .foregroundStyle(item.done ? Theme.emerald : .secondary)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(item.text)
                                        .font(.callout)
                                        .strikethrough(item.done)
                                    if !item.assignee.isEmpty || !item.note.isEmpty {
                                        Text([item.assignee, item.note].filter { !$0.isEmpty }.joined(separator: " · "))
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                            }
                            .opacity(dim ? 0.55 : 1)
                        }
                        if items == nil {
                            Button("Clear tasks", role: .destructive) { clearing = true }
                                .font(.footnote)
                                .padding(.top, 4)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12)
                }
            }
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card))
            .padding(.horizontal, 12)
            .padding(.top, 8)
            .confirmationDialog("Clear the task list?", isPresented: $clearing, titleVisibility: .visible) {
                Button("Clear tasks", role: .destructive) { Task { await model.clearBoard() } }
            }
        }
    }
}

/// Running subagent cards between the thread and the composer, plus a chip for the finished ones.
struct SubagentTray: View {
    @Environment(AppModel.self) private var model
    @State private var opened: Silo_V1_Subagent?

    var body: some View {
        let running = model.subagents.filter(\.running)
        let finished = model.subagents.filter { !$0.running }
        if !model.subagents.isEmpty {
            HStack(spacing: 8) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(running, id: \.id) { agent in
                            card(agent)
                        }
                    }
                }
                if running.count > 1 {
                    Button {
                        Task { await model.stopAllSubagents() }
                    } label: {
                        Image(systemName: "stop.circle")
                    }
                    .accessibilityLabel("Stop all subagents")
                }
                if !finished.isEmpty {
                    Menu {
                        ForEach(finished, id: \.id) { agent in
                            Button {
                                opened = agent
                            } label: {
                                Label("\(agent.name) · \(agent.status)", systemImage: agent.status == "done" ? "checkmark" : "exclamationmark.triangle")
                            }
                        }
                    } label: {
                        Text("\(finished.count) finished")
                            .font(.caption.weight(.medium))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(Theme.well, in: Capsule())
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

    private func card(_ agent: Silo_V1_Subagent) -> some View {
        let waiting = model.waitingRunIDs.contains(agent.runID)
        return Button {
            opened = agent
        } label: {
            VStack(alignment: .leading, spacing: 3) {
                Text(agent.name).font(.subheadline.weight(.medium)).lineLimit(1)
                HStack(spacing: 5) {
                    StatusDot(status: waiting ? "needs_you" : "working", size: 7)
                    Text(waiting ? "needs you" : (agent.activity.isEmpty ? shortModel(agent.model) : agent.activity))
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            .frame(width: 200, alignment: .leading)
            .siloCard(padding: 10)
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button(role: .destructive) {
                Task { await model.stopSubagent(agent.id) }
            } label: {
                Label("Stop \(agent.name)", systemImage: "stop.circle")
            }
        }
    }
}

extension Silo_V1_Subagent: @retroactive Identifiable {}

func shortModel(_ id: String) -> String {
    id.split(separator: "/").last.map(String.init) ?? id
}

/// A subagent's read-only log with its brief and Stop.
struct SubagentPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let agent: Silo_V1_Subagent
    @State private var log: LogModel?
    @State private var showBrief = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let log {
                    ThreadView(events: log.events, busy: log.busy, interactive: false)
                } else {
                    ProgressView().frame(maxHeight: .infinity)
                }
            }
            .navigationTitle(agent.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } }
                ToolbarItemGroup(placement: .primaryAction) {
                    Button { showBrief = true } label: { Label("Brief", systemImage: "text.alignleft") }
                    if current.running {
                        Button(role: .destructive) {
                            Task { await model.stopSubagent(agent.id) }
                        } label: {
                            Label("Stop", systemImage: "stop.circle")
                        }
                    }
                }
            }
            .sheet(isPresented: $showBrief) { brief }
        }
        .onAppear {
            guard log == nil, let botID = model.selectedBotID else { return }
            let model = LogModel(client: model.client, botID: botID, chatID: agent.chatID)
            log = model
            model.start()
        }
        .onDisappear { log?.stop() }
    }

    /// The freshest copy of this agent (the tray list refreshes while it runs).
    private var current: Silo_V1_Subagent {
        model.subagents.first { $0.id == agent.id } ?? agent
    }

    private var brief: some View {
        NavigationStack {
            List {
                Section("Goal") { Text(current.goal).textSelection(.enabled) }
                if !current.context.isEmpty {
                    Section("Context") { Text(current.context).textSelection(.enabled) }
                }
                Section("Model") { Text(current.model) }
                if !current.result.isEmpty {
                    Section("Result") { Text(current.result).textSelection(.enabled) }
                }
            }
            .navigationTitle("Brief")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { showBrief = false } } }
        }
        .presentationDetents([.medium, .large])
    }
}
