import SiloClient
import SwiftUI

extension Silo_V1_Drive: @retroactive Identifiable {}
extension Silo_V1_DriveTemplate: @retroactive Identifiable { public var id: String { key } }

/// The Workspace's drives: cloud and network storage connected once here and mounted at
/// `/workspace/drives/<name>` in the Bots chosen for each. Sign-ins and passwords stay on the CP.
struct SpaceDrivesPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var drives: [Silo_V1_Drive] = []
    @State private var templates: [Silo_V1_DriveTemplate] = []
    @State private var loaded = false
    @State private var adding = false
    @State private var opened: Silo_V1_Drive?
    @State private var editing: Silo_V1_Drive?

    private var shown: [Silo_V1_Drive] { drives.filter { !$0.draft } }

    var body: some View {
        PageScroll {
            Text("Connect cloud storage or a file server once for the whole workspace, then choose which Bots mount it in \(Text("/\u{2060}workspace/\u{2060}drives").font(.siloMono(12.5)).foregroundStyle(Theme.ink)).")
                .font(.silo(13.5))
                .foregroundStyle(Theme.ink2)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 4)
            if !loaded {
                SkeletonRows(count: 2, height: 112)
            } else if shown.isEmpty {
                EmptyWell(symbol: "externaldrive", title: "No drives in this workspace yet", message: "Add one, then choose which Bots mount it.") {
                    Button("Add drive") { adding = true }.buttonStyle(.ink)
                }
            } else {
                VStack(spacing: 10) {
                    ForEach(shown) { drive in
                        DriveCard(drive: drive, template: templates.first { $0.key == drive.template }) {
                            opened = drive
                        } delete: {
                            Task { await delete(drive) }
                        }
                        .transition(.rise)
                    }
                }
                .animation(Motion.arrival, value: shown.map(\.id))
                Text("Deleting a drive unmounts it from every Bot. Nothing on the remote is deleted.")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 4)
            }
        }
        .pageActions {
            Button { adding = true } label: { Image(systemName: "plus") }
                .buttonStyle(.glyph)
                .accessibilityLabel("Add")
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) {
            await load()
            // Poll while a mount is coming up.
            while !Task.isCancelled, drives.contains(where: { $0.state == "mounting" }) {
                try? await Task.sleep(for: .seconds(2))
                await load()
            }
        }
        .navigationDestination(item: $opened) { drive in
            DriveGrantsPage(id: drive.id, initial: drive, template: templates.first { $0.key == drive.template }) {
                await load()
            } edit: {
                editing = drive
            }
        }
        .sensoryFeedback(.impact(weight: .medium), trigger: shown.count) { old, new in new < old }
        .sheet(isPresented: $adding) {
            DriveTemplatePicker(templates: templates, taken: Set(drives.map(\.name))) { await load() }
        }
        .sheet(item: $editing) { drive in
            if let template = templates.first(where: { $0.key == drive.template }) {
                NavigationStack {
                    DriveForm(template: template, drive: drive, taken: Set(drives.filter { $0.id != drive.id }.map(\.name))) { await load() }
                }
                .siloSheet()
            }
        }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do {
            async let list = model.client.listSpaceDrives(spaceID: spaceID)
            async let kinds = model.client.listDriveTemplates()
            let (result, all) = try await (list, kinds)
            drives = result
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

/// A provider's tile. The brands' own marks are SVGs, which iOS cannot draw, so this is the glyph
/// of the kind of storage it is.
private struct DriveMark: View {
    let category: String?
    var size: CGFloat = 40
    var muted = false

    private var glyph: String {
        switch category {
        case "consumer": return "cloud"
        case "self-hosted": return "server.rack"
        case "object-storage": return "shippingbox"
        case "protocol": return "network"
        default: return "externaldrive"
        }
    }

    var body: some View {
        Image(systemName: glyph)
            .font(.system(size: size * 0.46))
            .foregroundStyle(muted ? Theme.ink3 : Theme.ink2)
            .frame(width: size, height: size)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            .accessibilityHidden(true)
    }
}

/// What a drive is and how it stands: the mark, its name with the provider and Read-only as chips,
/// the lamp and word, whose account it is, and the provider's own words while it is not mounted.
private struct DriveSummary: View {
    let drive: Silo_V1_Drive
    let template: Silo_V1_DriveTemplate?

    private var who: String {
        [drive.account, drive.lentBy.isEmpty ? "" : "Signed in by \(drive.lentBy)"].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            DriveMark(category: template?.category)
            VStack(alignment: .leading, spacing: 4) {
                FlowRow(spacing: 6) {
                    Text(drive.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                    Chip(text: template?.title ?? drive.template, tint: Theme.ink3)
                    if drive.readOnly { Chip(text: "Read-only", tint: Theme.ink3) }
                }
                StatusWord(look: driveLook(drive.state))
                if !who.isEmpty {
                    Text(who).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
                if !drive.stateDetail.isEmpty, drive.state != "mounted" {
                    Text(drive.stateDetail).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(2)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// One drive in the list: the card opens it; a drive that needs you wears the ribbon.
private struct DriveCard: View {
    let drive: Silo_V1_Drive
    let template: Silo_V1_DriveTemplate?
    let open: () -> Void
    let delete: () -> Void

    private var used: Bool { !drive.usedBy.isEmpty || drive.usedByHidden > 0 }

    var body: some View {
        PressCard(label: drive.name, ribbon: driveLook(drive.state).lamp == .needsYou, action: open) {
            VStack(alignment: .leading, spacing: 10) {
                DriveSummary(drive: drive, template: template).allowsHitTesting(false)
                Theme.line.frame(height: 1).allowsHitTesting(false)
                HStack(spacing: 8) {
                    UsedByLine(ids: drive.usedBy, hidden: drive.usedByHidden)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .allowsHitTesting(false)
                    if drive.canEdit {
                        ArmedButton(title: "Delete", armedTitle: used ? "Unmount and delete" : "Tap again", symbol: "trash", small: true, action: delete)
                            .layoutPriority(1)
                    }
                }
            }
        }
    }
}

/// A drive: Choose Bots, lending, Reconnect when its sign-in lapsed, and Edit for who added it or
/// an admin.
private struct DriveGrantsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.openURL) private var openURL
    let id: String
    let template: Silo_V1_DriveTemplate?
    let onChange: () async -> Void
    let edit: () -> Void
    @State private var drive: Silo_V1_Drive

    init(id: String, initial: Silo_V1_Drive, template: Silo_V1_DriveTemplate?, onChange: @escaping () async -> Void, edit: @escaping () -> Void) {
        self.id = id
        self.template = template
        self.onChange = onChange
        self.edit = edit
        _drive = State(initialValue: initial)
    }

    var body: some View {
        SiloPage(title: drive.name) {
            PageScroll {
                VStack(alignment: .leading, spacing: 12) {
                    DriveSummary(drive: drive, template: template)
                    HStack(spacing: 4) {
                        let path = drive.path.isEmpty ? "/workspace/drives/\(drive.name)" : drive.path
                        Text(path)
                            .font(.siloMono(12))
                            .foregroundStyle(Theme.ink3)
                            .lineLimit(1)
                            .truncationMode(.middle)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        CopyButton(text: path, size: 13)
                    }
                    .padding(.leading, 10)
                    .padding(.vertical, -2)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                    if drive.state == "needs_auth" && drive.canEdit {
                        Button("Reconnect") { Task { await reconnect() } }
                            .buttonStyle(.ink)
                            .controlSize(.small)
                    }
                }
                .folio(ribbon: driveLook(drive.state).lamp == .needsYou)
                if drive.canEdit {
                    Panel {
                        PanelRow(symbol: "pencil", title: "Edit drive", chevron: true, action: edit)
                    }
                }
                LendSection(lentBy: drive.lentBy, shared: drive.shared) { shared in
                    await act { try await model.client.setLendShared(driveID: drive.id, shared: shared) }
                }
                ChooseBotsSection(granted: drive.usedBy) { botID, on in
                    await act {
                        if on { try await model.client.grantDrive(botID: botID, driveID: drive.id) } else {
                            try await model.client.revokeDrive(botID: botID, driveID: drive.id)
                        }
                    }
                }
            }
            .refreshable { await reload() }
        }
    }

    private func reconnect() async {
        do {
            let url = try await model.client.beginDriveAuth(id: drive.id, shared: drive.shared)
            if let target = URL(string: url) { openURL(target) }
        } catch { await model.report(error) }
    }

    private func act(_ work: () async throws -> Void) async {
        do { try await work() } catch { await model.report(error) }
        await reload()
        await onChange()
    }

    private func reload() async {
        guard let spaceID = spaces.currentID,
              let list = try? await model.client.listSpaceDrives(spaceID: spaceID),
              let fresh = list.first(where: { $0.id == id }) else { return }
        drive = fresh
    }
}

/// The providers, by category. One an admin has not set up stays in view, quiet, and does not open.
private struct DriveTemplatePicker: View {
    @Environment(\.dismiss) private var dismiss
    let templates: [Silo_V1_DriveTemplate]
    let taken: Set<String>
    let onSaved: () async -> Void
    @State private var query = ""

    private static let categories = [
        ("consumer", "Personal cloud"), ("self-hosted", "Self-hosted"), ("object-storage", "Object storage"), ("protocol", "File servers"),
    ]

    private var shown: [Silo_V1_DriveTemplate] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return templates.filter { q.isEmpty || ($0.title + " " + $0.blurb).lowercased().contains(q) }
    }

    /// Each category with anything in it; a category this build has no name for goes last.
    private var groups: [(title: String, items: [Silo_V1_DriveTemplate])] {
        let known = Set(Self.categories.map(\.0))
        let named = Self.categories.map { key, title in (title: title, items: shown.filter { $0.category == key }) }
        return (named + [(title: "Other", items: shown.filter { !known.contains($0.category) })]).filter { !$0.items.isEmpty }
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                SheetHead(title: "Add drive", onCancel: { dismiss() })
                PageScroll {
                    SearchBox(prompt: "Search providers", text: $query) {}
                    if groups.isEmpty {
                        EmptyWell(title: "Nothing matches “\(query)”.")
                    }
                    ForEach(groups, id: \.title) { group in
                        PanelSection(title: group.title) {
                            ForEach(group.items) { template in
                                if template.available {
                                    NavigationLink {
                                        DriveForm(template: template, drive: nil, taken: taken) { await onSaved(); dismiss() }
                                    } label: {
                                        row(template)
                                    }
                                    .buttonStyle(RowPressStyle())
                                } else {
                                    row(template)
                                }
                            }
                        }
                    }
                }
            }
            .background(Theme.surface.ignoresSafeArea())
            .toolbar(.hidden, for: .navigationBar)
        }
        .siloSheet()
    }

    private func row(_ template: Silo_V1_DriveTemplate) -> some View {
        HStack(spacing: 12) {
            DriveMark(category: template.category, muted: !template.available)
            VStack(alignment: .leading, spacing: 1) {
                Text(template.title)
                    .font(.silo(14.5, .medium))
                    .foregroundStyle(template.available ? Theme.ink : Theme.ink3)
                    .lineLimit(1)
                Text(template.available ? template.blurb : "Needs admin setup")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if template.available {
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }
}

/// Add or edit a drive, as numbered steps: connect, choose what to mount, name and access. A
/// server-side draft is made at the first step that needs the server (sign-in, test, browse) so
/// those work before the drive exists; editing writes only on Save.
private struct DriveForm: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
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
    @State private var advanced = false
    /// A new drive's Bots: every Bot the user manages, unless they untick some.
    @State private var picked: Set<String> = []
    /// Who may use the sign-in: personal (only Bots the signer alone uses) or the Workspace's.
    @State private var sharedSignIn = false

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
    private var solo: Bool { (spaces.current?.people ?? 1) <= 1 }
    private var provider: String { template.authLabel.isEmpty ? template.title : template.authLabel }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: editing ? drive!.name : template.title, cancel: editing ? "Cancel" : "Back", onCancel: { dismiss() }) {
                Button(saving ? "Saving…" : "Save") { Task { await save() } }
                    .buttonStyle(.ink)
                    .disabled(saving || !connected || !nameOK || (!oauth && missing))
            }
            PageScroll {
                if !editing, !template.guide.isEmpty {
                    GuideWell(text: template.guide)
                }
                StepCard(number: 1, title: oauth ? "Connect \(provider)" : "Connect",
                         note: oauth ? "Sign in with the account whose files the Bot should see." : "Where the files are, and how to sign in.",
                         done: connected) {
                    VStack(alignment: .leading, spacing: 14) {
                        ForEach(connectVars, id: \.key) { varRow($0) }
                        if oauth && !solo && draft?.connected != true {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("Who can use this sign-in").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                                ChipRadio(options: [(value: false, label: "Only my Bots"),
                                                    (value: true, label: "Anyone in \(spaces.current?.name ?? "the workspace")")],
                                          selection: $sharedSignIn)
                            }
                        }
                        if oauth { oauthRow } else { testRow }
                    }
                }
                if !mountVars.isEmpty {
                    StepCard(number: 2, title: "Choose what to mount", locked: !connected) {
                        VStack(alignment: .leading, spacing: 14) {
                            ForEach(mountVars, id: \.key) { varRow($0) }
                        }
                    }
                }
                StepCard(number: mountVars.isEmpty ? 2 : 3, title: "Name and access", locked: !connected) {
                    VStack(alignment: .leading, spacing: 14) {
                        VStack(alignment: .leading, spacing: 6) {
                            SiloField(label: "Name", prompt: "name", text: $name, mono: true)
                                .textInputAutocapitalization(.never)
                                .autocorrectionDisabled()
                            Text(nameOK ? "/workspace/drives/\(name)" : "Lowercase letters, digits and dashes; unique in this workspace.")
                                .font(nameOK ? .siloMono(12) : .silo(12))
                                .foregroundStyle(nameOK ? Theme.ink3 : Theme.vermilion)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        Toggle("Read-only", isOn: $readOnly)
                        if !advancedVars.isEmpty { advancedFold }
                    }
                }
                if !editing { BotsStep(picked: $picked) }
                if let failure { ErrorLine(text: failure) }
            }
        }
        .background(Theme.surface.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
        .sensoryFeedback(.success, trigger: connected) { _, now in now }
        .onAppear(perform: load)
        .onDisappear { pollTask?.cancel() }
    }

    private var advancedFold: some View {
        VStack(alignment: .leading, spacing: 14) {
            Button {
                withAnimation(Motion.fold) { advanced.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 11, weight: .semibold))
                        .rotationEffect(.degrees(advanced ? 90 : 0))
                    Text("Advanced settings").font(.silo(13.5, .medium))
                }
                .foregroundStyle(Theme.ink2)
                .frame(minHeight: 32)
                .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            if advanced {
                VStack(alignment: .leading, spacing: 14) {
                    ForEach(advancedVars, id: \.key) { varRow($0) }
                }
                .transition(.opacity)
            }
        }
    }

    // MARK: rows

    @ViewBuilder
    private func varRow(_ v: Silo_V1_DriveVar) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            switch v.type {
            case "secret":
                SiloField(label: v.label + (draft?.secretsSet.contains(v.key) == true ? " (set)" : ""),
                          text: Binding(get: { secrets[v.key] ?? "" }, set: { secrets[v.key] = $0 }), secure: true)
            case "select":
                SiloPicker(label: v.label, options: v.options.map { (value: $0.value, label: $0.label) },
                           selection: Binding(get: { values[v.key] ?? v.defaultValue }, set: { values[v.key] = $0 }))
            case "textarea":
                Text(v.label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                TextEditor(text: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }))
                    .font(.siloMono(13))
                    .foregroundStyle(Theme.ink)
                    .tint(Theme.cobalt)
                    .scrollContentBackground(.hidden)
                    .padding(6)
                    .frame(minHeight: 92)
                    .siloInputFrame()
            case "pick":
                PickRow(label: v.label, key: v.key, value: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }), sync: { try await sync() })
            case "folder":
                FolderRow(label: v.label, value: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }), sync: { try await sync() })
            default:
                SiloField(label: v.label + (v.required ? "" : " (optional)"), prompt: v.placeholder,
                          text: Binding(get: { values[v.key] ?? "" }, set: { values[v.key] = $0 }))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(v.type == "url" ? .URL : v.type == "number" ? .numberPad : .default)
            }
            if !v.help.isEmpty {
                Text(v.help).font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    @ViewBuilder
    private var oauthRow: some View {
        if draft?.connected == true {
            HStack(spacing: 7) {
                Image(systemName: "checkmark.circle.fill").font(.system(size: 14))
                Text("Connected\(draft?.account.isEmpty == false ? " as \(draft!.account)" : "")").font(.silo(13.5, .medium))
            }
            .foregroundStyle(Theme.emerald)
            .transition(.rise)
            Button { Task { await signIn() } } label: {
                signInLabel("Change account")
            }
            .buttonStyle(.quiet)
            .controlSize(.small)
            .disabled(authBusy)
        } else {
            Button { Task { await signIn() } } label: {
                signInLabel(template.authLabel.isEmpty ? "Sign in" : "Sign in with \(template.authLabel)")
            }
            .buttonStyle(.ink)
            .disabled(authBusy)
        }
    }

    private func signInLabel(_ title: String) -> some View {
        HStack(spacing: 7) {
            if authBusy { Spinner(size: 13) }
            Text(authBusy ? "Waiting for sign-in…" : title)
        }
    }

    @ViewBuilder
    private var testRow: some View {
        Button { Task { await runTest() } } label: {
            HStack(spacing: 7) {
                if testState == .busy { Spinner(size: 13) }
                Text(testState == .busy ? "Testing…" : "Test connection")
            }
        }
        .buttonStyle(.quiet)
        .controlSize(.small)
        .disabled(testState == .busy || missing)
        switch testState {
        case .ok(let message):
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Image(systemName: "checkmark.circle.fill").font(.system(size: 13))
                Text(message).font(.silo(13, .medium)).fixedSize(horizontal: false, vertical: true)
            }
            .foregroundStyle(Theme.emerald)
            .transition(.rise)
        case .fail(let message):
            Text(message)
                .font(.silo(13))
                .foregroundStyle(Theme.vermilion)
                .fixedSize(horizontal: false, vertical: true)
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.vermilionPale, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                .transition(.rise)
        default:
            EmptyView()
        }
    }

    // MARK: actions

    private func load() {
        guard name.isEmpty else { return }
        picked = Set(spaces.managedBots.map(\.id))
        draft = drive
        name = drive?.name ?? DriveRules.uniqueName(template.title, taken: taken)
        readOnly = drive?.readOnly ?? false
        sharedSignIn = drive?.shared ?? false
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
        guard let spaceID = spaces.currentID else { throw SiloError.invalidArgument("No workspace is open.") }
        var request = Silo_V1_SaveDriveRequest()
        request.spaceID = spaceID
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
            let url = try await model.client.beginDriveAuth(id: d.id, shared: sharedSignIn)
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
        guard let spaceID = spaces.currentID else { return }
        for _ in 0..<150 where !Task.isCancelled {
            try? await Task.sleep(for: .seconds(2))
            guard let current = draft,
                  let list = try? await model.client.listSpaceDrives(spaceID: spaceID, draftID: current.draft ? current.id : ""),
                  let fresh = list.first(where: { $0.id == current.id }) else { continue }
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
        guard let spaceID = spaces.currentID else { return }
        saving = true
        defer { saving = false }
        var request = Silo_V1_SaveDriveRequest()
        request.spaceID = spaceID
        if !editing { request.botIds = Array(picked) }
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

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            SiloMenu {
                ForEach(options, id: \.value) { option in
                    MenuRow(title: option.label, checked: option.value == value) { value = option.value }
                }
                if !options.isEmpty { MenuDivider() }
                MenuRow(title: "Load choices…", symbol: "arrow.clockwise") { Task { await load() } }
            } label: {
                HStack(spacing: 8) {
                    Text(options.first { $0.value == value }?.label ?? (value.isEmpty ? "Choose" : value))
                        .font(.silo(15))
                        .foregroundStyle(value.isEmpty ? Theme.ink3 : Theme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if loading { Spinner(size: 14) }
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.ink3)
                }
                .padding(.horizontal, 12)
                .frame(height: 44)
                .siloInputFrame()
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
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            Button { showing = true } label: {
                HStack(spacing: 8) {
                    Image(systemName: "folder").font(.system(size: 14)).foregroundStyle(Theme.ink2)
                    Text(value.isEmpty ? "Top level" : value)
                        .font(value.isEmpty ? .silo(15) : .siloMono(13.5))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .truncationMode(.head)
                    Spacer(minLength: 4)
                    Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
                }
                .padding(.horizontal, 12)
                .frame(height: 44)
                .siloInputFrame()
            }
            .buttonStyle(.press)
        }
        .sheet(isPresented: $showing) {
            NavigationStack { FolderBrowser(path: "", value: $value, sync: sync, close: { showing = false }) }
                .siloSheet()
        }
    }
}

