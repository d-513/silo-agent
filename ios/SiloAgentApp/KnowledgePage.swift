import SiloClient
import SwiftUI

extension Silo_V1_KnowledgeFolder: @retroactive Identifiable {}

/// Folders of documents the Bot can search with `search_docs`. Files stay on the Bot's machine;
/// only their text and an embedding are kept. Searching here is the same search the Bot gets.
struct KnowledgePage: View {
    @Environment(AppModel.self) private var model
    @State private var folders: [Silo_V1_KnowledgeFolder] = []
    @State private var enabled = true
    @State private var loaded = false
    @State private var picking = false
    @State private var removing: Silo_V1_KnowledgeFolder?
    @State private var query = ""
    @State private var hits: [Silo_V1_KnowledgeHit]?
    @State private var searching = false

    var body: some View {
        List {
            if !enabled {
                Section {
                    Label("The operator turned knowledge off, so nothing new is indexed and the Bot cannot search.", systemImage: "exclamationmark.triangle")
                        .foregroundStyle(Theme.vermilion)
                }
            }
            if let hits {
                Section(searching ? "Searching…" : (hits.isEmpty ? "Nothing matches" : "Matches")) {
                    ForEach(Array(hits.enumerated()), id: \.offset) { _, hit in
                        HitRow(hit: hit)
                    }
                }
            } else {
                Section {
                    ForEach(folders) { folder in
                        FolderRow(folder: folder, canSync: model.selectedBot?.workerConnected == true)
                            .swipeActions {
                                Button(role: .destructive) { removing = folder } label: { Label("Remove", systemImage: "trash") }
                                Button { Task { await sync(folder) } } label: { Label("Sync", systemImage: "arrow.triangle.2.circlepath") }
                                    .tint(Theme.cobalt)
                                    .disabled(model.selectedBot?.workerConnected != true)
                            }
                            .contextMenu {
                                Button { Task { await sync(folder) } } label: { Label("Sync now", systemImage: "arrow.triangle.2.circlepath") }
                                    .disabled(model.selectedBot?.workerConnected != true)
                                Button(role: .destructive) { removing = folder } label: { Label("Remove", systemImage: "trash") }
                            }
                    }
                } header: {
                    if !folders.isEmpty { Text("Indexed folders") }
                } footer: {
                    Text("PDFs, Word and OpenDocument files, HTML, Markdown, plain text and code. Scanned pages and images are read with OCR. Changes are picked up every few minutes.")
                }
            }
        }
        .searchable(text: $query, prompt: "Search the documents")
        .onSubmit(of: .search) { Task { await search() } }
        .onChange(of: query) { _, value in if value.isEmpty { hits = nil } }
        .overlay {
            if loaded && folders.isEmpty && hits == nil {
                ContentUnavailableView {
                    Label("No folders yet", systemImage: "text.book.closed")
                } description: {
                    Text("Pick a folder of documents and the Bot can search it by meaning and by exact words, with the file and page cited.")
                } actions: {
                    Button("Add Folder") { picking = true }.buttonStyle(.ink)
                }
            }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { picking = true } label: { Label("Add Folder", systemImage: "plus") }
                    .disabled(!enabled)
            }
        }
        .sheet(isPresented: $picking) {
            FolderPicker(taken: folders.map(\.path)) { path in await add(path) }
        }
        .confirmationDialog("Remove this folder?", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing) { folder in
            Button("Stop indexing \(folder.path)", role: .destructive) { Task { await remove(folder) } }
        } message: { _ in
            Text("The files stay; the Bot can no longer search them.")
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(folders.contains { $0.status == "syncing" } ? 2.5 : 20))
            }
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listKnowledge(botID: botID)
            folders = list.folders
            enabled = list.enabled
        } catch { await model.report(error) }
        loaded = true
    }

    private func add(_ path: String) async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        do {
            folders.append(try await model.client.addKnowledgeFolder(botID: botID, path: path))
            return true
        } catch {
            await model.report(error)
            return false
        }
    }

    private func sync(_ folder: Silo_V1_KnowledgeFolder) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let fresh = try await model.client.syncKnowledge(botID: botID, id: folder.id)
            if let i = folders.firstIndex(where: { $0.id == fresh.id }) { folders[i] = fresh }
        } catch { await model.report(error) }
    }

    private func remove(_ folder: Silo_V1_KnowledgeFolder) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.removeKnowledgeFolder(botID: botID, id: folder.id)
            folders.removeAll { $0.id == folder.id }
        } catch { await model.report(error) }
    }

    private func search() async {
        guard let botID = model.selectedBotID else { return }
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else { hits = nil; return }
        searching = true
        hits = hits ?? []
        defer { searching = false }
        do { hits = try await model.client.searchKnowledge(botID: botID, query: q) } catch { await model.report(error) }
    }
}

