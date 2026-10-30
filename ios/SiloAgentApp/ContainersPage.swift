import SiloClient
import SwiftUI

/// Everything a Bot runs on: its machine, the drive sidecar, and one sidecar per STDIO connector.
struct ContainersPage: View {
    @Environment(AppModel.self) private var model
    @State private var containers: [Silo_V1_BotContainer] = []
    @State private var loaded = false
    @State private var removed = 0

    private var running: [Silo_V1_BotContainer] { containers.filter { $0.state == "running" } }

    var body: some View {
        PageScroll(spacing: 12) {
            if !loaded {
                SkeletonRows(count: 2, height: 120)
            } else if containers.isEmpty {
                EmptyWell(symbol: "shippingbox", title: "No containers yet")
            } else {
                summary
                ForEach(containers, id: \.name) { box in
                    ContainerCard(box: box) {
                        if box.kind == "bot" { power }
                    }
                }
            }
            PanelSection(title: "Dangerous", footer: "Removing keeps the workspace, drives, connectors and caches; each container is made again on its next use. Reset stops and removes the machine; Start makes a new one.") {
                PanelRow(title: "Remove all containers", trailing: {
                    ArmedButton(title: "Remove", armedTitle: "Tap again to remove all", small: true) { Task { await removeAll() } }
                })
                PanelRow(title: "Reset machine", trailing: {
                    ArmedButton(title: "Reset", armedTitle: "Tap again to reset", symbol: "arrow.counterclockwise", small: true) { Task { await reset() } }
                })
            }
            .padding(.top, 8)
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sensoryFeedback(.success, trigger: removed)
    }

    /// The machines at a glance, on a `well` band.
    private var summary: some View {
        HStack(alignment: .top, spacing: 16) {
            ContainerStat(label: "Running", value: "\(running.count) of \(containers.count)")
            ContainerStat(label: "CPU", value: String(format: "%.1f%%", running.reduce(0) { $0 + $1.cpuPercent }))
            ContainerStat(label: "RAM", value: formatBytes(running.reduce(0) { $0 + $1.memUsed }))
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }

    /// The Bot's Start or Stop, on its machine's card. `powerWord` says which.
    @ViewBuilder
    private var power: some View {
        if let bot = model.selectedBot {
            let word = powerWord(machine: bot.machine)
            if bot.machine == "online" {
                Button { Task { await model.stopBot(); await load() } } label: { powerLabel(word) }
                    .buttonStyle(.quiet)
                    .controlSize(.small)
            } else {
                Button { Task { await model.startBot(); await load() } } label: { powerLabel(word) }
                    .buttonStyle(.ink)
                    .controlSize(.small)
                    .disabled(bot.machine == "starting")
            }
        }
    }

    private func powerLabel(_ word: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: "power").font(.system(size: 12, weight: .medium))
            Text(word)
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { containers = try await model.client.listBotContainers(botID) } catch { await model.report(error) }
        loaded = true
    }

    private func removeAll() async {
        guard let botID = model.selectedBotID else { return }
        do {
            model.applyBot(try await model.client.removeBotContainers(botID))
            removed += 1
        } catch { await model.report(error) }
        await load()
    }

    private func reset() async {
        guard let botID = model.selectedBotID else { return }
        do {
            model.applyBot(try await model.client.resetContainer(botID))
            removed += 1
        } catch { await model.report(error) }
        await load()
    }
}

private struct ContainerCard<Accessory: View>: View {
    let box: Silo_V1_BotContainer
    @ViewBuilder let accessory: Accessory

    private var up: Bool { box.state == "running" }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 12) {
                MachineTile(symbol: kind.symbol)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(box.label).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        Chip(text: kind.chip, tint: Theme.ink3)
                    }
                    if !box.detail.isEmpty {
                        Text(box.detail).font(.silo(12.5)).foregroundStyle(Theme.ink2).lineLimit(1)
                    }
                    HStack(spacing: 10) {
                        StatusWord(look: containerLook(box.state))
                        if up, let uptime {
                            Text(uptime).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                        }
                    }
                    .padding(.top, 2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                accessory
            }
            if up {
                HStack(alignment: .top, spacing: 20) {
                    ContainerStat(label: "CPU", value: String(format: "%.1f%%", box.cpuPercent)) {
                        ContainerMeter(value: box.cpuPercent, limit: 100)
                    }
                    ContainerStat(label: "RAM", value: box.memLimit > 0 ? "\(formatBytes(box.memUsed)) / \(formatBytes(box.memLimit))" : formatBytes(box.memUsed)) {
                        ContainerMeter(value: Double(box.memUsed), limit: Double(box.memLimit))
                    }
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(box.name)
                if !box.image.isEmpty { Text(box.image) }
            }
            .font(.siloMono(11.5))
            .foregroundStyle(Theme.ink3)
            .lineLimit(1)
            .truncationMode(.middle)
        }
        .padding(14)
        .machineFolio()
    }

    private var kind: (symbol: String, chip: String) {
        switch box.kind {
        case "bot": return ("display", "Machine")
        case "drive": return ("externaldrive", "Drive sidecar")
        default: return ("powerplug", "MCP sidecar")
        }
    }

    /// "Up 3h 12m", counted from when the engine says it started.
    private var uptime: String? {
        guard let start = parseDate(box.startedAt) else { return nil }
        var seconds = max(0, Int(Date().timeIntervalSince(start)))
        let days = seconds / 86_400
        seconds -= days * 86_400
        let hours = seconds / 3600
        let minutes = (seconds - hours * 3600) / 60
        if days > 0 { return "Up \(days)d \(hours)h" }
        if hours > 0 { return "Up \(hours)h \(minutes)m" }
        if minutes > 0 { return "Up \(minutes)m" }
        return "Up just now"
    }
}

/// A caps label over a mono figure, with an optional meter under it.
private struct ContainerStat<Meter: View>: View {
    let label: String
    let value: String
    @ViewBuilder var meter: Meter

    init(label: String, value: String, @ViewBuilder meter: () -> Meter = { EmptyView() }) {
        self.label = label
        self.value = value
        self.meter = meter()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            SectionLabel(text: label)
            Text(value)
                .font(.siloMono(14, .medium))
                .monospacedDigit()
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            meter.padding(.top, 4)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct ContainerMeter: View {
    let value: Double
    let limit: Double

    var body: some View {
        let share = limit > 0 ? min(1, value / limit) : 0
        GeometryReader { box in
            Capsule().fill(Theme.pressed)
                .overlay(alignment: .leading) {
                    Capsule().fill(Theme.ink).frame(width: box.size.width * share)
                }
                .animation(Motion.quiet(0.28), value: share)
        }
        .frame(height: 6)
        .accessibilityHidden(true)
    }
}
