import SiloClient
import SwiftUI

extension Silo_V1_Change: @retroactive Identifiable {}

/// What changed in the Bot's workspace, as diffs, and what changed on its drives, as a journal.
/// Files can be put back as they were before a change; the restore is itself a change.
struct ChangesPage: View {
    @Environment(AppModel.self) private var model
    @State private var view = "workspace"
    @State private var hasDrives = false

    var body: some View {
        Group {
            if view == "drives" {
                DriveJournal(hasDrives: $hasDrives)
            } else if model.selectedBot?.workerConnected == true {
                WorkspaceChanges()
            } else {
                NeedMachine(what: "Changes")
            }
        }
        .safeAreaInset(edge: .top) {
            if hasDrives {
                Picker("Show", selection: $view) {
                    Text("Workspace").tag("workspace")
                    Text("Drives").tag("drives")
                }
                .pickerStyle(.segmented)
                .padding(.horizontal)
                .padding(.bottom, 6)
            }
        }
        .task(id: model.selectedBotID) {
            guard let botID = model.selectedBotID else { return }
            hasDrives = (try? await model.client.listDriveChanges(botID: botID).hasDrives_p) ?? false
        }
    }
}

private struct WorkspaceChanges: View {
    @Environment(AppModel.self) private var model
    @State private var list: Silo_V1_ListChangesResponse?

    var body: some View {
        List {
            if let list {
                if let notice = changesNotice(list.state) {
                    Section {
                        Text(notice.text).foregroundStyle(.secondary)
                        if notice.reset { NavigationLink("Open Containers", value: BotRoute.page(.container)) }
                    }
                } else {
                    if list.indexing {
                        Section { Label("Indexing the workspace…", systemImage: "hourglass").foregroundStyle(.secondary) }
                    }
                    if list.hasPending, list.pending.files > 0 {
                        Section("Not yet recorded") {
                            NavigationLink { ChangeDetail(change: list.pending, maxFileBytes: list.maxFileBytes) } label: {
                                ChangeRow(change: list.pending, title: "Now")
                            }
                        }
                    }
                    Section {
                        ForEach(list.changes) { change in
                            NavigationLink { ChangeDetail(change: change, maxFileBytes: list.maxFileBytes) } label: {
                                ChangeRow(change: change, title: changeSourceTitle(change.sources, restore: change.restore))
                            }
                        }
                    } footer: {
                        if !list.since.isEmpty { Text("Recorded since \(shortDay(list.since)).") }
                    }
                }
            }
        }
        .overlay {
            if let list, changesNotice(list.state) == nil, list.changes.isEmpty, !(list.hasPending && list.pending.files > 0) {
                ContentUnavailableView("No changes yet", systemImage: "clock.arrow.trianglehead.counterclockwise.rotate.90",
                                       description: Text("When the Bot or anyone else changes files in the workspace, it shows up here."))
            } else if list == nil {
                ProgressView()
            }
        }
        .refreshable { await load() }
        // Each look snapshots the workspace on the Bot, so poll only while the page is shown.
        .task(id: model.selectedBotID) {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(8))
            }
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { list = try await model.client.listChanges(botID: botID) } catch { await model.report(error) }
    }
}

private struct ChangeRow: View {
    let change: Silo_V1_Change
    let title: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                if change.restore { Image(systemName: "arrow.uturn.backward").foregroundStyle(Theme.cobalt) }
                Text(title).font(.silo(.headline)).lineLimit(2)
                Spacer(minLength: 6)
                if !change.pending { Text(relativeTime(change.at)).font(.silo(.caption)).foregroundStyle(.secondary) }
            }
            HStack(spacing: 8) {
                Text(change.files == 1 ? "1 file" : "\(change.files) files")
                if change.added > 0 { Text("+\(change.added)").foregroundStyle(Theme.emerald) }
                if change.deleted > 0 { Text("−\(change.deleted)").foregroundStyle(Theme.vermilion) }
            }
            .font(.siloMono(.caption))
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }
}

