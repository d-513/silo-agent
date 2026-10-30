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
        PageScroll {
            Segmented(options: [(value: "space", label: "Workspace"), (value: "library", label: "Library")], selection: $scope)
            Text(scope == "space"
                 ? "Skills are instructions and scripts a Bot loads when a task calls for them. These belong to this workspace."
                 : "Site-wide skills an admin added. Choose which of your Bots have each one on.")
                .font(.silo(13.5))
                .foregroundStyle(Theme.ink2)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 4)
            if let note {
                Text(note)
                    .font(.silo(13.5))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .siloCard()
                    .transition(.rise)
            }
            if lists[scope] == nil {
                SkeletonRows(count: 3, height: 112)
            } else if all.isEmpty {
                if scope == "space" {
                    EmptyWell(symbol: "scroll", title: "No workspace skills yet", message: "Install one to teach this workspace's Bots something new, or see what the Library already offers.") {
                        Button("Browse the Library") { scope = "library" }.buttonStyle(.quiet)
                    }
                } else {
                    EmptyWell(symbol: "scroll", title: "The Library is empty", message: "An admin can add site-wide skills on the web client.")
                }
            } else {
                SearchBox(prompt: "Search skills", text: $query) {}
                if shown.isEmpty {
                    EmptyWell(title: "Nothing matches “\(query)”.")
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 300), spacing: 12, alignment: .top)], spacing: 12) {
                    ForEach(shown) { skill in
                        NavigationLink {
                            SkillPage(skill: skill, scope: scope) { await load() }
                        } label: {
                            SkillCard(skill: skill)
                        }
                        .buttonStyle(.press)
                        .transition(.rise)
                    }
                }
                .animation(Motion.arrival, value: shown.map(\.id))
            }
        }
        .animation(Motion.state, value: note)
        .pageActions {
            if busy {
                Spinner().frame(width: 40, height: 40)
            } else if scope == "space" {
                SiloMenu {
                    MenuRow(title: "From URL", symbol: "link") { installing = true }
                    MenuRow(title: "From Zip", symbol: "doc.zipper") { importing = true }
                } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 16))
                        .foregroundStyle(Theme.ink2)
                        .frame(width: 40, height: 40)
                }
                .accessibilityLabel("Install")
            }
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
        .sensoryFeedback(.success, trigger: note) { _, new in new != nil }
        .promptSlip($installing, title: "Install from URL", prompt: "GitHub URL or owner/repo", action: "Install", text: $url) {
            Task { await install(url: url) }
        }
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
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

/// A skill's source on a card: a GitHub URL shrinks to owner/repo, any other URL loses its scheme,
/// and anything else is shown as it is.
private func skillSource(_ source: String) -> String {
    let bare = source.trimmingCharacters(in: .whitespacesAndNewlines)
        .replacingOccurrences(of: "^https?://", with: "", options: [.regularExpression, .caseInsensitive])
    let host = bare.replacingOccurrences(of: "^www\\.", with: "", options: [.regularExpression, .caseInsensitive])
    guard host.lowercased().hasPrefix("github.com/") else { return bare }
    let parts = host.split(separator: "/").dropFirst().prefix(2).map(String.init)
    guard parts.count == 2, let repo = parts[1].split(whereSeparator: { "#?".contains($0) }).first else { return bare }
    return parts[0] + "/" + (repo.hasSuffix(".git") ? String(repo.dropLast(4)) : String(repo))
}

/// One skill: a scroll mark, its name and two lines, then where it came from and who has it on.
private struct SkillCard: View {
    let skill: Silo_V1_Skill

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 14) {
                Image(systemName: "scroll")
                    .font(.system(size: 20))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 44, height: 44)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                VStack(alignment: .leading, spacing: 4) {
                    FlowRow(spacing: 6) {
                        Text(skill.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        if skill.seeded { Chip(text: "Catalog", tint: Theme.ink3) }
                    }
                    Text(skill.description_p.isEmpty ? "No description." : skill.description_p)
                        .font(.silo(12.5))
                        .foregroundStyle(skill.description_p.isEmpty ? Theme.ink3 : Theme.ink2)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            Theme.line.frame(height: 1).padding(.top, 12)
            HStack(spacing: 10) {
                Text(skill.source.isEmpty ? "Local" : skillSource(skill.source))
                    .font(.siloMono(11))
                    .foregroundStyle(Theme.ink3)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                UsedByLine(ids: skill.usedBy)
            }
            .padding(.top, 10)
        }
        .folio()
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

    init(skill: Silo_V1_Skill, scope: String, onChange: @escaping () async -> Void) {
        _skill = State(initialValue: skill)
        self.scope = scope
        self.onChange = onChange
    }

    private var kind: String { skill.kind.isEmpty ? scope : skill.kind }

    var body: some View {
        SiloPage(title: skill.name) {
            PageScroll {
                VStack(alignment: .leading, spacing: 6) {
                    Text(skill.name).font(.silo(22, .medium)).foregroundStyle(Theme.ink)
                    if !skill.description_p.isEmpty {
                        Text(skill.description_p)
                            .font(.silo(14))
                            .foregroundStyle(Theme.ink2)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if !skill.installedBy.isEmpty {
                        Text("Installed by \(skill.installedBy)").font(.silo(12)).foregroundStyle(Theme.ink3)
                    } else if !skill.source.isEmpty {
                        Text(skill.source).font(.siloMono(12)).foregroundStyle(Theme.ink3).lineLimit(2)
                    }
                }
                .folio()
                Panel {
                    NavigationLink {
                        SiloPage(title: skill.name) {
                            FileBrowser(source: .skill(client: model.client, scope: kind, name: skill.name, spaceID: spaces.currentID ?? ""))
                        }
                    } label: {
                        PanelRow(symbol: "folder", title: "Files", chevron: true)
                    }
                    .buttonStyle(RowPressStyle())
                }
                ChooseBotsSection(granted: skill.usedBy) { botID, on in
                    do {
                        try await model.client.setBotSkill(botID: botID, kind: kind, name: skill.name, enabled: on)
                    } catch { await model.report(error) }
                    await reload()
                }
                if kind == "space" && skill.canRemove {
                    PanelSection(footer: "Every Bot in the workspace loses it.") {
                        ArmedButton(title: "Remove skill", armedTitle: "Tap again to remove \(skill.name)", wide: true) {
                            Task {
                                do {
                                    try await model.client.deleteSkill(spaceID: spaces.currentID ?? "", name: skill.name)
                                    await onChange()
                                    dismiss()
                                } catch { await model.report(error) }
                            }
                        }
                        .padding(6)
                    }
                }
            }
        }
    }

    private func reload() async {
        await onChange()
        guard let spaceID = spaces.currentID,
              let list = try? await model.client.listSkills(scope: kind, spaceID: spaceID),
              let fresh = list.first(where: { $0.name == skill.name }) else { return }
        skill = fresh
    }
}
