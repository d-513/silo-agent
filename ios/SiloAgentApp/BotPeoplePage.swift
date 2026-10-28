import SiloClient
import SwiftUI

/// A Bot's Settings → People: who can use it and who manages it. Managers change the list;
/// everyone else reads it and may leave. A Bot always keeps one manager.
struct BotPeoplePage: View {
    @Environment(AppModel.self) private var model
    @State private var people: [Silo_V1_BotPerson]?
    @State private var candidates: [Silo_V1_SpacePerson] = []
    @State private var adding = false
    @State private var removing: Silo_V1_BotPerson?

    private var bot: Silo_V1_Bot? { model.selectedBot }
    private var manage: Bool { bot?.role == "manage" }
    private var managers: Int { people?.filter { $0.role == "manage" }.count ?? 0 }

    var body: some View {
        List {
            Section {
                ForEach(people ?? [], id: \.userID) { person in
                    row(person)
                }
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Everyone on \(bot?.name ?? "this Bot") shares what it holds: its memory, files, machine and connections. Only its managers change what it may do.")
                    if manage && managers <= 1 {
                        Text("A Bot always has a manager. Make someone else a manager before you step down.")
                    }
                }
            }
        }
        .overlay { if people == nil { ProgressView() } }
        .toolbar {
            if manage {
                ToolbarItem(placement: .primaryAction) {
                    Button { adding = true } label: { Label("Add people", systemImage: "person.badge.plus") }
                }
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(isPresented: $adding, onDismiss: { Task { await load() } }) {
            if let bot { AddBotPeopleSheet(botID: bot.id, candidates: candidates) }
        }
        .confirmationDialog(removing?.you == true ? "Leave this Bot?" : "Remove \(removing?.email ?? "")?",
                            isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing) { person in
            Button(person.you ? "Leave this Bot" : "Remove", role: .destructive) { Task { await remove(person) } }
        } message: { person in
            Text(person.you ? "You lose access to it and its chats." : "They lose access to it. Their chats stay with the Bot.")
        }
    }

    private func row(_ person: Silo_V1_BotPerson) -> some View {
        let last = person.role == "manage" && managers <= 1
        return HStack(spacing: 12) {
            PersonCircle(email: person.email)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(person.email).font(.silo(15, .medium)).lineLimit(1).truncationMode(.middle)
                    if person.you { Text("you").font(.silo(12)).foregroundStyle(Theme.ink3) }
                }
                if person.spaceRole == "guest" { Text("Guest").font(.silo(12)).foregroundStyle(Theme.ink3) }
            }
            Spacer(minLength: 6)
            if manage && !last {
                Menu {
                    Picker("Role", selection: Binding(get: { person.role }, set: { role in Task { await setRole(person, role) } })) {
                        Text("Can use").tag("use")
                        Text("Can manage").tag("manage").selectionDisabled(person.spaceRole == "guest")
                    }
                    if person.spaceRole == "guest" { Text("A guest can only use a Bot.") }
                } label: {
                    HStack(spacing: 3) {
                        Text(roleWord(person.role))
                        Image(systemName: "chevron.up.chevron.down").font(.system(size: 10))
                    }
                    .font(.silo(13, .medium))
                    .foregroundStyle(Theme.ink2)
                }
            } else {
                WellChip(text: roleWord(person.role))
            }
        }
        .padding(.vertical, 2)
        .swipeActions {
            if (manage || person.you) && !last {
                Button(role: .destructive) { removing = person } label: {
                    Label(person.you ? "Leave" : "Remove", systemImage: "person.badge.minus")
                }
            }
        }
    }

    private func roleWord(_ role: String) -> String { role == "manage" ? "Can manage" : "Can use" }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listBotPeople(botID: botID)
            people = list.people
            candidates = list.candidates
        } catch { await model.report(error) }
    }

    private func setRole(_ person: Silo_V1_BotPerson, _ role: String) async {
        guard let botID = model.selectedBotID else { return }
        do { try await model.client.setBotRole(botID: botID, userID: person.userID, role: role) } catch { await model.report(error) }
        await load()
    }

    private func remove(_ person: Silo_V1_BotPerson) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.removeBotPerson(botID: botID, userID: person.userID)
            if person.you {
                await model.botDeleted(botID)
                return
            }
        } catch { await model.report(error) }
        await load()
    }
}

