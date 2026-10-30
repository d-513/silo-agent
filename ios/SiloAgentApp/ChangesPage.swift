import SiloClient
import SwiftUI

extension Silo_V1_Change: @retroactive Identifiable {}

/// What changed in the Bot's workspace, as diffs, and what changed on its drives, as a journal.
/// A row opens in place into its files, a file into its diff. Files can be put back as they were
/// before a change; the restore is itself a change.
struct ChangesPage: View {
    @Environment(AppModel.self) private var model
    @State private var view = "workspace"
    @State private var hasDrives = false

    var body: some View {
        Group {
            if view == "drives" {
                DriveJournal(hasDrives: $hasDrives)
            } else if model.selectedBot?.machine == "online" {
                WorkspaceChanges()
            } else {
                NeedMachine(what: "Changes", copy: "Start the Bot to see what changed in /workspace.")
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if hasDrives {
                Segmented(options: [("workspace", "Files"), ("drives", "Drives")], selection: $view)
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .padding(.bottom, 4)
                    .background(Theme.canvas)
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
    @State private var open = ""
    @State private var busy = false
    /// What the last restore did, until the reader moves on.
    @State private var done = ""
    @State private var seen = FirstSeen()

    var body: some View {
        PageScroll(spacing: 12) {
            if let list {
                if let notice = changesNotice(list.state) {
                    VStack(alignment: .leading, spacing: 8) {
                        MachineNote(text: notice.text)
                        if notice.reset {
                            NavigationLink(value: BotRoute.page(.container)) {
                                Text("Open Containers").font(.silo(13, .medium)).foregroundStyle(Theme.cobalt).padding(.horizontal, 4)
                            }
                            .buttonStyle(.press)
                        }
                    }
                } else {
                    content(list)
                }
            } else {
                SkeletonRows(count: 3, height: 56)
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
        .sensoryFeedback(.success, trigger: done) { _, new in !new.isEmpty }
    }

    @ViewBuilder
    private func content(_ list: Silo_V1_ListChangesResponse) -> some View {
        if list.indexing { MachineNote(text: "Still reading this workspace for the first time. Changes show once that is done.") }
        if !done.isEmpty { MachineNote(text: done).transition(.rise) }
        let rows = rows(of: list)
        if rows.isEmpty {
            EmptyWell(symbol: "clock.arrow.trianglehead.counterclockwise.rotate.90", title: "Nothing has changed yet",
                      message: "When the Bot, a script it runs, or you change a file in /workspace, the difference shows here.")
        } else {
            PanelSection(footer: footnote(list)) {
                ForEach(rows) { change in
                    let key = change.pending ? "pending" : change.id
                    ChangeRow(change: change, maxFileBytes: list.maxFileBytes, open: open == key, busy: busy) {
                        withAnimation(Motion.fold) {
                            open = open == key ? "" : key
                            done = ""
                        }
                    } restore: { file in
                        Task { await restore(change, file) }
                    }
                    .rise(when: seen.isNew(key))
                }
            }
        }
    }

    /// What is not snapshotted yet comes first, as "Now".
    private func rows(of list: Silo_V1_ListChangesResponse) -> [Silo_V1_Change] {
        (list.hasPending && list.pending.files > 0 ? [list.pending] : []) + list.changes
    }

    private func footnote(_ list: Silo_V1_ListChangesResponse) -> String {
        let since = list.since.isEmpty ? "The workspace is compared before and after each run." : "The workspace is compared before and after each run, since \(shortDay(list.since))."
        return "\(since) Drives, tmp/, bot/ and dependency folders are left out, and a file over \(formatBytes(list.maxFileBytes)) is recorded by its size only, so it cannot be put back. Undoing a change keeps what it replaces: the restore is listed as a change of its own."
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let fresh = try await model.client.listChanges(botID: botID)
            seen.note(rows(of: fresh).map { $0.pending ? "pending" : $0.id })
            withAnimation(list == nil ? nil : Motion.state) { list = fresh }
        } catch { await model.report(error) }
    }

    private func restore(_ change: Silo_V1_Change, _ file: Silo_V1_ChangeFile?) async {
        guard let botID = model.selectedBotID else { return }
        busy = true
        defer { busy = false }
        do {
            let out = try await model.client.restoreChange(botID: botID, base: change.base, head: change.head, file: file)
            withAnimation(Motion.state) { done = changeRestoreNotice(out, path: file?.path ?? "") }
            await load()
        } catch { await model.report(error) }
    }
}

/// Line counts as git writes them. No colour: the diff itself carries that.
private struct ChangeCounts: View {
    let added: Int32
    let deleted: Int32

    var body: some View {
        let parts = [added > 0 ? "+\(added)" : "", deleted > 0 ? "−\(deleted)" : ""].filter { !$0.isEmpty }
        if !parts.isEmpty {
            Text(parts.joined(separator: " ")).font(.siloMono(12)).monospacedDigit().foregroundStyle(Theme.ink3)
        }
    }
}

private func fileCount(_ count: Int32) -> String { count == 1 ? "1 file" : "\(count) files" }

/// One stretch between two snapshots: who was at work, when, how much; it opens into its files.
private struct ChangeRow: View {
    let change: Silo_V1_Change
    let maxFileBytes: Int64
    let open: Bool
    let busy: Bool
    let toggle: () -> Void
    let restore: (Silo_V1_ChangeFile?) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button(action: toggle) {
                HStack(alignment: .top, spacing: 10) {
                    MachineChevron(open: open).padding(.top, 4)
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(changeSourceTitle(change.sources, restore: change.restore))
                                .font(.silo(13.5, .medium))
                                .foregroundStyle(Theme.ink)
                                .lineLimit(1)
                            Spacer(minLength: 0)
                            Text(change.pending ? "Now" : relativeTime(change.at))
                                .font(.siloMono(12))
                                .foregroundStyle(Theme.ink3)
                                .fixedSize()
                        }
                        HStack(spacing: 8) {
                            Text(fileCount(change.files)).font(.silo(12.5)).foregroundStyle(Theme.ink3)
                            ChangeCounts(added: change.added, deleted: change.deleted)
                        }
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .accessibilityValue(open ? "Open" : "Closed")
            if open {
                ChangeBody(change: change, maxFileBytes: maxFileBytes, busy: busy, restore: restore)
                    .transition(.opacity)
            }
        }
    }
}

/// An open change: why it has the names it has, its files, and Undo.
private struct ChangeBody: View {
    @Environment(AppModel.self) private var model
    let change: Silo_V1_Change
    let maxFileBytes: Int64
    let busy: Bool
    let restore: (Silo_V1_ChangeFile?) -> Void
    @State private var files: [Silo_V1_ChangeFile] = []
    @State private var truncated = false
    @State private var loaded = false
    @State private var open = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            let hint = change.pending ? "" : changeSourceHint(change.sources, restore: change.restore)
            if !hint.isEmpty {
                Text(hint).font(.silo(12.5)).foregroundStyle(Theme.ink2).fixedSize(horizontal: false, vertical: true)
            }
            if loaded {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(files, id: \.path) { file in
                        ChangeFileRow(change: change, file: file, maxFileBytes: maxFileBytes, open: open == file.path, busy: busy) {
                            withAnimation(Motion.fold) { open = open == file.path ? "" : file.path }
                        } restore: {
                            restore(file)
                        }
                    }
                }
                if truncated {
                    Text("Only the first \(files.count) files are listed.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                }
            } else {
                SkeletonRows(count: min(max(Int(change.files), 1), 3), height: 28)
            }
            if !change.pending {
                ArmedButton(title: "Undo this change", armedTitle: "Tap again to put \(fileCount(change.files)) back",
                            symbol: "arrow.uturn.backward", small: true) {
                    restore(nil)
                }
                .disabled(busy)
                .padding(.top, 4)
            }
        }
        .padding(.leading, 35)
        .padding(.trailing, 14)
        .padding(.bottom, 14)
        // A change not yet snapshotted is a new pair on every look; the last list stays until the next.
        .task(id: change.base + ".." + change.head) {
            guard let botID = model.selectedBotID else { return }
            do {
                let out = try await model.client.listChangeFiles(botID: botID, base: change.base, head: change.head)
                files = out.files
                truncated = out.truncated
            } catch { await model.report(error) }
            loaded = true
        }
    }
}

/// One file of a change: the path cut at its start, so the file's name stays. It opens into its diff.
private struct ChangeFileRow: View {
    let change: Silo_V1_Change
    let file: Silo_V1_ChangeFile
    let maxFileBytes: Int64
    let open: Bool
    let busy: Bool
    let toggle: () -> Void
    let restore: () -> Void

    private var note: String { changeFileNote(file, maxFileBytes: maxFileBytes) }

    private var statusWord: String? {
        ["added": "Added", "deleted": "Deleted", "renamed": "Renamed", "repo": "Repository"][file.status]
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button(action: toggle) {
                HStack(spacing: 8) {
                    MachineChevron(open: open, size: 9)
                    Text(file.path)
                        .font(.siloMono(12.5))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .truncationMode(.head)
                    if let statusWord {
                        Text(statusWord).font(.silo(12.5)).foregroundStyle(Theme.ink3).fixedSize()
                    }
                    Spacer(minLength: 0)
                    if !note.isEmpty {
                        if file.status != "repo" {
                            Text(changeSizes(old: file.oldSize, new: file.newSize)).font(.siloMono(12)).foregroundStyle(Theme.ink3).fixedSize()
                        }
                    } else {
                        ChangeCounts(added: file.added, deleted: file.deleted).fixedSize()
                    }
                }
                .frame(minHeight: 40)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .accessibilityValue(open ? "Open" : "Closed")
            if open {
                VStack(alignment: .leading, spacing: 8) {
                    if !file.oldPath.isEmpty {
                        Text("Was \(Text(file.oldPath).font(.siloMono(12)))").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                    }
                    if note.isEmpty {
                        FilePatch(change: change, file: file)
                    } else {
                        MachineNote(text: note)
                    }
                    if !change.pending, changeCanRestore(status: file.status, large: file.large) {
                        HStack(spacing: 8) {
                            ArmedButton(title: sentenceCase(changeRestoreLabel(file.status)), armedTitle: "Tap again",
                                        symbol: "arrow.uturn.backward", small: true, action: restore)
                                .disabled(busy)
                            Text("As it was before this change.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                        }
                    }
                }
                .padding(.bottom, 10)
                .transition(.opacity)
            }
        }
    }

    /// The shared labels are title-cased for the old alert; a button here is a sentence.
    private func sentenceCase(_ label: String) -> String {
        label.prefix(1) + label.dropFirst().lowercased()
    }
}

/// One file's diff, numbered: additions `emerald`, deletions `vermilion`, as text only.
private struct FilePatch: View {
    @Environment(AppModel.self) private var model
    let change: Silo_V1_Change
    let file: Silo_V1_ChangeFile
    @State private var lines: [DiffLine]?
    @State private var binary = false
    @State private var truncated = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let lines {
                if lines.isEmpty {
                    MachineNote(text: binary ? "Not text, so there are no lines to compare."
                                : file.status == "renamed" ? "Moved without a change to its lines." : "No lines changed.")
                } else {
                    DiffWell(lines: lines)
                    if truncated { Text("The diff is long; only its start is shown.").font(.silo(12.5)).foregroundStyle(Theme.ink3) }
                }
            } else {
                SkeletonRows(count: 1, height: 64)
            }
        }
        .task(id: change.base + ".." + change.head) {
            guard let botID = model.selectedBotID else { return }
            do {
                let out = try await model.client.getChangePatch(botID: botID, base: change.base, head: change.head, file: file)
                lines = parsePatch(out.patch)
                binary = out.binary
                truncated = out.truncated
            } catch {
                lines = lines ?? []
                await model.report(error)
            }
        }
    }
}

/// A `well` mono block that scrolls on its own, both ways.
private struct DiffWell: View {
    let lines: [DiffLine]

    private var gutter: CGFloat {
        let digits = String(lines.compactMap(\.number).max() ?? 0).count
        return CGFloat(digits) * 7.5 + 4
    }

    var body: some View {
        ScrollView([.horizontal, .vertical]) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                    DiffRow(line: line, gutter: gutter, first: index == 0)
                }
            }
            .padding(.vertical, 8)
            .padding(.trailing, 12)
        }
        .defaultScrollAnchor(.topLeading)
        .scrollBounceBehavior(.basedOnSize, axes: [.horizontal, .vertical])
        .frame(maxWidth: .infinity, maxHeight: 420, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
    }
}

private struct DiffRow: View {
    let line: DiffLine
    let gutter: CGFloat
    let first: Bool

