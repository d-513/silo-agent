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
    @State private var opened: IdentifiedURL?
    @State private var seen = FirstSeen()
    @State private var changed = 0

    private var full: Bool {
        guard let list else { return false }
        return list.max > 0 && list.tunnels.count >= Int(list.max)
    }

    var body: some View {
        PageScroll(spacing: 16) {
            if let list {
                if let notice = tunnelNotice(list.state) {
                    MachineNote(text: notice)
                } else if list.tunnels.isEmpty {
                    EmptyWell(symbol: "globe", title: "No tunnels yet", message: "Start a service on the Bot's machine, then add its port here, or ask the Bot to open one.") {
                        Button("Open tunnel") { adding = true }.buttonStyle(.quiet).controlSize(.small)
                    }
                } else {
                    PanelSection(title: "Tunnels", footer: footnote(list)) {
                        ForEach(list.tunnels) { tunnel in
                            TunnelRow(tunnel: tunnel) {
                                if let url = URL(string: tunnel.url) { opened = IdentifiedURL(url: url) }
                            } flip: { value in
                                Task { await setPublic(tunnel, value) }
                            } delete: {
                                Task { await delete(tunnel) }
                            }
                            .rise(when: seen.isNew(tunnel.id))
                        }
                    }
                }
                if !shares.isEmpty {
                    PanelSection(title: "Shared links", footer: "Each link stays pinned to the version you shared and works while the Bot is stopped.") {
                        ForEach(shares, id: \.id) { share in
                            ShareRow(share: share) {
                                if let url = URL(string: share.url) { opened = IdentifiedURL(url: url) }
                            } remove: {
                                Task { await removeShare(share) }
                            }
                        }
                    }
                }
            } else {
                SkeletonRows(count: 2, height: 84)
            }
        }
        .pageActions {
            if let list, tunnelNotice(list.state) == nil {
                Button { adding = true } label: { Image(systemName: "plus") }
                    .buttonStyle(.glyph)
                    .disabled(full)
                    .accessibilityLabel("Open tunnel")
            }
        }
        .sheet(isPresented: $adding) {
            NewTunnelSheet { port, isPublic in await create(port, isPublic) }
                .siloSheet(detents: [.medium])
        }
        .sheet(item: $opened) { SafariView(url: $0.url).ignoresSafeArea() }
        .sensoryFeedback(.success, trigger: changed)
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            // The Bot can open tunnels too, so look again while the page is shown.
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(15))
            }
        }
    }

    private func footnote(_ list: Silo_V1_ListTunnelsResponse) -> String {
        full ? "This Bot has \(list.max) tunnels, the most it can have. Delete one to add another."
            : "A private tunnel asks you to sign in to Silo once in the browser that opens it."
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let fresh = try await model.client.listTunnels(botID: botID)
            seen.note(fresh.tunnels.map(\.id))
            withAnimation(list == nil ? nil : Motion.state) { list = fresh }
            shares = (try? await model.client.listArtifactShares(botID: botID)) ?? shares
        } catch { await model.report(error) }
    }

    private func create(_ port: Int32, _ isPublic: Bool) async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        do {
            _ = try await model.client.createTunnel(botID: botID, port: port, isPublic: isPublic)
            await load()
            changed += 1
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
            _ = withAnimation(Motion.fold) { list?.tunnels.removeAll { $0.id == tunnel.id } }
            changed += 1
        } catch { await model.report(error) }
    }

    private func removeShare(_ share: Silo_V1_ArtifactShare) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteArtifactShare(botID: botID, id: share.id)
            withAnimation(Motion.fold) { shares.removeAll { $0.id == share.id } }
            changed += 1
        } catch { await model.report(error) }
    }
}

private func accessCaption(_ isPublic: Bool) -> String {
    isPublic ? "Anyone with the link can open it, no sign-in." : "Only you, signed in to Silo, can open it."
}

