import SiloClient
import SwiftUI

extension Silo_V1_Drive: @retroactive Identifiable {}
extension Silo_V1_DriveTemplate: @retroactive Identifiable { public var id: String { key } }

/// Cloud and network storage mounted into the Bot's workspace at `/workspace/drives/<name>`.
struct DrivesPage: View {
    @Environment(AppModel.self) private var model
    @State private var drives: [Silo_V1_Drive] = []
    @State private var templates: [Silo_V1_DriveTemplate] = []
    @State private var unavailable = ""
    @State private var loaded = false
    @State private var adding = false
    @State private var editing: Silo_V1_Drive?
    @State private var deleting: Silo_V1_Drive?

    var body: some View {
        List {
            if !unavailable.isEmpty {
                Section { Text(unavailable).font(.footnote).foregroundStyle(Theme.vermilion) }
            }
            ForEach(drives.filter { !$0.draft }) { drive in
                Button { editing = drive } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(drive.name).font(.headline).foregroundStyle(.primary)
                            Spacer()
                            Text(drive.state.isEmpty ? "—" : drive.state).font(.caption.weight(.medium)).foregroundStyle(stateTone(drive))
                        }
                        Text(templates.first { $0.key == drive.template }?.title ?? drive.template)
                            .font(.caption).foregroundStyle(.secondary)
                        Text("/workspace/drives/\(drive.name)\(drive.readOnly ? " · read-only" : "")")
                            .font(.system(.caption2, design: .monospaced)).foregroundStyle(.tertiary)
                        if !drive.account.isEmpty { Text(drive.account).font(.caption).foregroundStyle(.secondary) }
                        if !drive.stateDetail.isEmpty, drive.state != "mounted" {
                            Text(drive.stateDetail).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                }
                .swipeActions { Button(role: .destructive) { deleting = drive } label: { Label("Remove", systemImage: "trash") } }
            }
        }
        .overlay {
            if loaded && drives.filter({ !$0.draft }).isEmpty && unavailable.isEmpty {
                ContentUnavailableView("No drives", systemImage: "externaldrive", description: Text("Mount Google Drive, S3, WebDAV and more into the Bot's workspace."))
            }
        }
        .toolbar { ToolbarItem(placement: .primaryAction) { Button { adding = true } label: { Label("Add", systemImage: "plus") } } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(isPresented: $adding) {
            DriveTemplatePicker(templates: templates, taken: Set(drives.map(\.name))) { await load() }
        }
        .sheet(item: $editing) { drive in
            if let template = templates.first(where: { $0.key == drive.template }) {
                NavigationStack { DriveForm(template: template, drive: drive, taken: Set(drives.map(\.name))) { await load() } }
            }
        }
        .confirmationDialog("Remove this drive?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { drive in
            Button("Remove \(drive.name)", role: .destructive) { Task { await delete(drive) } }
        } message: { _ in Text("Only the mount is removed; nothing on the remote is deleted.") }
    }

    private func stateTone(_ drive: Silo_V1_Drive) -> Color {
        switch drive.state {
        case "mounted": return Theme.emerald
        case "error": return Theme.vermilion
        default: return .secondary
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            async let list = model.client.listDrives(botID: botID)
            async let kinds = model.client.listDriveTemplates()
            let (result, all) = try await (list, kinds)
            drives = result.drives
            unavailable = result.unavailable
            templates = all
        } catch { await model.report(error) }
        loaded = true
    }

    private func delete(_ drive: Silo_V1_Drive) async {
        do {
            try await model.client.deleteDrive(id: drive.id)
            drives.removeAll { $0.id == drive.id }
        } catch { await model.report(error) }
    }
}

private struct DriveTemplatePicker: View {
    @Environment(\.dismiss) private var dismiss
    let templates: [Silo_V1_DriveTemplate]
    let taken: Set<String>
    let onSaved: () async -> Void