private struct FolderRow: View {
    let folder: Silo_V1_KnowledgeFolder
    let canSync: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: folder.drive ? "externaldrive" : "folder").foregroundStyle(.secondary)
                Text(folder.path).font(.siloMono(.body)).lineLimit(1).truncationMode(.middle)
            }
            status
            if folder.status == "idle", !folder.detail.isEmpty {
                Text(folder.detail).font(.silo(.footnote)).foregroundStyle(.secondary)
            }
            if folder.skipped > 0 {
                DisclosureGroup {
                    ForEach(folder.issues, id: \.path) { issue in
                        VStack(alignment: .leading, spacing: 1) {
                            Text(issue.path).font(.siloMono(.caption))
                            Text(issue.detail).font(.silo(.caption)).foregroundStyle(.secondary)
                        }
                    }
                    if folder.skipped > folder.issues.count {
                        Text("…and \(Int(folder.skipped) - folder.issues.count) more").font(.silo(.caption)).foregroundStyle(.secondary)
                    }
                } label: {
                    Text("\(folder.skipped) not indexed").font(.silo(.footnote)).foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private var status: some View {
        switch folder.status {
        case "syncing":
            HStack(spacing: 6) {
                ProgressView().controlSize(.mini)
                Text(folder.files > 0 ? "Indexing… \(folder.files) files so far" : "Indexing…")
            }
            .font(.silo(.footnote))
            .foregroundStyle(.secondary)
        case "error":
            Text(folder.detail.isEmpty ? "Sync failed." : folder.detail).font(.silo(.footnote)).foregroundStyle(Theme.vermilion)
        default:
            Text(["\(folder.files) files", "\(folder.chunks) passages", folder.lastSyncAt.isEmpty ? "" : "synced \(relativeTime(folder.lastSyncAt))"]
                .filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.silo(.footnote))
                .foregroundStyle(.secondary)
        }
    }
}

private struct HitRow: View {
    let hit: Silo_V1_KnowledgeHit

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(hit.path).font(.siloMono(.subheadline)).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 6)
                if !hit.locator.isEmpty { Text(hit.locator).font(.silo(.caption)).foregroundStyle(.secondary) }
            }
            Text(hit.snippet).font(.silo(.subheadline)).foregroundStyle(.secondary).lineLimit(5).textSelection(.enabled)
            if !hit.indexedAt.isEmpty { Text("indexed \(shortDay(hit.indexedAt))").font(.silo(.caption2)).foregroundStyle(.tertiary) }
        }
        .padding(.vertical, 2)
    }
}

/// Browse the workspace's folders and pick one to index. Drives are shown with a note: indexing
/// reads every file over the network.
private struct FolderPicker: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let taken: [String]
    let add: (String) async -> Bool

    var body: some View {
        NavigationStack {
            Group {
                if model.selectedBot?.workerConnected == true {
                    FolderLevel(path: "", taken: taken, add: pick)
                } else {
                    NeedMachine(what: "Picking a folder")
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }

    private func pick(_ path: String) async {
        if await add(path) { dismiss() }
    }
}

private struct FolderLevel: View {
    @Environment(AppModel.self) private var model
    let path: String
    let taken: [String]
    let add: (String) async -> Void
    @State private var dirs: [Silo_V1_FileEntry] = []
    @State private var loaded = false
    @State private var adding = false

    var body: some View {
        List {
            if let blocked {
                Section { Text(blocked).foregroundStyle(.secondary) }
            } else if path.hasPrefix("drives/") {
                Section {
                    Label("This folder is on a drive. Indexing reads every file over the network, so a large folder can be slow.", systemImage: "externaldrive")
                        .font(.silo(.footnote))
                        .foregroundStyle(.secondary)
                }
            }
            Section {
                ForEach(dirs, id: \.path) { dir in
                    NavigationLink(dir.name) { FolderLevel(path: dir.path, taken: taken, add: add) }
                }
            }
        }
        .overlay { if loaded && dirs.isEmpty && path.isEmpty { ContentUnavailableView("No folders", systemImage: "folder") } }
        .navigationTitle(path.isEmpty ? "Workspace" : (path.split(separator: "/").last.map(String.init) ?? path))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(adding ? "Adding…" : "Index") {
                    adding = true
                    Task { await add(path); adding = false }
                }
                .disabled(blocked != nil || adding)
            }
        }
        .task {
            guard let botID = model.selectedBotID else { return }
            do {
                dirs = try await model.client.listFiles(botID: botID, path: path).filter {
                    $0.dir && !$0.name.hasPrefix(".") && !(path.isEmpty && ["bot", "tmp"].contains($0.name))
                }
                .sorted { $0.name.lowercased() < $1.name.lowercased() }
            } catch { await model.report(error) }
            loaded = true
        }
    }

    /// Why this folder cannot be indexed; nil when it can.
    private var blocked: String? {
        if path.isEmpty { return "Open a folder to index it." }
        if path == "drives" { return "Open one drive folder, not all drives at once." }
        if taken.contains(where: { path == $0 || path.hasPrefix($0 + "/") }) { return "Already indexed." }
        if taken.contains(where: { $0.hasPrefix(path + "/") }) { return "Holds a folder that is already indexed." }
        return nil
    }
}