private struct ChangeDetail: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let change: Silo_V1_Change
    let maxFileBytes: Int64
    @State private var files: [Silo_V1_ChangeFile] = []
    @State private var truncated = false
    @State private var loaded = false
    @State private var confirmAll = false
    @State private var notice: String?

    var body: some View {
        List {
            if let notice { Section { Text(notice).font(.silo(.footnote)) } }
            let hint = change.pending ? "" : changeSourceHint(change.sources, restore: change.restore)
            if !hint.isEmpty { Section { Text(hint).font(.silo(.footnote)).foregroundStyle(.secondary) } }
            Section {
                ForEach(files, id: \.path) { file in
                    NavigationLink {
                        PatchPage(change: change, file: file, maxFileBytes: maxFileBytes) { notice = $0 }
                    } label: {
                        FileChangeRow(file: file)
                    }
                }
            } footer: {
                if truncated { Text("Showing the first files of this change.") }
            }
        }
        .overlay { if !loaded { ProgressView() } }
        .navigationTitle(change.pending ? "Now" : (parseDate(change.at)?.formatted(date: .abbreviated, time: .shortened) ?? "Change"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !change.pending {
                ToolbarItem(placement: .primaryAction) {
                    Button { confirmAll = true } label: { Label("Restore All", systemImage: "arrow.uturn.backward") }
                }
            }
        }
        .confirmationDialog("Put every file back as it was before this change?", isPresented: $confirmAll, titleVisibility: .visible) {
            Button("Restore \(files.count == 1 ? "1 file" : "\(files.count) files")", role: .destructive) { Task { await restoreAll() } }
        } message: {
            Text("What is there now is recorded first, so the restore can be undone.")
        }
        .task {
            guard let botID = model.selectedBotID else { return }
            do {
                let out = try await model.client.listChangeFiles(botID: botID, base: change.base, head: change.head)
                files = out.files
                truncated = out.truncated
            } catch { await model.report(error) }
            loaded = true
        }
    }

    private func restoreAll() async {
        guard let botID = model.selectedBotID else { return }
        do {
            notice = changeRestoreNotice(try await model.client.restoreChange(botID: botID, base: change.base, head: change.head), path: "")
        } catch { await model.report(error) }
    }
}

private struct FileChangeRow: View {
    let file: Silo_V1_ChangeFile

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: symbol).foregroundStyle(color).frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                Text(file.path.split(separator: "/").last.map(String.init) ?? file.path).lineLimit(1)
                Text(file.status == "renamed" && !file.oldPath.isEmpty ? "from \(file.oldPath)" : directory)
                    .font(.siloMono(.caption))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Spacer(minLength: 6)
            if file.binary || file.large {
                Text(changeSizes(old: file.oldSize, new: file.newSize)).font(.silo(.caption)).foregroundStyle(.secondary)
            } else {
                HStack(spacing: 4) {
                    if file.added > 0 { Text("+\(file.added)").foregroundStyle(Theme.emerald) }
                    if file.deleted > 0 { Text("−\(file.deleted)").foregroundStyle(Theme.vermilion) }
                }
                .font(.siloMono(.caption))
            }
        }
    }

    private var directory: String {
        let parts = file.path.split(separator: "/")
        return parts.count > 1 ? parts.dropLast().joined(separator: "/") : "workspace"
    }

    private var symbol: String {
        switch file.status {
        case "added": return "plus.circle.fill"
        case "deleted": return "minus.circle.fill"
        case "renamed": return "arrow.right.circle.fill"
        default: return "pencil.circle.fill"
        }
    }

    private var color: Color {
        switch file.status {
        case "added": return Theme.emerald
        case "deleted": return Theme.vermilion
        default: return .secondary
        }
    }
}

/// One file's diff, numbered, with the restore button for that file.
private struct PatchPage: View {
    @Environment(AppModel.self) private var model
    let change: Silo_V1_Change
    let file: Silo_V1_ChangeFile
    let maxFileBytes: Int64
    let restored: (String) -> Void
    @State private var lines: [DiffLine]?
    @State private var truncated = false
    @State private var confirm = false

