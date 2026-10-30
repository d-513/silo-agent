import PhotosUI
import QuickLook
import SiloClient
import SwiftUI
import UniformTypeIdentifiers

/// Where a `FileBrowser` reads from: the Bot's workspace, a directory under it, or a CP skill dir.
struct FsSource {
    var rootLabel: String
    var list: (String) async throws -> [Silo_V1_FileEntry]
    var read: (String) async throws -> Silo_V1_ReadFileResponse
    /// Workspace sources can also create, delete and upload.
    var workspaceBotID: String?
    var workspaceRoot = ""

    static func workspace(client: SiloClient, botID: String, root: String = "") -> FsSource {
        func abs(_ path: String) -> String { path.isEmpty ? root : (root.isEmpty ? path : root + "/" + path) }
        return FsSource(
            rootLabel: root.split(separator: "/").last.map(String.init) ?? "Workspace",
            list: { path in
                try await client.listFiles(botID: botID, path: abs(path)).map { entry in
                    var copy = entry
                    if !root.isEmpty, entry.path.hasPrefix(root + "/") { copy.path = String(entry.path.dropFirst(root.count + 1)) }
                    return copy
                }
            },
            read: { path in try await client.readFile(botID: botID, path: abs(path)) },
            workspaceBotID: root.isEmpty ? botID : nil,
            workspaceRoot: root
        )
    }

    /// A skill's directory on the CP; a Workspace skill (`scope` "space") names its Workspace.
    static func skill(client: SiloClient, scope: String, name: String, spaceID: String = "") -> FsSource {
        FsSource(
            rootLabel: name,
            list: { try await client.listSkillFiles(scope: scope, name: name, path: $0, spaceID: spaceID) },
            read: { try await client.readSkillFile(scope: scope, name: name, path: $0, spaceID: spaceID) }
        )
    }
}

/// Tree + preview: folders push, files open a preview. Workspace roots add New folder, Upload, Delete.
struct FileBrowser: View {
    @Environment(AppModel.self) private var model
    let source: FsSource
    var path = ""

    @State private var entries: [Silo_V1_FileEntry] = []
    @State private var loaded = false
    @State private var failure: String?
    @State private var newFolder = false
    @State private var folderName = ""
    @State private var importing = false
    @State private var pickingPhotos = false
    @State private var photos: [PhotosPickerItem] = []
    @State private var deleting: Silo_V1_FileEntry?
    @State private var confirmDelete = false
    @State private var seen = FirstSeen()
    /// Counts what changed here, for the haptic.
    @State private var changed = 0

    private var editable: Bool { source.workspaceBotID != nil }

    var body: some View {
        PageScroll(spacing: 12) {
            if let failure { ErrorLine(text: failure) }
            if !loaded {
                SkeletonRows(count: 6)
            } else if entries.isEmpty {
                if failure == nil {
                    EmptyWell(symbol: "folder", title: "Empty", message: editable ? "Upload a file or make a folder." : nil)
                }
            } else {
                Panel {
                    ForEach(entries, id: \.path) { entry in
                        row(entry).rise(when: seen.isNew(entry.path))
                    }
                }
            }
        }
        .pageActions {
            if editable {
                SiloMenu {
                    MenuRow(title: "New folder", symbol: "folder.badge.plus") { folderName = ""; newFolder = true }
                    MenuRow(title: "Upload file", symbol: "arrow.up.doc") { importing = true }
                    MenuRow(title: "Upload photo", symbol: "photo") { pickingPhotos = true }
                } label: {
                    Image(systemName: "plus").font(.system(size: 16)).foregroundStyle(Theme.ink2).frame(width: 40, height: 40)
                }
                .accessibilityLabel("Add")
            }
        }
        .refreshable { await load() }
        .task(id: path + (model.selectedBotID ?? "")) { await load() }
        .promptSlip($newFolder, title: "New folder", prompt: "Name", action: "Create", text: $folderName) {
            Task { await makeFolder() }
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            Task { for url in urls { await upload(url) } }
        }
        .photosPicker(isPresented: $pickingPhotos, selection: $photos, maxSelectionCount: 5, matching: .images)
        .onChange(of: photos) { _, items in
            guard !items.isEmpty else { return }
            photos = []
            Task {
                for item in items {
                    guard let data = try? await item.loadTransferable(type: Data.self) else { continue }
                    let ext = item.supportedContentTypes.first?.preferredFilenameExtension ?? "jpg"
                    await put(name: "photo-\(Int(Date().timeIntervalSince1970)).\(ext)", data: data)
                }
            }
        }
        .confirmSlip($confirmDelete, title: "Delete \(deleting?.name ?? "this item")?",
                     message: deleting?.dir == true ? "The folder and everything in it is removed." : "This cannot be undone.",
                     action: "Delete") {
            if let entry = deleting { Task { await remove(entry) } }
        }
        .sensoryFeedback(.success, trigger: changed)
    }

    /// The link stops short of the row's end: a menu inside a link would open the link.
    private func row(_ entry: Silo_V1_FileEntry) -> some View {
        HStack(spacing: 0) {
            NavigationLink {
                if entry.dir {
                    SiloPage(title: entry.name, subtitle: trail(to: entry)) { FileBrowser(source: source, path: entry.path) }
                } else {
                    SiloPage(title: entry.name, subtitle: trail(to: entry)) { FilePreviewPage(source: source, path: entry.path, name: entry.name) }
                }
            } label: {
                PanelRow(symbol: entry.dir ? "folder" : symbol(entry.name), title: entry.name, chevron: entry.dir, trailing: {
                    if !entry.dir {
                        Text(formatBytes(entry.size)).font(.siloMono(12)).foregroundStyle(Theme.ink3)
                    }
                })
            }
            .buttonStyle(RowPressStyle())
            if editable {
                SiloMenu {
                    MenuRow(title: "Delete", symbol: "trash", destructive: true) {
                        deleting = entry
                        confirmDelete = true
                    }
                } label: {
                    Image(systemName: "ellipsis").font(.system(size: 14)).foregroundStyle(Theme.ink3).frame(width: 40, height: 40)
                }
                .accessibilityLabel("Actions for \(entry.name)")
                .padding(.trailing, 4)
            }
        }
    }

