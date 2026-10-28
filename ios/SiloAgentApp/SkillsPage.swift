import SiloClient
import SwiftUI
import UniformTypeIdentifiers

extension Silo_V1_Skill: @retroactive Identifiable { public var id: String { kind + "/" + name } }

/// The Workspace's skills: its own (installed by URL or zip) and the site-wide Library beside them.
/// A skill opens to its files, Choose Bots (switched on or off per Bot the user manages) and Remove.
struct SpaceSkillsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var scope = "space"
    @State private var lists: [String: [Silo_V1_Skill]] = [:]
    @State private var query = ""
    @State private var installing = false
    @State private var url = ""
    @State private var importing = false
    @State private var busy = false
    @State private var note: String?

    private var all: [Silo_V1_Skill] { lists[scope] ?? [] }
    private var shown: [Silo_V1_Skill] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return all.filter { q.isEmpty || ($0.name + " " + $0.description_p).lowercased().contains(q) }
    }

    var body: some View {
        List {
            Section {
                Picker("Scope", selection: $scope) {
                    Text("Workspace").tag("space")
                    Text("Library").tag("library")
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            } footer: {
                Text(scope == "space"
                     ? "Skills are instructions and scripts a Bot loads when a task calls for them. These belong to this workspace."
                     : "Site-wide skills an admin added. Choose which of your Bots have each one on.")
            }
            if let note { Section { Text(note).font(.silo(14)) } }
            ForEach(shown) { skill in
                NavigationLink {
                    SkillPage(skill: skill, scope: scope) { await load() }
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(skill.name).font(.silo(16, .medium))
                            if skill.seeded { WellChip(text: "catalog", tone: Theme.cobalt) }
                        }
                        if !skill.description_p.isEmpty {
                            Text(skill.description_p).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(2)
                        }
                        UsedByLine(ids: skill.usedBy)
                    }
                    .padding(.vertical, 2)
                }
            }
        }
        .siloList()
        .searchable(text: $query, prompt: "Search skills")
        .overlay {
            if lists[scope] != nil && all.isEmpty {
                if scope == "space" {
                    ContentUnavailableView {
                        Label("No workspace skills yet", systemImage: "book")
                    } description: {
                        Text("Install one to teach this workspace's Bots something new, or see what the Library already offers.")
                    } actions: {
                        Button("Browse the Library") { scope = "library" }.buttonStyle(.quiet)
                    }
                } else {
                    ContentUnavailableView("The Library is empty", systemImage: "book", description: Text("An admin can add site-wide skills on the web client."))
                }
            }
        }
        .overlay { if busy { ProgressView().controlSize(.large) } }
        .toolbar {
            if scope == "space" {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button { installing = true } label: { Label("From URL", systemImage: "link") }
                        Button { importing = true } label: { Label("From Zip", systemImage: "doc.zipper") }
                    } label: { Label("Install", systemImage: "plus") }
                }
            }
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
        .alert("Install from URL", isPresented: $installing) {
            TextField("GitHub URL", text: $url).textInputAutocapitalization(.never).autocorrectionDisabled()
            Button("Cancel", role: .cancel) {}
            Button("Install") { Task { await install(url: url) } }
        } message: { Text("A GitHub repository, owner/repo, or a skill folder.") }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.zip, .archive], allowsMultipleSelection: false) { result in
            guard case .success(let urls) = result, let file = urls.first else { return }
            Task { await installArchive(file) }
        }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do {
            async let own = model.client.listSkills(scope: "space", spaceID: spaceID)
            async let library = model.client.listSkills(scope: "library", spaceID: spaceID)
            let (a, b) = try await (own, library)
            lists = ["space": a, "library": b]
        } catch { await model.report(error) }
    }

    private func install(url: String) async {
        guard let spaceID = spaces.currentID else { return }
        busy = true
        defer { busy = false }
        do { report(try await model.client.installSkill(spaceID: spaceID, url: url.trimmingCharacters(in: .whitespaces))) } catch { await model.report(error) }
        self.url = ""
        await load()
    }

    private func installArchive(_ file: URL) async {
        guard let spaceID = spaces.currentID else { return }
        let scoped = file.startAccessingSecurityScopedResource()
        defer { if scoped { file.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: file) else { return }
        busy = true
        defer { busy = false }
        do { report(try await model.client.installSkill(spaceID: spaceID, archive: data, filename: file.lastPathComponent)) } catch { await model.report(error) }
        await load()
    }

    private func report(_ result: Silo_V1_InstallSkillResponse) {
        var parts: [String] = []
        if !result.installed.isEmpty { parts.append("Installed \(result.installed.joined(separator: ", ")). It is on in every Bot you manage here") }
        if !result.skipped.isEmpty { parts.append("Skipped \(result.skipped.joined(separator: ", "))") }
        note = parts.isEmpty ? nil : parts.joined(separator: ". ") + "."
    }
}

/// One skill: its files, Choose Bots, and Remove for whoever installed it or a Workspace admin.
private struct SkillPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    let scope: String
    let onChange: () async -> Void
    @State private var skill: Silo_V1_Skill
    @State private var removing = false

    init(skill: Silo_V1_Skill, scope: String, onChange: @escaping () async -> Void) {
        _skill = State(initialValue: skill)
        self.scope = scope
        self.onChange = onChange
    }

    private var kind: String { skill.kind.isEmpty ? scope : skill.kind }

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 6) {
                    Text(skill.name).font(.silo(22, .semibold))
                    if !skill.description_p.isEmpty { Text(skill.description_p).font(.silo(14)).foregroundStyle(Theme.ink2) }
                    if !skill.installedBy.isEmpty {
                        Text("Installed by \(skill.installedBy)").font(.silo(12)).foregroundStyle(Theme.ink3)
                    } else if !skill.source.isEmpty {
                        Text(skill.source).font(.siloMono(12)).foregroundStyle(Theme.ink3).lineLimit(2)
                    }
                }
                .padding(.vertical, 4)
                NavigationLink {
                    FileBrowser(source: .skill(client: model.client, scope: kind, name: skill.name, spaceID: spaces.currentID ?? ""))
                        .navigationTitle(skill.name)
                } label: {
                    Label("Files", systemImage: "folder")
                }
            }
            ChooseBotsSection(granted: skill.usedBy) { botID, on in
                do {
                    try await model.client.setBotSkill(botID: botID, kind: kind, name: skill.name, enabled: on)
                } catch { await model.report(error) }
                await reload()
            }
            if kind == "space" && skill.canRemove {
                Section {
                    Button("Remove skill", role: .destructive) { removing = true }
                }
            }
        }
        .siloList()
        .navigationTitle(skill.name)
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("Remove \(skill.name)?", isPresented: $removing, titleVisibility: .visible) {
            Button("Remove", role: .destructive) {
                Task {
                    do {
                        try await model.client.deleteSkill(spaceID: spaces.currentID ?? "", name: skill.name)
                        await onChange()
                        dismiss()
                    } catch { await model.report(error) }
                }
            }
        } message: { Text("Every Bot in the workspace loses it.") }
    }

    private func reload() async {
        await onChange()
        guard let spaceID = spaces.currentID,
              let list = try? await model.client.listSkills(scope: kind, spaceID: spaceID),
              let fresh = list.first(where: { $0.name == skill.name }) else { return }
        skill = fresh
    }
}
