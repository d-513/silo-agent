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
    @State private var query = ""
    @State private var hits: [Silo_V1_KnowledgeHit]?
    @State private var searching = false

    private var online: Bool { model.selectedBot?.machine == "online" }

    var body: some View {
        PageScroll {
            if !enabled {
                ErrorLine(text: "The operator turned knowledge off, so nothing new is indexed and the Bot cannot search.")
            }
            if !loaded || !folders.isEmpty || hits != nil {
                SearchBox(prompt: "Search the documents", text: $query, searching: searching) { Task { await search() } }
            }
            if !loaded {
                SkeletonRows(count: 3, height: 64)
            } else if let hits {
                PanelSection(title: searching ? "Searching…" : (hits.isEmpty ? "Nothing matches" : "Matches")) {
                    ForEach(Array(hits.enumerated()), id: \.offset) { _, hit in
                        HitRow(hit: hit)
                    }
                }
            } else if folders.isEmpty {
                EmptyWell(symbol: "text.book.closed", title: "No folders yet",
                          message: "Pick a folder of documents and the Bot can search it by meaning and by exact words, with the file and page cited.") {
                    Button("Add folder") { picking = true }.buttonStyle(.ink).controlSize(.small).disabled(!enabled)
                }
            } else {
                PanelSection(title: "Indexed folders",
                             footer: "PDFs, Word and OpenDocument files, HTML, Markdown, plain text and code. Scanned pages and images are read with OCR. Changes are picked up every few minutes.") {
                    ForEach(folders) { folder in
                        FolderRow(folder: folder, canSync: online,
                                  onSync: { Task { await sync(folder) } },
                                  onRemove: { Task { await remove(folder) } })
                    }
                }
            }
        }
        .pageActions {
            Button { picking = true } label: { Image(systemName: "plus") }
                .buttonStyle(.glyph)
                .disabled(!enabled)
                .accessibilityLabel("Add folder")
        }
        .onChange(of: query) { _, value in if value.isEmpty { hits = nil } }
        .sheet(isPresented: $picking) {
            FolderPicker(taken: folders.map(\.path)) { path in await add(path) }
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
            withAnimation(Motion.state) { folders.removeAll { $0.id == folder.id } }
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
    let onSync: () -> Void
    let onRemove: () -> Void
    @State private var open = false

    private var syncing: Bool { folder.status == "syncing" }

    var body: some View {
        HStack(alignment: .top, spacing: 2) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(folder.path).font(.siloMono(13.5)).foregroundStyle(Theme.ink).lineLimit(1).truncationMode(.middle)
                    if folder.drive { Chip(text: "drive") }
                }
                status
                if folder.status == "idle", !folder.detail.isEmpty {
                    Text(folder.detail).font(.silo(12.5)).foregroundStyle(Theme.ink2)
                }
                if folder.skipped > 0 { skipped }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 6)
            Button(action: onSync) { Image(systemName: "arrow.triangle.2.circlepath") }
                .buttonStyle(.glyph(tint: Theme.ink2, side: 34))
                .disabled(!canSync || syncing)
                .accessibilityLabel("Sync now")
            ArmedGlyph(symbol: "trash", label: "Stop indexing \(folder.path)", action: onRemove)
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .padding(.vertical, 6)
    }

    @ViewBuilder
    private var status: some View {
        switch folder.status {
        case "syncing":
            HStack(spacing: 6) {
                Spinner(size: 12)
                Text(folder.files > 0 ? "Indexing… \(folder.files) files so far" : "Indexing…")
            }
            .font(.silo(12.5))
            .foregroundStyle(Theme.ink2)
        case "error":
            Text(folder.detail.isEmpty ? "Sync failed." : folder.detail).font(.silo(12.5)).foregroundStyle(Theme.vermilion)
        default:
            Text(["\(folder.files) files", "\(folder.chunks) passages", folder.lastSyncAt.isEmpty ? "" : "synced \(relativeTime(folder.lastSyncAt))"]
                .filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.silo(12.5))
                .foregroundStyle(Theme.ink2)
        }
    }

    private var skipped: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(Motion.fold) { open.toggle() }
            } label: {
                HStack(spacing: 5) {
                    Text("\(folder.skipped) not indexed").font(.silo(12.5)).foregroundStyle(Theme.ink2)
                    FoldChevron(open: open, size: 9)
                }
                .frame(minHeight: 26)
                .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            if open {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(folder.issues, id: \.path) { issue in
                        VStack(alignment: .leading, spacing: 1) {
                            Text(issue.path).font(.siloMono(12)).foregroundStyle(Theme.ink2)
                            Text(issue.detail).font(.silo(12)).foregroundStyle(Theme.ink3)
                        }
                    }
                    if folder.skipped > folder.issues.count {
                        Text("…and \(Int(folder.skipped) - folder.issues.count) more").font(.silo(12)).foregroundStyle(Theme.ink3)
                    }
                }
                .transition(.opacity)
            }
        }
    }
}