    var body: some View {
        NavigationStack {
            List(templates) { template in
                NavigationLink {
                    DriveForm(template: template, drive: nil, taken: taken) { await onSaved(); dismiss() }
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(template.title).font(.headline)
                        Text(template.blurb).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                        if !template.available {
                            Text("Needs operator setup").font(.caption2).foregroundStyle(Theme.vermilion)
                        }
                    }
                }
                .disabled(!template.available)
            }
            .navigationTitle("Add drive")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
    }
}

/// Add or edit a drive. A server-side draft is made at the first step that needs the server
/// (sign-in, test, browse) so those work before the drive exists; editing writes only on Save.
private struct DriveForm: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    let template: Silo_V1_DriveTemplate
    let drive: Silo_V1_Drive?
    let taken: Set<String>
    let onDone: () async -> Void

    @State private var draft: Silo_V1_Drive?
    @State private var name = ""
    @State private var readOnly = false
    @State private var values: [String: String] = [:]
    @State private var secrets: [String: String] = [:]
    @State private var testState: TestState = .idle
    @State private var authBusy = false
    @State private var failure: String?
    @State private var saving = false
    @State private var pollTask: Task<Void, Never>?

    enum TestState: Equatable { case idle, busy, ok(String), fail(String) }

    private var oauth: Bool { template.authKind == "oauth2" }
    private var editing: Bool { drive != nil }
    private var defaults: [String: String] {
        Dictionary(uniqueKeysWithValues: template.vars.filter { $0.kind == "user" }.map { ($0.key, $0.defaultValue) })
    }
    private var userVars: [Silo_V1_DriveVar] {
        template.vars.filter { $0.kind == "user" && $0.type != "hidden"
            && DriveRules.isVisible(visibleIf: $0.visibleIf, values: values, defaults: defaults) }
    }
    private var connectVars: [Silo_V1_DriveVar] { userVars.filter { !$0.advanced && $0.type != "pick" && $0.type != "folder" } }
    private var mountVars: [Silo_V1_DriveVar] { userVars.filter { !$0.advanced && ($0.type == "pick" || $0.type == "folder") } }
    private var advancedVars: [Silo_V1_DriveVar] { userVars.filter(\.advanced) }
    private var connected: Bool {
        if oauth { return draft?.connected ?? false }
        if editing { return true }
        if case .ok = testState { return true }
        return false
    }
    private var missing: Bool {
        connectVars.contains { $0.required && (values[$0.key] ?? "").isEmpty && (secrets[$0.key] ?? "").isEmpty
            && !(draft?.secretsSet.contains($0.key) ?? false) && $0.defaultValue.isEmpty }
    }
    private var nameOK: Bool { DriveRules.isValidName(name) && (!taken.contains(name) || name == drive?.name) }

    var body: some View {
        Form {
            if !editing, !template.guide.isEmpty {
                Section("Before you add") { ProseView(text: template.guide) }
            }
            Section(oauth ? "Connect \(template.authLabel.isEmpty ? template.title : template.authLabel)" : "Connect") {
                ForEach(connectVars, id: \.key) { varRow($0) }
                if oauth { oauthRow } else { testRow }
            }
            if !mountVars.isEmpty {
                Section("What to mount") { ForEach(mountVars, id: \.key) { varRow($0) } }
            }
            if !advancedVars.isEmpty {
                Section { DisclosureGroup("Advanced") { ForEach(advancedVars, id: \.key) { varRow($0) } } }
            }
            Section {
                TextField("name", text: $name).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .font(.system(.body, design: .monospaced))
                Toggle("Read-only", isOn: $readOnly)
            } header: { Text("Mount") } footer: {
                Text(nameOK ? "/workspace/drives/\(name)" : "Lowercase letters, digits and dashes; unique on this Bot.")
                    .foregroundStyle(nameOK ? Color.secondary : Theme.vermilion)
            }
            if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
        }
        .navigationTitle(editing ? drive!.name : template.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { if editing { Button("Cancel") { dismiss() } } }
            ToolbarItem(placement: .confirmationAction) {
                Button(saving ? "Saving…" : "Save") { Task { await save() } }
                    .disabled(saving || !connected || !nameOK || (!oauth && missing))
            }
        }
        .onAppear(perform: load)
        .onDisappear { pollTask?.cancel() }
    }

    // MARK: rows

    @ViewBuilder
    private func varRow(_ v: Silo_V1_DriveVar) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            switch v.type {
            case "secret":
                SecureField(v.label + (draft?.secretsSet.contains(v.key) == true ? " (set)" : ""), text: Binding(get: { secrets[v.key] ?? "" }, set: { secrets[v.key] = $0 }))
            case "select":
                Picker(v.label, selection: Binding(get: { values[v.key] ?? v.defaultValue }, set: { values[v.key] = $0 })) {
                    ForEach(v.options, id: \.value) { Text($0.label).tag($0.value) }
                }
            case "textarea":
                Text(v.label).font(.caption).foregroundStyle(.secondary)
                TextEditor(text: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 })).frame(minHeight: 80)
                    .font(.system(.footnote, design: .monospaced))
            case "pick":
                PickRow(label: v.label, key: v.key, value: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }), sync: { try await sync() })
            case "folder":
                FolderRow(label: v.label, value: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }), sync: { try await sync() })
            default:
                TextField(v.label + (v.required ? "" : " (optional)"), text: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }), prompt: Text(v.placeholder))
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                    .keyboardType(v.type == "url" ? .URL : v.type == "number" ? .numberPad : .default)
            }
            if !v.help.isEmpty { Text(v.help).font(.caption).foregroundStyle(.secondary) }
        }
    }

    @ViewBuilder
    private var oauthRow: some View {
        if draft?.connected == true {
            Label("Connected\(draft?.account.isEmpty == false ? " as \(draft!.account)" : "")", systemImage: "checkmark.seal.fill")
                .foregroundStyle(Theme.emerald)
        }
        Button(authBusy ? "Waiting for sign-in…" : (draft?.connected == true ? "Sign in again" : "Sign in")) { Task { await signIn() } }
            .disabled(authBusy)
    }

    @ViewBuilder
    private var testRow: some View {
        Button(testState == .busy ? "Testing…" : "Test connection") { Task { await runTest() } }
            .disabled(testState == .busy || missing)
        switch testState {
        case .ok(let message): Label(message, systemImage: "checkmark.circle.fill").foregroundStyle(Theme.emerald).font(.footnote)
        case .fail(let message): Text(message).foregroundStyle(Theme.vermilion).font(.footnote)
        default: EmptyView()
        }
    }

    // MARK: actions

    private func load() {
        guard name.isEmpty else { return }
        draft = drive
        name = drive?.name ?? DriveRules.uniqueName(template.title, taken: taken)
        readOnly = drive?.readOnly ?? false
        values = drive?.options ?? [:]
    }

    private func optionsPayload() -> [String: String] {
        var out: [String: String] = [:]
        for v in template.vars where v.kind == "user" {
            if v.secret || v.type == "secret" {
                if let s = secrets[v.key], !s.isEmpty { out[v.key] = s }
            } else if let value = values[v.key], !value.isEmpty { out[v.key] = value }
        }
        return out
    }

    /// The saved drive when editing, else a server-side draft synced to what is on screen.
    private func sync() async throws -> Silo_V1_Drive {
        if let current = draft, !current.draft { return current }
        guard let botID = model.selectedBotID else { throw SiloError.invalidArgument("No Bot selected") }
        var request = Silo_V1_SaveDriveRequest()
        request.botID = botID
        request.id = draft?.id ?? ""
        request.template = template.key
        request.draft = true
        request.readOnly = readOnly
        request.options = optionsPayload()
        let saved = try await model.client.saveDrive(request)
        draft = saved
        return saved
    }

    private func runTest() async {
        testState = .busy
        do {
            let d = try await sync()
            let dirs = try await model.client.browseDrive(id: d.id, path: "")
            testState = .ok(dirs.isEmpty ? "Connected. The drive is empty at the top level." : "Connected. Found \(dirs.count) folder\(dirs.count == 1 ? "" : "s").")
        } catch {
            testState = .fail((error as? SiloError)?.errorDescription ?? error.localizedDescription)
        }
    }

    private func signIn() async {
        failure = nil
        do {
            let d = try await sync()
            let url = try await model.client.beginDriveAuth(id: d.id)
            guard let target = URL(string: url) else { return }
            authBusy = true
            openURL(target)
            pollTask?.cancel()
            pollTask = Task { await pollAuth() }
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }

    /// The browser finishes the sign-in on the CP; poll the draft until it reports connected.
    private func pollAuth() async {
        guard let botID = model.selectedBotID else { return }
        for _ in 0..<150 where !Task.isCancelled {
            try? await Task.sleep(for: .seconds(2))
            guard let current = draft,
                  let list = try? await model.client.listDrives(botID: botID, draftID: current.draft ? current.id : ""),
                  let fresh = list.drives.first(where: { $0.id == current.id }) else { continue }
            if fresh.connected {
                draft = fresh
                for (key, value) in fresh.options where (values[key] ?? "").isEmpty { values[key] = value }
                authBusy = false
                return
            }
        }
        authBusy = false
    }

    private func save() async {
        guard let botID = model.selectedBotID else { return }
        saving = true
        defer { saving = false }
        var request = Silo_V1_SaveDriveRequest()
        request.botID = botID
        request.id = draft?.id ?? ""
        request.template = template.key
        request.name = name
        request.readOnly = readOnly
        request.draft = false
        request.options = optionsPayload()
        do {
            _ = try await model.client.saveDrive(request)
            await onDone()
            dismiss()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

/// A choice loaded from the provider account (for example a shared drive).
private struct PickRow: View {
    @Environment(AppModel.self) private var model
    let label: String
    let key: String
    @Binding var value: String
    let sync: () async throws -> Silo_V1_Drive
    @State private var options: [Silo_V1_DriveOption] = []
    @State private var loading = false
    @State private var failure: String?

    var body: some View {
        Menu {
            ForEach(options, id: \.value) { option in
                Button(option.label) { value = option.value }
            }
            Button("Load choices…") { Task { await load() } }
        } label: {
            HStack {
                Text(label).foregroundStyle(.primary)
                Spacer()
                if loading { ProgressView().controlSize(.small) }
                Text(options.first { $0.value == value }?.label ?? (value.isEmpty ? "Choose" : value)).foregroundStyle(.secondary)
            }
        }
        .task { await load() }
    }

    private func load() async {
        loading = true
        defer { loading = false }
        do {
            let d = try await sync()
            options = try await model.client.pickDriveOptions(id: d.id, key: key)
        } catch { /* not connected yet; the menu offers Load choices again */ }
    }
}

/// A folder inside the remote, chosen by browsing.
private struct FolderRow: View {
    let label: String
    @Binding var value: String
    let sync: () async throws -> Silo_V1_Drive
    @State private var showing = false

    var body: some View {
        Button { showing = true } label: {
            HStack {
                Text(label).foregroundStyle(.primary)
                Spacer()
                Text(value.isEmpty ? "Top level" : value).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .sheet(isPresented: $showing) {
            NavigationStack { FolderBrowser(path: "", value: $value, sync: sync, close: { showing = false }) }
        }
    }
}

private struct FolderBrowser: View {
    @Environment(AppModel.self) private var model
    let path: String
    @Binding var value: String
    let sync: () async throws -> Silo_V1_Drive
    let close: () -> Void
    @State private var dirs: [Silo_V1_BrowseDriveDir] = []
    @State private var failure: String?
    @State private var loaded = false

    var body: some View {
        List {
            if let failure { Text(failure).foregroundStyle(Theme.vermilion) }
            Button("Use this folder") { value = path; close() }
            ForEach(dirs, id: \.path) { dir in
                NavigationLink(dir.name) { FolderBrowser(path: dir.path, value: $value, sync: sync, close: close) }
            }
        }
        .navigationTitle(path.isEmpty ? "Top level" : (path.split(separator: "/").last.map(String.init) ?? path))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { close() } } }
        .task {
            do {
                let d = try await sync()
                dirs = try await model.client.browseDrive(id: d.id, path: path)
            } catch {
                failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
            }
            loaded = true
        }
    }
}
