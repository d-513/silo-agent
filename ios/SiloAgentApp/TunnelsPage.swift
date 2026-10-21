import SiloClient
import SwiftUI

extension Silo_V1_Tunnel: @retroactive Identifiable {}

/// Addresses for services running on the Bot's machine, and the public links made from artifact
/// cards. A private tunnel opens for the owner signed in to Silo; a public one for anyone.
struct TunnelsPage: View {
    @Environment(AppModel.self) private var model
    @State private var list: Silo_V1_ListTunnelsResponse?
    @State private var shares: [Silo_V1_ArtifactShare] = []
    @State private var adding = false
    @State private var deleting: Silo_V1_Tunnel?
    @State private var going: Silo_V1_Tunnel?
    @State private var opened: IdentifiedURL?

    var body: some View {
        List {
            if let list {
                if let notice = tunnelNotice(list.state) {
                    Section { Text(notice).foregroundStyle(.secondary) }
                } else {
                    Section {
                        ForEach(list.tunnels) { tunnel in
                            TunnelRow(tunnel: tunnel, open: { if let url = URL(string: tunnel.url) { opened = IdentifiedURL(url: url) } },
                                      flip: { flip(tunnel) })
                                .swipeActions {
                                    Button(role: .destructive) { deleting = tunnel } label: { Label("Delete", systemImage: "trash") }
                                }
                        }
                    } header: {
                        Text("Tunnels")
                    } footer: {
                        Text(list.tunnels.isEmpty
                            ? "Start a service on the Bot's machine, then add its port here, or ask the Bot to open one."
                            : "A private tunnel asks you to sign in to Silo once in the browser that opens it.")
                    }
                }
                if !shares.isEmpty {
                    Section {
                        ForEach(shares, id: \.id) { share in
                            ShareRow(share: share) { if let url = URL(string: share.url) { opened = IdentifiedURL(url: url) } }
                                .swipeActions {
                                    Button(role: .destructive) { Task { await removeShare(share) } } label: { Label("Remove", systemImage: "link.badge.minus") }
                                }
                        }
                    } header: {
                        Text("Shared links")
                    } footer: {
                        Text("Each link stays pinned to the version you shared and works while the Bot is stopped.")
                    }
                }
            }
        }
        .overlay { if list == nil { ProgressView() } }
        .toolbar {
            if let list, tunnelNotice(list.state) == nil {
                ToolbarItem(placement: .primaryAction) {
                    Button { adding = true } label: { Label("Open Tunnel", systemImage: "plus") }
                        .disabled(list.max > 0 && list.tunnels.count >= Int(list.max))
                }
            }
        }
        .sheet(isPresented: $adding) {
            NewTunnelSheet { port, isPublic in await create(port, isPublic) }
                .presentationDetents([.medium])
        }
        .sheet(item: $opened) { SafariView(url: $0.url).ignoresSafeArea() }
        .confirmationDialog("Delete this tunnel?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { tunnel in
            Button("Delete \(tunnel.name)", role: .destructive) { Task { await delete(tunnel) } }
        } message: { _ in
            Text("The address stops working. The service keeps running.")
        }
        .confirmationDialog("Make this tunnel public?", isPresented: Binding(get: { going != nil }, set: { if !$0 { going = nil } }),
                            titleVisibility: .visible, presenting: going) { tunnel in
            Button("Make Public") { Task { await setPublic(tunnel, true) } }
        } message: { _ in
            Text("Anyone with the link can open it, without signing in.")
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            // The Bot can open tunnels too, so look again while the page is shown.
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(15))
            }
        }
    }

    private func flip(_ tunnel: Silo_V1_Tunnel) {
        if tunnel.public { Task { await setPublic(tunnel, false) } } else { going = tunnel }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            list = try await model.client.listTunnels(botID: botID)
            shares = (try? await model.client.listArtifactShares(botID: botID)) ?? shares
        } catch { await model.report(error) }
    }

    private func create(_ port: Int32, _ isPublic: Bool) async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        do {
            _ = try await model.client.createTunnel(botID: botID, port: port, isPublic: isPublic)
            await load()
            return true
        } catch {
            await model.report(error)
            return false
        }
    }

    private func setPublic(_ tunnel: Silo_V1_Tunnel, _ value: Bool) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let fresh = try await model.client.updateTunnel(botID: botID, id: tunnel.id, isPublic: value)
            if let i = list?.tunnels.firstIndex(where: { $0.id == fresh.id }) { list?.tunnels[i] = fresh }
        } catch { await model.report(error) }
    }

    private func delete(_ tunnel: Silo_V1_Tunnel) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteTunnel(botID: botID, id: tunnel.id)
            list?.tunnels.removeAll { $0.id == tunnel.id }
        } catch { await model.report(error) }
    }

    private func removeShare(_ share: Silo_V1_ArtifactShare) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteArtifactShare(botID: botID, id: share.id)
            shares.removeAll { $0.id == share.id }
        } catch { await model.report(error) }
    }
}