    var body: some View {
        Group {
            if line.kind == .hunk {
                Text(verbatim: "⋯ \(line.text)")
                    .foregroundStyle(Theme.ink3)
                    .padding(.leading, 12)
                    .padding(.top, first ? 0 : 8)
            } else {
                HStack(alignment: .top, spacing: 0) {
                    Text(line.number.map(String.init) ?? "")
                        .monospacedDigit()
                        .foregroundStyle(Theme.ink3)
                        .frame(width: gutter, alignment: .trailing)
                        .padding(.leading, 12)
                        .padding(.trailing, 8)
                    Text(sign).frame(width: 12, alignment: .leading).foregroundStyle(tone)
                    Text(line.text.isEmpty ? " " : line.text).foregroundStyle(tone)
                }
            }
        }
        .font(.siloMono(12))
        .lineLimit(1)
        .fixedSize(horizontal: true, vertical: false)
        .frame(minHeight: 18)
        .textSelection(.enabled)
    }

    private var sign: String {
        switch line.kind {
        case .add: return "+"
        case .del: return "−"
        default: return ""
        }
    }

    private var tone: Color {
        switch line.kind {
        case .add: return Theme.emerald
        case .del: return Theme.vermilion
        case .ctx: return Theme.ink2
        default: return Theme.ink3
        }
    }
}

/// What reached the Bot's drives, read off rclone's log. Nothing is kept, so nothing is restored here.
private struct DriveJournal: View {
    @Environment(AppModel.self) private var model
    @Binding var hasDrives: Bool
    @State private var list: Silo_V1_ListDriveChangesResponse?

