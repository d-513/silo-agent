import SiloClient
import SwiftUI

/// A Bot's Settings → People: who can use it and who manages it. Managers change the list;
/// everyone else reads it and may leave. A Bot always keeps one manager.
struct BotPeoplePage: View {
    @Environment(AppModel.self) private var model
    @State private var people: [Silo_V1_BotPerson]?
    @State private var candidates: [Silo_V1_SpacePerson] = []
    @State private var adding = false
    @State private var seen = FirstSeen()
    @State private var changed = 0

    private var bot: Silo_V1_Bot? { model.selectedBot }
    private var manage: Bool { bot?.role == "manage" }
    private var managers: Int { people?.filter { $0.role == "manage" }.count ?? 0 }

    var body: some View {
        PageScroll(spacing: 12) {
            if let people {
                PanelSection(footer: footnote) {
                    ForEach(people, id: \.userID) { person in
                        row(person).rise(when: seen.isNew(person.userID))
                    }
                }
            } else {
                SkeletonRows(count: 2, height: 56)
            }
        }
        .pageActions {
            if manage {
                Button { adding = true } label: { Image(systemName: "person.badge.plus") }
                    .buttonStyle(.glyph)
                    .accessibilityLabel("Add people")
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sensoryFeedback(.success, trigger: changed)
        .sheet(isPresented: $adding, onDismiss: { Task { await load() } }) {
            if let bot {
                AddBotPeopleSheet(botID: bot.id, candidates: candidates)
                    .siloSheet()
            }
        }
    }

    private var footnote: String {
        let shared = "Everyone on \(bot?.name ?? "this Bot") shares what it holds: its memory, files, machine and connections. Only its managers change what it may do."
        return manage && managers <= 1 ? "\(shared) A Bot always has a manager. Make someone else a manager before you step down." : shared
    }

    private func row(_ person: Silo_V1_BotPerson) -> some View {
        let last = person.role == "manage" && managers <= 1
        return HStack(alignment: .top, spacing: 12) {
            Initials(email: person.email)
            VStack(alignment: .leading, spacing: 6) {
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 6) {
                        Text(person.email).font(.silo(13.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1).truncationMode(.middle)
                        if person.you { Text("you").font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize() }
                    }
                    if person.spaceRole == "guest" { Text("Guest").font(.silo(12)).foregroundStyle(Theme.ink3) }
                }
                HStack(spacing: 4) {
                    if manage && !last {
                        roleMenu(person)
                    } else {
                        Chip(text: roleWord(person.role))
                    }
                    Spacer(minLength: 0)
                    if (manage || person.you) && !last {
                        ArmedButton(title: person.you ? "Leave this Bot" : "Remove",
                                    armedTitle: person.you ? "Tap again to leave" : "Tap again to remove", small: true) {
                            Task { await remove(person) }
                        }
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }

    private func roleMenu(_ person: Silo_V1_BotPerson) -> some View {
        SiloMenu {
            MenuRow(title: "Can use", checked: person.role == "use") {
                Task { await setRole(person, "use") }
            }
            MenuRow(title: "Can manage", detail: person.spaceRole == "guest" ? "A guest can only use a Bot." : nil,
                    checked: person.role == "manage", enabled: person.spaceRole != "guest") {
                Task { await setRole(person, "manage") }
            }
        } label: {
            HStack(spacing: 4) {
                Text(roleWord(person.role)).font(.silo(13, .medium)).foregroundStyle(Theme.ink2)
                Image(systemName: "chevron.up.chevron.down").font(.system(size: 9, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 10)
            .frame(height: 32)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
        }
        .accessibilityLabel("Role of \(person.email): \(roleWord(person.role))")
    }

    private func roleWord(_ role: String) -> String { role == "manage" ? "Can manage" : "Can use" }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listBotPeople(botID: botID)
            seen.note(list.people.map(\.userID))
            withAnimation(people == nil ? nil : Motion.state) { people = list.people }
            candidates = list.candidates
        } catch { await model.report(error) }
    }

    private func setRole(_ person: Silo_V1_BotPerson, _ role: String) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.setBotRole(botID: botID, userID: person.userID, role: role)
            changed += 1
        } catch { await model.report(error) }
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
            changed += 1
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
        VStack(spacing: 0) {
            SheetHead(title: "Add people", confirm: {
                Button("Done") { dismiss() }.buttonStyle(.ink)
            })
            PageScroll(spacing: 16) {
                VStack(alignment: .leading, spacing: 6) {
                    Segmented(options: [("use", "Can use"), ("manage", "Can manage")], selection: $role)
                    Text("A guest can only use a Bot.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                }
                if candidates.isEmpty {
                    MachineNote(text: "Everyone in the workspace is on this Bot. Invite people from the workspace's People tab.")
                } else {
                    PanelSection(title: "People in the workspace") {
                        ForEach(candidates, id: \.userID) { person in candidate(person) }
                    }
                }
                if let failure { ErrorLine(text: failure) }
            }
        }
        .sensoryFeedback(.success, trigger: added.count)
    }

    private func candidate(_ person: Silo_V1_SpacePerson) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Button { Task { await add(person) } } label: {
                HStack(spacing: 10) {
                    Initials(email: person.email, size: 24)
                    Text(person.email).font(.silo(13.5)).foregroundStyle(Theme.ink).lineLimit(1).truncationMode(.middle)
                    if person.role == "guest" { Text("Guest").font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize() }
                    Spacer(minLength: 0)
                    ZStack {
                        if busy == person.userID {
                            Spinner(size: 14)
                        } else if added.contains(person.userID) {
                            Image(systemName: "checkmark").font(.system(size: 13, weight: .semibold)).foregroundStyle(Theme.emerald).transition(.scale(scale: 0.6).combined(with: .opacity))
                        } else {
                            Image(systemName: "plus").font(.system(size: 13)).foregroundStyle(Theme.ink3)
                        }
                    }
                    .frame(width: 20)
                    .animation(Motion.settle(0.28), value: added.contains(person.userID))
                }
                .padding(.horizontal, 14)
                .frame(minHeight: 48)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .disabled(added.contains(person.userID) || busy != nil)
            if let confirm, confirm.userID == person.userID {
                MachineNote(text: "They will be able to have the Bot use secrets someone else set: \(confirm.secrets.joined(separator: ", ")). Tap again to add them.")
                    .padding(.horizontal, 14)
                    .padding(.bottom, 10)
                    .transition(.rise)
            }
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
                withAnimation(Motion.fold) { confirm = (person.userID, answer.confirmSecrets) }
                return
            }
            withAnimation(Motion.fold) { confirm = nil }
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
    @State private var busy = false
    @State private var failure: String?

    private var bot: Silo_V1_Bot? { model.selectedBot }
    private var targets: [Silo_V1_Space] {
        spaces.spaces.filter { $0.id != bot?.spaceID && canBuildSpace($0.role) }
    }

    var body: some View {
        PageScroll(spacing: 16) {
            PanelSection(title: "In") {
                HStack(spacing: 12) {
                    let here = bot.flatMap { spaces.space($0.spaceID) }
                    SpaceMarkView(mark: here?.mark ?? "", size: 32)
                    Text(here?.name ?? "Workspace").font(.silo(15, .medium)).foregroundStyle(Theme.ink)
                }
                .padding(.horizontal, 14)
                .frame(minHeight: 52)
            }
            if bot?.role != "manage" {
                MachineNote(text: "Only a manager of this Bot can move it.")
            } else if targets.isEmpty {
                MachineNote(text: "There is no other workspace you can make Bots in.")
            } else {
                PanelSection(title: "Move to another workspace",
                             footer: plan.map { moveSummary(connectors: $0.connectors, drives: $0.drives, skills: $0.skills, people: $0.people) }
                                 ?? "Its machine, files, memories, chats, rules and secrets go with it.") {
                    VStack(alignment: .leading, spacing: 12) {
                        SiloPicker(options: [(value: "", label: "Choose a workspace")] + targets.map { (value: $0.id, label: $0.name) }, selection: $target)
                        if !target.isEmpty {
                            ArmedButton(title: busy ? "Moving…" : "Move", armedTitle: "Tap again to move to \(spaces.space(target)?.name ?? "the workspace")", wide: true) {
                                Task { await move() }
                            }
                            .disabled(busy || plan == nil)
                            .transition(.opacity)
                        }
                    }
                    .padding(14)
                    .animation(Motion.fold, value: target.isEmpty)
                }
            }
            if let failure { ErrorLine(text: failure) }
        }
        .task { await spaces.load() }
        .task(id: target) { await loadPlan() }
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
