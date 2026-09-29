import SiloClient
import SwiftUI

/// Everything a Bot runs on: its machine, the drive sidecar, and one sidecar per STDIO connector.
struct ContainersPage: View {
    @Environment(AppModel.self) private var model
    @State private var containers: [Silo_V1_BotContainer] = []
    @State private var loaded = false
    @State private var confirmRemove = false
    @State private var confirmReset = false

    var body: some View {
        List {
            Section {
                ForEach(containers, id: \.name) { box in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(box.label).font(.headline)
                            Spacer()
                            StatusLine(status: box.state == "running" ? "online" : "stopped")
                        }
                        if !box.detail.isEmpty { Text(box.detail).font(.footnote).foregroundStyle(.secondary) }
                        Text(usage(box)).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                        if !box.image.isEmpty {
                            Text(box.image).font(.system(.caption2, design: .monospaced)).foregroundStyle(.tertiary).lineLimit(1)
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
            Section {
                if let bot = model.selectedBot {
                    if bot.workerConnected {
                        Button("Stop machine") { Task { await model.stopBot(); await load() } }
                    } else {
                        Button("Start machine") { Task { await model.startBot(); await load() } }
                    }
                }
                Button("Remove all containers", role: .destructive) { confirmRemove = true }
                Button("Reset machine", role: .destructive) { confirmReset = true }
            } footer: {
                Text("Removing keeps the workspace, drives, connectors and caches; each container is made again on its next use. Reset stops and removes the machine; Start makes a new one.")
            }
        }
        .overlay { if loaded && containers.isEmpty { ContentUnavailableView("No containers yet", systemImage: "shippingbox") } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .confirmationDialog("Remove all containers?", isPresented: $confirmRemove, titleVisibility: .visible) {
            Button("Remove all containers", role: .destructive) { Task { await removeAll() } }
        }
        .confirmationDialog("Reset the machine?", isPresented: $confirmReset, titleVisibility: .visible) {
            Button("Reset machine", role: .destructive) { Task { await reset() } }
        }
    }

    private func usage(_ box: Silo_V1_BotContainer) -> String {
        guard box.state == "running" else { return box.state }
        let mem = box.memLimit > 0 ? "\(formatBytes(box.memUsed)) / \(formatBytes(box.memLimit))" : formatBytes(box.memUsed)
        return "\(String(format: "%.1f", box.cpuPercent))% CPU · \(mem) · up \(relativeTime(box.startedAt).replacingOccurrences(of: " ago", with: ""))"
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { containers = try await model.client.listBotContainers(botID) } catch { await model.report(error) }
        loaded = true
    }

    private func removeAll() async {
        guard let botID = model.selectedBotID else { return }
        do { model.applyBot(try await model.client.removeBotContainers(botID)) } catch { await model.report(error) }
        await load()
    }

    private func reset() async {
        guard let botID = model.selectedBotID else { return }
        do { model.applyBot(try await model.client.resetContainer(botID)) } catch { await model.report(error) }
        await load()
    }
}