    var body: some View {
        ScrollView([.vertical, .horizontal]) {
            VStack(alignment: .leading, spacing: 0) {
                let note = changeFileNote(file, maxFileBytes: maxFileBytes)
                if !note.isEmpty {
                    Text(note).font(.silo(.footnote)).foregroundStyle(.secondary).padding()
                } else if let lines {
                    if lines.isEmpty { Text("No lines changed.").font(.silo(.footnote)).foregroundStyle(.secondary).padding() }
                    ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                        DiffRow(line: line)
                    }
                    if truncated { Text("The rest of this diff is not shown.").font(.silo(.footnote)).foregroundStyle(.secondary).padding() }
                } else {
                    ProgressView().padding(40)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .navigationTitle(file.path.split(separator: "/").last.map(String.init) ?? file.path)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !change.pending, changeCanRestore(status: file.status, large: file.large) {
                ToolbarItem(placement: .primaryAction) {
                    Button { confirm = true } label: { Label(changeRestoreLabel(file.status), systemImage: "arrow.uturn.backward") }
                }
            }
        }
        .confirmationDialog(changeRestoreLabel(file.status) + "?", isPresented: $confirm, titleVisibility: .visible) {
            Button(changeRestoreLabel(file.status), role: .destructive) { Task { await restore() } }
        } message: {
            Text("It goes back to how it was before this change. What is there now is recorded first, so this can be undone.")
        }
        .task {
            guard let botID = model.selectedBotID, changeFileNote(file, maxFileBytes: maxFileBytes).isEmpty else { return }
            do {
                let out = try await model.client.getChangePatch(botID: botID, base: change.base, head: change.head, file: file)
                lines = parsePatch(out.patch)
                truncated = out.truncated
            } catch { await model.report(error) }
        }
    }

    private func restore() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let out = try await model.client.restoreChange(botID: botID, base: change.base, head: change.head, file: file)
            restored(changeRestoreNotice(out, path: file.path))
        } catch { await model.report(error) }
    }
}

private struct DiffRow: View {
    let line: DiffLine

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(line.number.map(String.init) ?? "")
                .frame(width: 40, alignment: .trailing)
                .foregroundStyle(.tertiary)
            Text(mark).frame(width: 10).foregroundStyle(markColor)
            Text(line.text.isEmpty ? " " : line.text)
                .foregroundStyle(line.kind == .hunk || line.kind == .note ? .secondary : .primary)
                .fixedSize(horizontal: true, vertical: false)
        }
        .font(.siloMono(.caption))
        .padding(.vertical, 1.5)
        .padding(.trailing, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(background)
        .textSelection(.enabled)
    }

    private var mark: String {
        switch line.kind {
        case .add: return "+"
        case .del: return "−"
        case .hunk: return "@"
        default: return ""
        }
    }

    private var markColor: Color {
        switch line.kind {
        case .add: return Theme.emerald
        case .del: return Theme.vermilion
        default: return .secondary
        }
    }

    private var background: Color {
        switch line.kind {
        case .add: return Theme.emerald.opacity(0.12)
        case .del: return Theme.vermilion.opacity(0.10)
        case .hunk: return Theme.well
        default: return .clear
        }
    }
}

/// What reached the Bot's drives, read off rclone's log. Nothing is kept, so nothing is restored here.
private struct DriveJournal: View {
    @Environment(AppModel.self) private var model
    @Binding var hasDrives: Bool
    @State private var list: Silo_V1_ListDriveChangesResponse?

    var body: some View {
        List {
            if let list {
                if list.state == "off" {
                    Text("Change tracking is turned off by the operator (changes.enabled).").foregroundStyle(.secondary)
                }
                Section {
                    ForEach(list.changes, id: \.id) { entry in
                        VStack(alignment: .leading, spacing: 3) {
                            HStack(alignment: .firstTextBaseline) {
                                Text(entry.path.isEmpty ? entry.drive : "\(entry.drive)/\(entry.path)")
                                    .font(.siloMono(.subheadline))
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                                Spacer(minLength: 6)
                                Text(relativeTime(entry.at)).font(.silo(.caption)).foregroundStyle(.secondary)
                            }
                            Text(driveChangeLine(entry)).font(.silo(.caption)).foregroundStyle(.secondary)
                        }
                    }
                } footer: {
                    Text("Only what changed is recorded, not the files. Use the provider's version history or trash to get something back.")
                }
            }
        }
        .overlay {
            if let list, list.changes.isEmpty {
                ContentUnavailableView("No drive changes", systemImage: "externaldrive", description: Text("Uploads, deletions and renames on the Bot's drives show up here."))
            } else if list == nil {
                ProgressView()
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            list = try await model.client.listDriveChanges(botID: botID)
            hasDrives = list?.hasDrives_p ?? hasDrives
        } catch { await model.report(error) }
    }
}