private struct HitRow: View {
    let hit: Silo_V1_KnowledgeHit

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(hit.path).font(.siloMono(12.5)).foregroundStyle(Theme.ink).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 6)
                if !hit.locator.isEmpty { Text(hit.locator).font(.siloMono(12)).foregroundStyle(Theme.ink3) }
            }
            Text(hit.snippet).font(.silo(13.5)).foregroundStyle(Theme.ink2).lineLimit(5).textSelection(.enabled)
            if !hit.indexedAt.isEmpty {
                Text("indexed \(shortDay(hit.indexedAt))").font(.siloMono(11)).foregroundStyle(Theme.ink3)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }
}

/// Browse the workspace's folders one level at a time and pick one to index. Drives are shown with
/// a note: indexing reads every file over the network.
private struct FolderPicker: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let taken: [String]
    let add: (String) async -> Bool
    @State private var path = ""
    @State private var adding = false

    private var online: Bool { model.selectedBot?.machine == "online" }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: "Add folder", onCancel: { dismiss() }) {
                if online {
                    Button {
                        adding = true
                        Task {
                            if await add(path) { dismiss() }
                            adding = false
                        }
                    } label: {
                        HStack(spacing: 6) {
                            if adding { Spinner(size: 13, color: Theme.onInk) }
                            Text(adding ? "Adding…" : "Index")
                        }
                    }
                    .buttonStyle(.ink)
                    .disabled(blocked != nil || adding)
                }
            }
            if online {
                crumbs
                FolderLevel(path: path, blocked: blocked) { path = $0 }
            } else {
                NeedMachine(what: "Picking a folder").frame(maxHeight: .infinity)
            }
        }
        .background(Theme.surface)
        .siloSheet()
    }

    /// The trail to here; a press goes back up.
    private var crumbs: some View {
        let parts = path.split(separator: "/").map(String.init)
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 2) {
                crumb("workspace", to: "", last: parts.isEmpty)
                ForEach(Array(parts.enumerated()), id: \.offset) { index, part in
                    Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.ink3)
                    crumb(part, to: parts[...index].joined(separator: "/"), last: index == parts.count - 1)
                }
            }
            .padding(.horizontal, 12)
        }
        .defaultScrollAnchor(.trailing)
        .frame(height: 40)
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }

    private func crumb(_ label: String, to target: String, last: Bool) -> some View {
        Button { path = target } label: {
            Text(label)
                .font(.silo(13, last ? .medium : .regular))
                .foregroundStyle(last ? Theme.ink : Theme.ink2)
                .padding(.horizontal, 6)
                .frame(height: 30)
                .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .disabled(last)
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

private struct FolderLevel: View {
    @Environment(AppModel.self) private var model
    let path: String
    let blocked: String?
    let open: (String) -> Void
    @State private var dirs: [Silo_V1_FileEntry]?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let blocked {
                    Text(blocked).font(.silo(13)).foregroundStyle(Theme.ink3)
                } else if path.hasPrefix("drives/") {
                    Text("This folder is on a drive. Indexing reads every file over the network, so a large folder can be slow.")
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink2)
                }
                if let dirs {
                    if dirs.isEmpty {
                        Text(path.isEmpty ? "No folders." : "No subfolders.").font(.silo(13)).foregroundStyle(Theme.ink3)
                    } else {
                        Panel {
                            ForEach(dirs, id: \.path) { dir in
                                PanelRow(symbol: "folder", title: dir.name, chevron: true) { open(dir.path) }
                            }
                        }
                    }
                } else {
                    SkeletonRows(count: 4, height: 44)
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: path) {
            dirs = nil
            guard let botID = model.selectedBotID else { return }
            do {
                dirs = try await model.client.listFiles(botID: botID, path: path).filter {
                    $0.dir && !$0.name.hasPrefix(".") && !(path.isEmpty && ["bot", "tmp"].contains($0.name))
                }
                .sorted { $0.name.lowercased() < $1.name.lowercased() }
            } catch {
                dirs = []
                await model.report(error)
            }
        }
    }
}