    var body: some View {
        PageScroll(spacing: 12) {
            if let list {
                if list.state == "off" {
                    MachineNote(text: changesNotice("off")?.text ?? "")
                }
                if list.changes.isEmpty {
                    EmptyWell(symbol: "externaldrive", title: "Nothing has been changed on a drive yet",
                              message: "A file written, deleted or renamed under /workspace/drives is noted here, whatever did it.")
                } else {
                    PanelSection(footer: footnote(list)) {
                        ForEach(list.changes, id: \.id) { entry in row(entry) }
                    }
                }
            } else {
                SkeletonRows(count: 3, height: 56)
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
    }

    private func row(_ entry: Silo_V1_DriveChangeEntry) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(entry.path.isEmpty ? entry.drive : "\(entry.drive)/\(entry.path)")
                    .font(.siloMono(12.5))
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                    .truncationMode(.head)
                Spacer(minLength: 0)
                Text(relativeTime(entry.at)).font(.siloMono(12)).foregroundStyle(Theme.ink3).fixedSize()
            }
            Text(driveChangeLine(entry))
                .font(.silo(12.5))
                .foregroundStyle(entry.op == "deleted" ? Theme.ink2 : Theme.ink3)
                .lineLimit(2)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }

    private func footnote(_ list: Silo_V1_ListDriveChangesResponse) -> String {
        let record = "A record of what was written, deleted or renamed on a drive, not a copy of it: to get a file back, use the provider's own version history or trash. An upload is noted when it reaches the provider, a few seconds after the file is saved."
        return list.keep > 0 ? "\(record) The newest \(list.keep) lines are kept." : record
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            list = try await model.client.listDriveChanges(botID: botID)
            hasDrives = list?.hasDrives_p ?? hasDrives
        } catch { await model.report(error) }
    }
}