/// The address without its scheme: what a person reads.
private func bareAddress(_ url: String) -> String {
    url.replacingOccurrences(of: #"^https?://"#, with: "", options: .regularExpression)
}

private struct TunnelRow: View {
    let tunnel: Silo_V1_Tunnel
    let open: () -> Void
    let flip: (Bool) -> Void
    let delete: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 12) {
                MachineTile(symbol: tunnel.public ? "globe" : "lock")
                VStack(alignment: .leading, spacing: 2) {
                    Text(tunnel.url.isEmpty ? tunnel.name : bareAddress(tunnel.url))
                        .font(.siloMono(13.5, .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Text(accessCaption(tunnel.public))
                        .font(.silo(12.5))
                        .foregroundStyle(tunnel.public ? Theme.vermilion : Theme.ink2)
                    Text([
                        "Port \(tunnel.port)",
                        tunnel.createdBy == "bot" ? "Opened by the Bot" : "Opened by you",
                        tunnel.lastUsedAt.isEmpty ? "Not used yet" : "Used \(relativeTime(tunnel.lastUsedAt))",
                    ].joined(separator: " · "))
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink3)
                        .lineLimit(2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            TunnelAccess(isPublic: tunnel.public, flip: flip)
            HStack(spacing: 0) {
                if !tunnel.url.isEmpty {
                    CopyButton(text: tunnel.url)
                        .accessibilityLabel("Copy address")
                    Button(action: open) { Image(systemName: "safari") }
                        .buttonStyle(.glyph)
                        .accessibilityLabel("Open")
                }
                Spacer(minLength: 0)
                ArmedButton(title: "Delete", armedTitle: "Tap again: the address stops working", symbol: "trash", small: true, action: delete)
            }
            .padding(.leading, -8)
        }
        .padding(14)
    }
}

/// Private | Public for one tunnel. Going Public takes a second press, because that opens the
/// service to anyone with the link; going Private never does.
private struct TunnelAccess: View {
    let isPublic: Bool
    let flip: (Bool) -> Void
    @State private var armed = false
    @State private var timer: Task<Void, Never>?

    var body: some View {
        Segmented(options: [(false, "Private"), (true, armed ? "Tap again" : "Public")],
                  selection: Binding(get: { isPublic }, set: choose))
            .sensoryFeedback(.warning, trigger: armed) { _, on in on }
    }

    private func choose(_ value: Bool) {
        guard value != isPublic else { return }
        timer?.cancel()
        if value, !armed {
            withAnimation(Motion.state) { armed = true }
            timer = Task {
                try? await Task.sleep(for: .seconds(3))
                guard !Task.isCancelled else { return }
                withAnimation(Motion.state) { armed = false }
            }
            return
        }
        armed = false
        flip(value)
    }
}

private struct ShareRow: View {
    let share: Silo_V1_ArtifactShare
    let open: () -> Void
    let remove: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 12) {
                MachineTile(symbol: "link")
                VStack(alignment: .leading, spacing: 2) {
                    Text(share.title.isEmpty ? "Shared \(share.type)" : share.title)
                        .font(.silo(13.5, .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Text([share.type == "app" ? "App" : "File", "v\(share.version)",
                          share.lastUsedAt.isEmpty ? "Not used yet" : "Used \(relativeTime(share.lastUsedAt))"].joined(separator: " · "))
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink3)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            HStack(spacing: 0) {
                if !share.url.isEmpty {
                    ShareLink(item: share.url) {
                        Image(systemName: "square.and.arrow.up")
                            .font(.system(size: 15))
                            .foregroundStyle(Theme.ink2)
                            .frame(width: 40, height: 40)
                    }
                    .buttonStyle(.press)
                    Button(action: open) { Image(systemName: "safari") }
                        .buttonStyle(.glyph)
                        .accessibilityLabel("Open")
                }
                Spacer(minLength: 0)
                ArmedButton(title: "Remove", symbol: "link.badge.minus", small: true, action: remove)
            }
            .padding(.leading, -8)
        }
        .padding(14)
    }
}

private struct NewTunnelSheet: View {
    @Environment(\.dismiss) private var dismiss
    let create: (Int32, Bool) async -> Bool
    @State private var port = ""
    @State private var isPublic = false
    @State private var busy = false
    @State private var touched = false

    private var portError: String? {
        guard touched, case .error(let message) = parseTunnelPort(port) else { return nil }
        return message
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: "Open tunnel", onCancel: { dismiss() }) {
                Button("Open") { open() }.buttonStyle(.ink).disabled(busy)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    VStack(alignment: .leading, spacing: 6) {
                        SiloField(label: "Port", prompt: "8000", text: $port, error: portError)
                            .keyboardType(.numberPad)
                            .onChange(of: port) { touched = true }
                        if portError == nil {
                            Text("The port a service on the Bot's machine listens on.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                        }
                    }
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Who can open it").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                        Segmented(options: [(false, "Private"), (true, "Public")], selection: $isPublic)
                        Text(accessCaption(isPublic)).font(.silo(12.5)).foregroundStyle(isPublic ? Theme.vermilion : Theme.ink3)
                    }
                }
                .padding(16)
            }
        }
    }

    private func open() {
        guard case .port(let value) = parseTunnelPort(port) else { touched = true; return }
        busy = true
        Task {
            if await create(value, isPublic) { dismiss() }
            busy = false
        }
    }
}
