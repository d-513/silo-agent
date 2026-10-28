import Observation
import SiloClient
import SwiftProtobuf
import SwiftUI

/// A new Bot in the current Workspace: its crest, name and description, the short interview, what
/// of the Workspace it can use (all ticked, but nobody else's personal sign-in) and who else uses it.
struct NewBotView: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss

    @State private var name = ""
    @State private var description = ""
    @State private var crest = packCrest(
        shape: Int.random(in: 0..<SHAPE_COUNT),
        color: Int.random(in: 0..<CREST_COLORS.count)
    )
    @State private var interview = true
    @State private var connectors: [Silo_V1_Connector] = []
    @State private var drives: [Silo_V1_Drive] = []
    @State private var skills: [Silo_V1_Skill] = []
    @State private var people: [Silo_V1_SpacePerson] = []
    /// What is unticked: "c:<id>", "d:<id>", "s:<name>".
    @State private var off: Set<String> = []
    @State private var users: Set<String> = []
    @State private var busy = false
    @State private var error: String?

    private var me: String { model.user?.email ?? "" }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack {
                        Spacer()
                        CrestView(index: crest, size: 84)
                        Spacer()
                    }
                    .listRowBackground(Color.clear)
                    CrestPicker(value: $crest)
                }

                Section("Name") {
                    TextField("Scout", text: $name)
                }

                Section {
                    TextField("What this machine is for", text: $description, axis: .vertical)
                        .lineLimit(2...5)
                } header: { Text("Description") } footer: {
                    Text("A Bot is its own machine. It does not share files with the others.")
                }

                Section {
                    Toggle("Start with a short interview", isOn: $interview)
                } footer: {
                    Text("The Bot asks what you want help with, offers what to connect, and how proactive to be. You can skip any step, or run /onboard later.")
                }

                if connectors.count + drives.count + skills.count > 0 {
                    Section {
                        ForEach(connectors) { c in
                            pick("c:\(c.id)", c.name, kind: "Connector", why: blocked(c.lentBy, c.shared))
                        }
                        ForEach(drives) { d in
                            pick("d:\(d.id)", d.name, kind: "Drive", why: blocked(d.lentBy, d.shared))
                        }
                        ForEach(skills) { k in
                            pick("s:\(k.name)", k.name, kind: "Skill", why: "")
                        }
                    } header: { Text("What it can use") } footer: {
                        Text("The workspace's connectors, drives and skills. Untick what this Bot should not have; you can change it later on the Workspace page.")
                    }
                }

                if !people.isEmpty {
                    Section {
                        ForEach(people, id: \.userID) { person in
                            Toggle(isOn: Binding(get: { users.contains(person.userID) }, set: { on in
                                if on { users.insert(person.userID) } else { users.remove(person.userID) }
                            })) {
                                HStack(spacing: 10) {
                                    PersonCircle(email: person.email, size: 22)
                                    Text(person.email).font(.silo(15)).lineLimit(1).truncationMode(.middle)
                                }
                            }
                        }
                    } header: { Text("Who can use it") } footer: {
                        Text("Just you, unless you pick people. Everyone on a Bot shares what it holds: its memory, files and connections.")
                    }
                }

                if let error {
                    Section {
                        Text(error).font(.silo(.footnote)).foregroundStyle(Theme.vermilion)
                    }
                }
            }
            .navigationTitle(spaces.current.map { "New Bot in \($0.name)" } ?? "New Bot")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if busy {
                        ProgressView()
                    } else {
                        Button("Create") { create() }
                            .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                }
            }
            .task { await load() }
        }
    }

    /// One thing the new Bot may get: on, off, or unavailable with why.
    private func pick(_ key: String, _ title: String, kind: String, why: String) -> some View {
        Toggle(isOn: Binding(get: { why.isEmpty && !off.contains(key) }, set: { on in
            if on { off.remove(key) } else { off.insert(key) }
        })) {
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(.silo(15)).strikethrough(!why.isEmpty).foregroundStyle(why.isEmpty ? Theme.ink : Theme.ink3)
                Text(why.isEmpty ? kind : why).font(.silo(12)).foregroundStyle(Theme.ink3)
            }
        }
        .disabled(!why.isEmpty)
    }

    private func blocked(_ lentBy: String, _ shared: Bool) -> String {
        lendBlocked(lentBy: lentBy, shared: shared, me: me, othersPicked: !users.isEmpty)
    }

    private func takes(_ key: String, _ why: String) -> Bool { why.isEmpty && !off.contains(key) }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        if canBuildSpace(spaces.role) {
            async let c = try? model.client.listSpaceConnectors(spaceID: spaceID)
            async let d = try? model.client.listSpaceDrives(spaceID: spaceID)
            async let s = try? model.client.listSkills(scope: "space", spaceID: spaceID)
            let (cs, ds, ss) = await (c, d, s)
            connectors = cs ?? []
            drives = (ds ?? []).filter { !$0.draft }
            skills = ss ?? []
        }
        people = ((try? await model.client.listSpacePeople(spaceID: spaceID))?.people ?? []).filter { !$0.you }
    }

    private func create() {
        let trimmed = name.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return }
        busy = true
        error = nil
        let request = Silo_V1_CreateBotRequest.with {
            $0.spaceID = spaces.currentID ?? ""
            $0.name = trimmed
            $0.crest = crest
            $0.description_p = description.trimmingCharacters(in: .whitespacesAndNewlines)
            $0.interview = interview
            $0.connectorIds = connectors.filter { takes("c:\($0.id)", blocked($0.lentBy, $0.shared)) }.map(\.id)
            $0.driveIds = drives.filter { takes("d:\($0.id)", blocked($0.lentBy, $0.shared)) }.map(\.id)
            $0.skillsOff = skills.filter { off.contains("s:\($0.name)") }.map(\.name)
            $0.userIds = Array(users)
        }
        Task {
            do {
                let bot = try await model.client.createBot(request)
                await model.refreshBots()
                spaces.follow(bot)
                model.selectedBotID = bot.id
                dismiss()
            } catch {
                self.error = (error as? SiloError)?.errorDescription ?? error.localizedDescription
                busy = false
            }
        }
    }
}
