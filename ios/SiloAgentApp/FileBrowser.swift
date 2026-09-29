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

    static func skill(client: SiloClient, scope: String, name: String) -> FsSource {
        FsSource(
            rootLabel: name,
            list: { try await client.listSkillFiles(scope: scope, name: name, path: $0) },
            read: { try await client.readSkillFile(scope: scope, name: name, path: $0) }
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
    @State private var photos: [PhotosPickerItem] = []
    @State private var deleting: Silo_V1_FileEntry?

    private var editable: Bool { source.workspaceBotID != nil }

    var body: some View {
        List {
            if let failure { Text(failure).foregroundStyle(Theme.vermilion) }
            ForEach(entries, id: \.path) { entry in
                if entry.dir {
                    NavigationLink {
                        FileBrowser(source: source, path: entry.path)
                    } label: {
                        Label(entry.name, systemImage: "folder").foregroundStyle(.primary)
                    }
                    .swipeActions { deleteAction(entry) }
                } else {
                    NavigationLink {
                        FilePreviewPage(source: source, path: entry.path, name: entry.name)
                    } label: {
                        HStack {
                            Label(entry.name, systemImage: symbol(entry.name))
                            Spacer()
                            Text(formatBytes(entry.size)).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .swipeActions { deleteAction(entry) }
                }
            }
        }
        .overlay {
            if loaded && entries.isEmpty && failure == nil {
                ContentUnavailableView("Empty", systemImage: "folder", description: Text(editable ? "Upload a file or make a folder." : ""))
            }
        }
        .navigationTitle(path.isEmpty ? source.rootLabel : (path.split(separator: "/").last.map(String.init) ?? path))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if editable {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button { folderName = ""; newFolder = true } label: { Label("New Folder", systemImage: "folder.badge.plus") }
                        Button { importing = true } label: { Label("Upload File", systemImage: "arrow.up.doc") }
                        PhotosPicker(selection: $photos, maxSelectionCount: 5, matching: .images) {
                            Label("Upload Photo", systemImage: "photo")
                        }
                    } label: { Label("Add", systemImage: "plus") }
                }
            }
        }
        .refreshable { await load() }
        .task(id: path + (model.selectedBotID ?? "")) { await load() }
        .alert("New folder", isPresented: $newFolder) {
            TextField("Name", text: $folderName)
            Button("Cancel", role: .cancel) {}
            Button("Create") { Task { await makeFolder() } }
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            Task { for url in urls { await upload(url) } }
        }
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
        .confirmationDialog("Delete this item?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { entry in
            Button("Delete \(entry.name)", role: .destructive) { Task { await remove(entry) } }
        } message: { entry in
            Text(entry.dir ? "The folder and everything in it is removed." : "This cannot be undone.")
        }
    }

    @ViewBuilder
    private func deleteAction(_ entry: Silo_V1_FileEntry) -> some View {
        if editable {
            Button(role: .destructive) { deleting = entry } label: { Label("Delete", systemImage: "trash") }
        }
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
            entries = list.sorted { ($0.dir ? 0 : 1, $0.name.lowercased()) < ($1.dir ? 0 : 1, $1.name.lowercased()) }
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
        do { try await model.client.mkdir(botID: botID, path: child(name)) } catch { await model.report(error) }
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
        do { try await model.client.putFile(botID: botID, path: child(name.replacingOccurrences(of: "/", with: "_")), data: data) } catch { await model.report(error) }
        await load()
    }

    private func remove(_ entry: Silo_V1_FileEntry) async {
        guard let botID = source.workspaceBotID else { return }
        do { try await model.client.removeFile(botID: botID, path: entry.path) } catch { await model.report(error) }
        await load()
    }
}

/// One file: image inline, text in a well, anything else through QuickLook.
struct FilePreviewPage: View {
    let source: FsSource
    let path: String
    let name: String
    @State private var file: Silo_V1_ReadFileResponse?
    @State private var failure: String?
    @State private var preview: URL?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let failure {
                    Text(failure).foregroundStyle(Theme.vermilion)
                } else if let file {
                    if file.truncated { Text("Showing the first 2 MB.").font(.footnote).foregroundStyle(.secondary) }
                    if !file.data.isEmpty, let image = UIImage(data: file.data) {
                        Image(uiImage: image).resizable().scaledToFit().clipShape(RoundedRectangle(cornerRadius: 8))
                    } else if !file.binary {
                        if name.lowercased().hasSuffix(".md") { ProseView(text: file.content) } else { CodeWell(code: file.content, language: nil) }
                    } else {
                        ContentUnavailableView("No preview", systemImage: "doc", description: Text("Open the file to view it."))
                    }
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 60)
                }
            }
            .padding()
        }
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if file != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button { open() } label: { Label("Open", systemImage: "arrow.up.forward.square") }
                }
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
        if model.selectedBot?.workerConnected == true, let botID = model.selectedBotID {
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

    var body: some View {
        ContentUnavailableView {
            Label("\(what) needs the machine", systemImage: "power")
        } description: {
            Text("Start the Bot to browse its workspace.")
        } actions: {
            Button("Start Bot") { Task { await model.startBot() } }
                .buttonStyle(.borderedProminent)
        }
    }
}