    /// Where a pushed page is, for its head: "Workspace / reports".
    private func trail(to entry: Silo_V1_FileEntry) -> String {
        ([source.rootLabel] + entry.path.split(separator: "/").dropLast().map(String.init)).joined(separator: " / ")
    }

    private func symbol(_ name: String) -> String {
        switch (name as NSString).pathExtension.lowercased() {
        case "png", "jpg", "jpeg", "gif", "webp", "heic": return "photo"
        case "pdf": return "doc.richtext"
        case "csv", "xlsx": return "tablecells"
        case "zip", "tar", "gz": return "doc.zipper"
        case "md", "txt", "json", "yaml", "yml": return "doc.text"
        case "py", "js", "ts", "go", "swift", "sh": return "chevron.left.forwardslash.chevron.right"
        default: return "doc"
        }
    }

    private func load() async {
        do {
            let list = try await source.list(path)
            let sorted = list.sorted { ($0.dir ? 0 : 1, $0.name.lowercased()) < ($1.dir ? 0 : 1, $1.name.lowercased()) }
            seen.note(sorted.map(\.path))
            withAnimation(loaded ? Motion.state : nil) { entries = sorted }
            failure = nil
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
            if let silo = error as? SiloError, case .unauthenticated = silo { await model.report(error) }
        }
        loaded = true
    }

    private func child(_ name: String) -> String { path.isEmpty ? name : path + "/" + name }

    private func makeFolder() async {
        let name = folderName.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: "/", with: "_")
        guard let botID = source.workspaceBotID, !name.isEmpty else { return }
        do {
            try await model.client.mkdir(botID: botID, path: child(name))
            changed += 1
        } catch { await model.report(error) }
        await load()
    }

    private func upload(_ url: URL) async {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url) else { return }
        await put(name: url.lastPathComponent, data: data)
    }

    private func put(name: String, data: Data) async {
        guard let botID = source.workspaceBotID else { return }
        guard data.count <= 50 << 20 else { model.errorMessage = "\(name) is larger than 50 MB"; return }
        do {
            try await model.client.putFile(botID: botID, path: child(name.replacingOccurrences(of: "/", with: "_")), data: data)
            changed += 1
        } catch { await model.report(error) }
        await load()
    }

    private func remove(_ entry: Silo_V1_FileEntry) async {
        guard let botID = source.workspaceBotID else { return }
        do {
            try await model.client.removeFile(botID: botID, path: entry.path)
            changed += 1
        } catch { await model.report(error) }
        await load()
    }
}

/// One file: image inline, text in a well, anything else through QuickLook. Wrapped in a `SiloPage`
/// where it is pushed.
struct FilePreviewPage: View {
    let source: FsSource
    let path: String
    let name: String
    @State private var file: Silo_V1_ReadFileResponse?
    @State private var failure: String?
    @State private var preview: URL?

    var body: some View {
        PageScroll(spacing: 12) {
            if let failure {
                ErrorLine(text: failure)
            } else if let file {
                if file.truncated { Text("Showing the first 2 MB.").font(.silo(12.5)).foregroundStyle(Theme.ink3) }
                if !file.data.isEmpty, let image = UIImage(data: file.data) {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFit()
                        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                } else if !file.binary {
                    if name.lowercased().hasSuffix(".md") { ProseView(text: file.content) } else { CodeWell(code: file.content, language: nil) }
                } else {
                    EmptyWell(symbol: "doc", title: "No preview", message: "Open the file to view it.") {
                        Button("Open") { open() }.buttonStyle(.quiet).controlSize(.small)
                    }
                }
            } else {
                SkeletonRows(count: 5, height: 20)
            }
        }
        .pageActions {
            if file != nil {
                Button { open() } label: { Image(systemName: "arrow.up.forward.square") }
                    .buttonStyle(.glyph)
                    .accessibilityLabel("Open")
            }
        }
        .quickLookPreview($preview)
        .task {
            do { file = try await source.read(path) } catch {
                failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
            }
        }
    }

    private func open() {
        guard let file else { return }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(name)
        guard (try? (file.data.isEmpty ? Data(file.content.utf8) : file.data).write(to: url)) != nil else { return }
        preview = url
    }
}

/// The Files tab: the Bot's workspace, or the offline well when the machine is down.
struct FilesPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if model.selectedBot?.machine == "online", let botID = model.selectedBotID {
            FileBrowser(source: .workspace(client: model.client, botID: botID))
                .id(botID)
        } else {
            NeedMachine(what: "Files")
        }
    }
}

/// Worker-backed pages when the Bot's machine is down.
struct NeedMachine: View {
    @Environment(AppModel.self) private var model
    let what: String
    var copy = "Start the Bot to browse its workspace."

    var body: some View {
        let machine = model.selectedBot?.machine ?? ""
        PageScroll {
            EmptyWell(symbol: "power", title: "\(what) needs the machine", message: copy) {
                Button { Task { await model.startBot() } } label: {
                    HStack(spacing: 8) {
                        if machine == "starting" { Spinner(size: 14, color: Theme.ink3) }
                        // This page only shows with the machine down, whatever the list last said.
                        Text(powerWord(machine: machine == "online" ? "" : machine))
                    }
                }
                .buttonStyle(.ink)
                .disabled(machine == "starting")
            }
        }
    }
}