/// The Workspace's people who are not on the Bot. Someone who could have the Bot use secrets
/// another person set is named with those secrets before a second press adds them.
private struct AddBotPeopleSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let botID: String
    let candidates: [Silo_V1_SpacePerson]
    @State private var role = "use"
    @State private var confirm: (userID: String, secrets: [String])?
    @State private var added: Set<String> = []
    @State private var busy: String?
    @State private var failure: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Role", selection: $role) {
                        Text("Can use").tag("use")
                        Text("Can manage").tag("manage")
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    Text("A guest can only use a Bot.")
                }
                Section {
                    if candidates.isEmpty {
                        Text("Everyone in the workspace is on this Bot. Invite people from the workspace's People tab.")
                            .font(.silo(14)).foregroundStyle(Theme.ink3)
                    }
                    ForEach(candidates, id: \.userID) { person in
                        VStack(alignment: .leading, spacing: 6) {
                            Button { Task { await add(person) } } label: {
                                HStack(spacing: 10) {
                                    PersonCircle(email: person.email, size: 24)
                                    Text(person.email).font(.silo(15)).foregroundStyle(Theme.ink).lineLimit(1)
                                    if person.role == "guest" { Text("Guest").font(.silo(12)).foregroundStyle(Theme.ink3) }
                                    Spacer()
                                    if busy == person.userID {
                                        ProgressView().controlSize(.small)
                                    } else if added.contains(person.userID) {
                                        Image(systemName: "checkmark").foregroundStyle(Theme.emerald)
                                    } else {
                                        Image(systemName: "plus.circle").foregroundStyle(Theme.ink3)
                                    }
                                }
                            }
                            .buttonStyle(.plain)
                            .disabled(added.contains(person.userID) || busy != nil)
                            if let confirm, confirm.userID == person.userID {
                                Text("They will be able to have the Bot use secrets someone else set: \(confirm.secrets.joined(separator: ", ")). Tap again to add them.")
                                    .font(.silo(13)).foregroundStyle(Theme.ink2)
                                    .padding(10)
                                    .background(Theme.well, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                            }
                        }
                    }
                } header: { Text("People in the workspace") }
                if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
            }
            .siloList()
            .navigationTitle("Add people")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }

    private func add(_ person: Silo_V1_SpacePerson) async {
        busy = person.userID
        failure = nil
        defer { busy = nil }
        do {
            let answer = try await model.client.addBotPerson(
                botID: botID, userID: person.userID, role: person.role == "guest" ? "use" : role,
                confirmed: confirm?.userID == person.userID)
            if !answer.confirmSecrets.isEmpty {
                confirm = (person.userID, answer.confirmSecrets)
                return
            }
            confirm = nil
            added.insert(person.userID)
            await model.refreshBots()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

/// A Bot's Settings → Workspace: where it is, and moving it to another Workspace the manager may
/// build in. What it loses comes first (grants, the Workspace's skills, the people not there).
struct MoveBotPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var target = ""
    @State private var plan: Silo_V1_MoveBotPlan?
    @State private var confirming = false
    @State private var busy = false
    @State private var failure: String?

    private var bot: Silo_V1_Bot? { model.selectedBot }
    private var targets: [Silo_V1_Space] {
        spaces.spaces.filter { $0.id != bot?.spaceID && canBuildSpace($0.role) }
    }

    var body: some View {
        Form {
            Section {
                HStack(spacing: 12) {
                    let here = bot.flatMap { spaces.space($0.spaceID) }
                    SpaceMarkView(mark: here?.mark ?? "", size: 32)
                    Text(here?.name ?? "Workspace").font(.silo(16, .medium))
                }
            } header: { Text("In") }
            if bot?.role != "manage" {
                Section { Text("Only a manager of this Bot can move it.").foregroundStyle(Theme.ink2) }
            } else if targets.isEmpty {
                Section {
                    Text("There is no other workspace you can make Bots in.").foregroundStyle(Theme.ink2)
                }
            } else {
                Section {
                    Picker("Move to", selection: $target) {
                        Text("Choose a workspace").tag("")
                        ForEach(targets) { Text($0.name).tag($0.id) }
                    }
                } header: { Text("Move to another workspace") } footer: {
                    Text(plan.map { moveSummary(connectors: $0.connectors, drives: $0.drives, skills: $0.skills, people: $0.people) }
                         ?? "Its machine, files, memories, chats, rules and secrets go with it.")
                }
                if !target.isEmpty {
                    Section {
                        Button(busy ? "Moving…" : "Move", role: .destructive) { confirming = true }
                            .disabled(busy || plan == nil)
                    }
                }
            }
            if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
        }
        .task { await spaces.load() }
        .task(id: target) { await loadPlan() }
        .confirmationDialog("Move \(bot?.name ?? "this Bot")?", isPresented: $confirming, titleVisibility: .visible) {
            Button("Move to \(spaces.space(target)?.name ?? "the workspace")", role: .destructive) { Task { await move() } }
        } message: {
            Text(plan.map { moveSummary(connectors: $0.connectors, drives: $0.drives, skills: $0.skills, people: $0.people) } ?? "")
        }
    }

    private func loadPlan() async {
        plan = nil
        failure = nil
        guard let bot, !target.isEmpty else { return }
        do { plan = try await model.client.planMoveBot(botID: bot.id, spaceID: target) } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }

    private func move() async {
        guard let bot else { return }
        busy = true
        defer { busy = false }
        do {
            let moved = try await model.client.moveBot(botID: bot.id, spaceID: target)
            model.applyBot(moved)
            await model.refreshBots()
            await spaces.load()
            spaces.follow(moved)
            model.path = []
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