/// One level of the remote at a time; Use this folder takes the level you are on.
private struct FolderBrowser: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let path: String
    @Binding var value: String
    let sync: () async throws -> Silo_V1_Drive
    let close: () -> Void
    @State private var dirs: [Silo_V1_BrowseDriveDir] = []
    @State private var failure: String?
    @State private var loaded = false

    private var title: String { path.isEmpty ? "Top level" : (path.split(separator: "/").last.map(String.init) ?? path) }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: title, cancel: path.isEmpty ? "Cancel" : "Back", onCancel: { if path.isEmpty { close() } else { dismiss() } }) {
                Button("Use this folder") { value = path; close() }.buttonStyle(.ink)
            }
            PageScroll {
                if !path.isEmpty {
                    Text("/" + path)
                        .font(.siloMono(12))
                        .foregroundStyle(Theme.ink3)
                        .lineLimit(1)
                        .truncationMode(.head)
                        .padding(.horizontal, 4)
                }
                if let failure { ErrorLine(text: failure) }
                if !loaded {
                    SkeletonRows(count: 5, height: 44)
                } else if dirs.isEmpty, failure == nil {
                    EmptyWell(symbol: "folder", title: "No folders here")
                } else if !dirs.isEmpty {
                    Panel {
                        ForEach(dirs, id: \.path) { dir in
                            NavigationLink {
                                FolderBrowser(path: dir.path, value: $value, sync: sync, close: close)
                            } label: {
                                PanelRow(symbol: "folder", title: dir.name, chevron: true)
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