private struct TunnelRow: View {
    let tunnel: Silo_V1_Tunnel
    let open: () -> Void
    let flip: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(tunnel.name).font(.silo(.headline)).lineLimit(1)
                Spacer(minLength: 6)
                Menu {
                    Button { if tunnel.public { flip() } } label: {
                        Label("Private", systemImage: tunnel.public ? "lock" : "checkmark")
                    }
                    Button { if !tunnel.public { flip() } } label: {
                        Label("Public", systemImage: tunnel.public ? "checkmark" : "globe")
                    }
                } label: {
                    Label(tunnel.public ? "Public" : "Private", systemImage: tunnel.public ? "globe" : "lock.fill")
                        .font(.silo(.caption, .medium))
                        .foregroundStyle(tunnel.public ? Theme.vermilion : .secondary)
                }
            }
            Text([
                "Port \(tunnel.port)",
                tunnel.createdBy == "bot" ? "Opened by the Bot" : "Opened by you",
                tunnel.lastUsedAt.isEmpty ? "Not used yet" : "Used \(relativeTime(tunnel.lastUsedAt))",
            ].joined(separator: " · "))
                .font(.silo(.caption))
                .foregroundStyle(.secondary)
            if !tunnel.url.isEmpty {
                HStack(spacing: 14) {
                    Text(tunnel.url).font(.siloMono(.caption)).foregroundStyle(Theme.cobalt).lineLimit(1).truncationMode(.middle)
                    Spacer(minLength: 0)
                    Button { UIPasteboard.general.string = tunnel.url } label: { Image(systemName: "doc.on.doc") }
                        .accessibilityLabel("Copy address")
                    Button(action: open) { Image(systemName: "safari") }
                        .accessibilityLabel("Open")
                }
                .buttonStyle(.borderless)
            }
        }
        .padding(.vertical, 2)
    }
}

private struct ShareRow: View {
    let share: Silo_V1_ArtifactShare
    let open: () -> Void

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 3) {
                Text(share.title.isEmpty ? "Shared \(share.type)" : share.title).lineLimit(1)
                Text([share.type == "app" ? "App" : "File", "v\(share.version)",
                      share.lastUsedAt.isEmpty ? "Not used yet" : "Used \(relativeTime(share.lastUsedAt))"].joined(separator: " · "))
                    .font(.silo(.caption))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 6)
            if !share.url.isEmpty {
                ShareLink(item: share.url) { Image(systemName: "square.and.arrow.up") }
                Button(action: open) { Image(systemName: "safari") }.accessibilityLabel("Open")
            }
        }
        .buttonStyle(.borderless)
    }
}

private struct NewTunnelSheet: View {
    @Environment(\.dismiss) private var dismiss
    let create: (Int32, Bool) async -> Bool
    @State private var port = ""
    @State private var isPublic = false
    @State private var busy = false
    @State private var touched = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Port, e.g. 8000", text: $port)
                        .keyboardType(.numberPad)
                        .onChange(of: port) { touched = true }
                } footer: {
                    if touched, case .error(let message) = parseTunnelPort(port) {
                        Text(message).foregroundStyle(Theme.vermilion)
                    } else {
                        Text("The port a service on the Bot's machine listens on.")
                    }
                }
                Section {
                    Picker("Who can open it", selection: $isPublic) {
                        Text("Private").tag(false)
                        Text("Public").tag(true)
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    Text(isPublic ? "Anyone with the link can open it, no sign-in." : "Only you, signed in to Silo, can open it.")
                }
            }
            .navigationTitle("Open Tunnel")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Open") {
                        guard case .port(let value) = parseTunnelPort(port) else { touched = true; return }
                        busy = true
                        Task {
                            if await create(value, isPublic) { dismiss() }
                            busy = false
                        }
                    }
                    .disabled(busy)
                }
            }
        }
    }
}
