import SiloClient
import SwiftUI
import UniformTypeIdentifiers

extension Silo_V1_BotSkill: @retroactive Identifiable { public var id: String { kind + "/" + name } }
extension Silo_V1_Skill: @retroactive Identifiable { public var id: String { kind + "/" + name } }

/// The Bot's skills: a switch per Library / Personal skill (unique name per Bot); tap to inspect.
struct SkillsPage: View {
    @Environment(AppModel.self) private var model
    @State private var skills: [Silo_V1_BotSkill] = []
    @State private var loaded = false
    @State private var inspecting: Silo_V1_BotSkill?

    var body: some View {
        List {
            ForEach([("personal", "Personal"), ("library", "Library")], id: \.0) { kind, title in
                let rows = skills.filter { $0.kind == kind }
                if !rows.isEmpty {
                    Section(title) {
                        ForEach(rows) { skill in
                            HStack {
                                Button { inspecting = skill } label: {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(skill.name).font(.body.weight(.medium)).foregroundStyle(.primary)
                                        if !skill.description_p.isEmpty {
                                            Text(skill.description_p).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                                        }
                                    }
                                }
                                .buttonStyle(.plain)
                                Spacer(minLength: 8)
                                Toggle("Enabled", isOn: Binding(get: { skill.enabled }, set: { value in Task { await set(skill, value) } }))
                                    .labelsHidden()
                            }
                        }
                    }
                }
            }
        }
        .overlay {
            if loaded && skills.isEmpty {
                ContentUnavailableView("No skills", systemImage: "book", description: Text("Install skills from the Skill Hub."))
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(item: $inspecting) { skill in
            NavigationStack {
                FileBrowser(source: .skill(client: model.client, scope: skill.kind, name: skill.name))
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Done") { inspecting = nil } } }
            }
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { skills = try await model.client.listBotSkills(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func set(_ skill: Silo_V1_BotSkill, _ enabled: Bool) async {
        guard let botID = model.selectedBotID else { return }
        if let index = skills.firstIndex(where: { $0.id == skill.id }) { skills[index].enabled = enabled }
        do { try await model.client.setBotSkill(botID: botID, kind: skill.kind, name: skill.name, enabled: enabled) } catch {
            await model.report(error)
            await load()
        }
    }
}

/// Skill Hub: every skill you own (Personal) and the shared Library. Install by URL or zip.
struct SkillHub: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    /// As a tab it has no Done button.
    var embedded = false
    @State private var scope = "personal"
    @State private var skills: [Silo_V1_Skill] = []
    @State private var loaded = false
    @State private var installing = false
    @State private var url = ""
    @State private var importing = false
    @State private var busy = false
    @State private var note: String?
    @State private var inspecting: Silo_V1_Skill?
    @State private var deleting: Silo_V1_Skill?

    var body: some View {
        NavigationStack {
            List {
                Picker("Scope", selection: $scope) {
                    Text("Personal").tag("personal")
                    Text("Library").tag("library")
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                if let note { Section { Text(note).font(.footnote) } }
                ForEach(skills) { skill in
                    Button { inspecting = skill } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            HStack {
                                Text(skill.name).font(.body.weight(.medium)).foregroundStyle(.primary)
                                if skill.seeded {
                                    Text("catalog").font(.caption2).padding(.horizontal, 6).padding(.vertical, 1)
                                        .background(Theme.cobaltPale, in: Capsule()).foregroundStyle(Theme.cobalt)
                                }
                            }
                            if !skill.description_p.isEmpty {
                                Text(skill.description_p).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                            }
                        }
                    }
                    .swipeActions {
                        if scope == "personal" {
                            Button(role: .destructive) { deleting = skill } label: { Label("Delete", systemImage: "trash") }
                        }
                    }
                }
            }
            .overlay { if loaded && skills.isEmpty { ContentUnavailableView("No skills", systemImage: "book") } }
            .navigationTitle("Skill Hub")
            .toolbar {
                if !embedded { ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } } }
                if scope == "personal" {
                    ToolbarItem(placement: .primaryAction) {
                        Menu {
                            Button { installing = true } label: { Label("From URL", systemImage: "link") }
                            Button { importing = true } label: { Label("From Zip", systemImage: "doc.zipper") }
                        } label: { Label("Install", systemImage: "plus") }
                    }
                }
            }
            .refreshable { await load() }
            .task(id: scope) { await load() }
            .alert("Install from URL", isPresented: $installing) {
                TextField("GitHub URL", text: $url).textInputAutocapitalization(.never).autocorrectionDisabled()
                Button("Cancel", role: .cancel) {}
                Button("Install") { Task { await install(url: url) } }
            } message: { Text("A GitHub repository or skill folder.") }
            .fileImporter(isPresented: $importing, allowedContentTypes: [.zip, .archive], allowsMultipleSelection: false) { result in
                guard case .success(let urls) = result, let file = urls.first else { return }
                Task { await installArchive(file) }
            }
            .sheet(item: $inspecting) { skill in
                NavigationStack {
                    FileBrowser(source: .skill(client: model.client, scope: skill.kind.isEmpty ? scope : skill.kind, name: skill.name))
                        .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Done") { inspecting = nil } } }
                }
            }
            .confirmationDialog("Delete this skill?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { skill in
                Button("Delete \(skill.name)", role: .destructive) { Task { await delete(skill) } }
            }
            .overlay { if busy { ProgressView().controlSize(.large) } }
        }
    }

    private func load() async {
        do { skills = try await model.client.listSkills(scope: scope) } catch { await model.report(error) }
        loaded = true
    }

    private func install(url: String) async {
        busy = true
        defer { busy = false }
        do { report(try await model.client.installSkill(scope: "personal", url: url.trimmingCharacters(in: .whitespaces))) } catch { await model.report(error) }
        await load()
    }

    private func installArchive(_ file: URL) async {
        let scoped = file.startAccessingSecurityScopedResource()
        defer { if scoped { file.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: file) else { return }
        busy = true
        defer { busy = false }
        do { report(try await model.client.installSkill(scope: "personal", archive: data, filename: file.lastPathComponent)) } catch { await model.report(error) }
        await load()
    }

    private func report(_ result: Silo_V1_InstallSkillResponse) {
        var parts: [String] = []
        if !result.installed.isEmpty { parts.append("Installed \(result.installed.joined(separator: ", "))") }
        if !result.skipped.isEmpty { parts.append("Skipped \(result.skipped.joined(separator: ", "))") }
        note = parts.joined(separator: ". ")
    }

    private func delete(_ skill: Silo_V1_Skill) async {
        do { try await model.client.deleteSkill(scope: "personal", name: skill.name) } catch { await model.report(error) }
        await load()
    }
}
