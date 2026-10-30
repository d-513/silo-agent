import SiloClient
import SwiftUI

extension Silo_V1_Connector: @retroactive Identifiable {}

/// The Workspace's connectors: every connection kept once here, which Bots use each, and adding
/// one from the library. A connection opens to Choose Bots, its sign-in and lending, and Delete.
/// OAuth sign-in, editing and custom MCP servers stay on the web (see `ios/todo_skipped.md`).
struct SpaceConnectorsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var rows: [Silo_V1_Connector] = []
    @State private var loaded = false
    @State private var adding = false
    @State private var query = ""

    private var shown: [Silo_V1_Connector] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return rows.filter { q.isEmpty || ($0.name + " " + $0.description_p).lowercased().contains(q) }
    }

    var body: some View {
        PageScroll {
            Text("Connect a service once for the whole workspace, then choose which Bots can use it. Each Bot keeps its own session and its own rules for it.")
                .font(.silo(13.5))
                .foregroundStyle(Theme.ink2)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 4)
            if !loaded {
                SkeletonRows(count: 3, height: 112)
            } else if rows.isEmpty {
                EmptyWell(symbol: "puzzlepiece.extension", title: "No connectors in this workspace yet", message: "Add one from the library, then choose which Bots can use it.") {
                    Button("Browse Library") { adding = true }.buttonStyle(.ink)
                }
            } else {
                SearchBox(prompt: "Search connectors", text: $query) {}
                if shown.isEmpty {
                    EmptyWell(title: "Nothing matches “\(query)”.")
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 300), spacing: 12, alignment: .top)], spacing: 12) {
                    ForEach(shown) { c in
                        NavigationLink {
                            ConnectionPage(id: c.id, initial: c)
                        } label: {
                            ConnectionCard(c: c)
                        }
                        .buttonStyle(.press)
                        .transition(.rise)
                    }
                }
                .animation(Motion.arrival, value: shown.map(\.id))
            }
        }
        .pageActions {
            Button { adding = true } label: { Image(systemName: "plus") }
                .buttonStyle(.glyph)
                .accessibilityLabel("Add")
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
        .sheet(isPresented: $adding) { AddConnectorSheet(names: rows.map(\.name)) { await load() } }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do { rows = try await model.client.listSpaceConnectors(spaceID: spaceID) } catch { await model.report(error) }
        loaded = true
    }
}

/// One connection: its mark and name, what it is, whose sign-in it carries, who uses it.
private struct ConnectionCard: View {
    let c: Silo_V1_Connector

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 14) {
                ConnectorMark(id: c.id, hasImage: c.hasImage_p, size: 44)
                VStack(alignment: .leading, spacing: 4) {
                    FlowRow(spacing: 6) {
                        Text(c.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        if !c.category.isEmpty { Chip(text: c.category, tint: Theme.ink3) }
                    }
                    let about = c.description_p.isEmpty ? (c.transport == "stdio" ? c.stdioCommand : c.httpURL) : c.description_p
                    if !about.isEmpty {
                        Text(about)
                            .font(.silo(12.5))
                            .foregroundStyle(Theme.ink2)
                            .lineLimit(2)
                            .multilineTextAlignment(.leading)
                    }
                    if needsSignIn(c) {
                        StatusWord(look: StatusLook(lamp: .needsYou, word: "Needs sign-in", tone: .vermilion), font: .silo(11.5, .medium))
                            .padding(.top, 2)
                    } else if !c.lentBy.isEmpty {
                        Text("Signed in by \(c.lentBy)").font(.silo(11.5, .medium)).foregroundStyle(Theme.ink3).lineLimit(1)
                            .padding(.top, 2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            Theme.line.frame(height: 1).padding(.top, 12)
            UsedByLine(ids: c.usedBy, hidden: c.usedByHidden).padding(.top, 10)
        }
        .folio()
    }
}

private func needsSignIn(_ c: Silo_V1_Connector) -> Bool { c.auth == "oauth" && !c.signedIn }

/// A connection: Choose Bots, its sign-in and lending, and Delete for who added it or an admin.
private struct ConnectionPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    let id: String
    @State private var c: Silo_V1_Connector

    init(id: String, initial: Silo_V1_Connector) {
        self.id = id
        _c = State(initialValue: initial)
    }

    var body: some View {
        SiloPage(title: c.name) {
            PageScroll {
                head
                LendSection(lentBy: c.lentBy, shared: c.shared) { shared in
                    await act { try await model.client.setLendShared(connectorID: c.id, shared: shared) }
                }
                ChooseBotsSection(granted: c.usedBy) { botID, on in
                    await act {
                        if on { try await model.client.grantConnector(botID: botID, connectorID: c.id) } else {
                            try await model.client.revokeConnector(botID: botID, connectorID: c.id)
                        }
                    }
                }
                if c.canEdit {
                    PanelSection(footer: (c.usedBy.isEmpty && c.usedByHidden == 0 ? "It is removed from the workspace." : "Every Bot that uses it loses it too.")
                        + " Editing its settings is on the web client. Changes there apply to every Bot that uses it.") {
                        ArmedButton(title: "Delete connector", armedTitle: "Tap again to delete \(c.name)", wide: true) {
                            Task {
                                await act { try await model.client.deleteConnector(id: c.id) }
                                dismiss()
                            }
                        }
                        .padding(6)
                    }
                }
            }
            .refreshable { await reload() }
        }
    }

    private var head: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 14) {
                ConnectorMark(id: c.id, hasImage: c.hasImage_p, size: 48)
                VStack(alignment: .leading, spacing: 4) {
                    Text(c.name).font(.silo(22, .medium)).foregroundStyle(Theme.ink)
                    if !c.description_p.isEmpty {
                        Text(c.description_p)
                            .font(.silo(14))
                            .foregroundStyle(Theme.ink2)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(c.transport == "builtin" ? "Built-in" : c.transport == "stdio" ? c.stdioCommand : c.httpURL)
                    .lineLimit(2)
                if !c.slug.isEmpty { Text("tools.\(c.slug)") }
            }
            .font(.siloMono(12))
            .foregroundStyle(Theme.ink3)
            if needsSignIn(c) {
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Lamp(status: .needsYou)
                    Text("Needs sign-in. Sign in on the web client.").font(.silo(14)).foregroundStyle(Theme.vermilion)
                }
            } else if !c.lentBy.isEmpty {
                Text("Signed in by \(c.lentBy)").font(.silo(14)).foregroundStyle(Theme.ink2)
            }
        }
        .folio()
    }

    private func act(_ work: () async throws -> Void) async {
        do { try await work() } catch { await model.report(error) }
        await reload()
    }

    private func reload() async {
        guard let spaceID = spaces.currentID,
              let list = try? await model.client.listSpaceConnectors(spaceID: spaceID),
              let fresh = list.first(where: { $0.id == id }) else { return }
        c = fresh
    }
}

/// The library: pick a preset, name the copy, choose which Bots get it.
private struct AddConnectorSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let names: [String]
    let onAdded: () async -> Void
    @State private var library: [Silo_V1_Connector] = []
    @State private var loaded = false
    @State private var query = ""

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                SheetHead(title: "Library", cancel: "Done", onCancel: { dismiss() })
                PageScroll {
                    SearchBox(prompt: "Search the library", text: $query) {}
                    if !loaded {
                        SkeletonRows(count: 6, height: 60)
                    } else if library.isEmpty {
                        EmptyWell(symbol: "puzzlepiece.extension", title: "The library is empty")
                    } else if filtered.isEmpty {
                        EmptyWell(title: "Nothing matches “\(query)”.")
                    } else {
                        Panel {
                            ForEach(filtered) { item in
                                NavigationLink {
                                    AddPresetForm(preset: item, names: names) {
                                        await onAdded()
                                        dismiss()
                                    }
                                } label: {
                                    presetRow(item)
                                }
                                .buttonStyle(RowPressStyle())
                            }
                        }
                    }
                }
            }
            .background(Theme.surface.ignoresSafeArea())
            .toolbar(.hidden, for: .navigationBar)
            .task {
                do { library = try await model.client.listConnectors() } catch { await model.report(error) }
                loaded = true
            }
        }
        .siloSheet()
    }

    private func presetRow(_ item: Silo_V1_Connector) -> some View {
        HStack(spacing: 12) {
            ConnectorMark(id: item.id, hasImage: item.hasImage_p, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.name).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                if !item.description_p.isEmpty {
                    Text(item.description_p)
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink2)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private var filtered: [Silo_V1_Connector] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return library.filter { q.isEmpty || ($0.name + " " + $0.description_p + " " + $0.category).lowercased().contains(q) }
    }
}

private struct AddPresetForm: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    let preset: Silo_V1_Connector
    let names: [String]
    let onDone: () async -> Void
    @State private var name = ""
    @State private var picked: Set<String> = []
    @State private var busy = false
    @State private var failure: String?

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: preset.name, cancel: "Back", onCancel: { dismiss() }) {
                Button(busy ? "Adding…" : "Add") { Task { await add() } }
                    .buttonStyle(.ink)
                    .disabled(busy || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            PageScroll {
                if !preset.catalogGuide.isEmpty {
                    GuideWell(text: preset.catalogGuide)
                }
                VStack(alignment: .leading, spacing: 6) {
                    SiloField(label: "Name", text: $name)
                    Text(preset.auth == "oauth" || !preset.fields.isEmpty
                         ? "Signing in or filling in its settings is on the web client, once it is added."
                         : "The Bots it is given reach its tools through import tools in Python.")
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink3)
                        .fixedSize(horizontal: false, vertical: true)
                }
                BotsStep(picked: $picked)
                if let failure { ErrorLine(text: failure) }
            }
        }
        .background(Theme.surface.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
        .onAppear {
            guard name.isEmpty else { return }
            name = nextCopyName(preset.name, taken: names)
            picked = Set(spaces.managedBots.map(\.id))
        }
    }

    private func add() async {
        guard let spaceID = spaces.currentID else { return }
        busy = true
        defer { busy = false }
        do {
            _ = try await model.client.addLibraryConnector(spaceID: spaceID, preset: preset,
                                                           name: name.trimmingCharacters(in: .whitespaces), botIDs: Array(picked))
            await onDone()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
