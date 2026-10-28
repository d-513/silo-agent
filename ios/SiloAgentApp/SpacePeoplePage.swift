import SiloClient
import SwiftProtobuf
import SwiftUI

/// Who is in the Workspace and with what role. Admins change roles (only the owner makes admins),
/// remove people and make invite links; everyone may leave (the owner hands the Workspace on first).
struct SpacePeoplePage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var people: [Silo_V1_SpacePerson]?
    @State private var invites: [Silo_V1_SpaceInvite] = []
    @State private var inviting = false
    @State private var removing: Silo_V1_SpacePerson?

    private var role: String { spaces.role }
    private var admin: Bool { canAdminSpace(role) }

    var body: some View {
        List {
            Section {
                ForEach(people ?? [], id: \.userID) { person in
                    PersonRow(person: person, callerRole: role, remove: { removing = person }) { next in
                        await setRole(person, next)
                    }
                }
            } footer: {
                if people?.count == 1 {
                    Text("Only you are in \(spaces.current?.name ?? "this workspace"). Invite people to share its Bots and what they use, without sharing your own.")
                } else {
                    Text("Everyone here can be given Bots to use. Admins see every Bot in the workspace, but cannot open one they are not on.")
                }
            }
            if admin && !invites.isEmpty {
                Section("Open invite links") {
                    ForEach(invites, id: \.id) { invite in
                        HStack(spacing: 10) {
                            Image(systemName: "link").foregroundStyle(Theme.ink3)
                            Text("\(spaceRoleWord(invite.role)) · made by \(invite.createdBy) · until \(shortDay(invite.expiresAt))")
                                .font(.silo(14)).lineLimit(2)
                        }
                        .swipeActions {
                            Button(role: .destructive) { Task { await deleteInvite(invite) } } label: { Label("Delete", systemImage: "trash") }
                        }
                    }
                }
            }
        }
        .siloList()
        .overlay { if people == nil { ProgressView() } }
        .toolbar {
            if admin {
                ToolbarItem(placement: .primaryAction) {
                    Button { inviting = true } label: { Label("Invite", systemImage: "person.badge.plus") }
                }
            }
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
        .navigationDestination(item: $removing) { person in
            RemovePersonPage(userID: person.userID, leaving: person.you)
        }
        .sheet(isPresented: $inviting, onDismiss: { Task { await load() } }) {
            if let space = spaces.current { SpaceInviteSheet(space: space) }
        }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do {
            people = try await model.client.listSpacePeople(spaceID: spaceID).people
            if admin { invites = try await model.client.listSpaceInvites(spaceID: spaceID) }
        } catch { await model.report(error) }
    }

    private func setRole(_ person: Silo_V1_SpacePerson, _ next: String) async {
        guard let spaceID = spaces.currentID else { return }
        do { _ = try await model.client.setSpaceRole(spaceID: spaceID, userID: person.userID, role: next) } catch { await model.report(error) }
        await load()
    }

    private func deleteInvite(_ invite: Silo_V1_SpaceInvite) async {
        guard let spaceID = spaces.currentID else { return }
        do { try await model.client.deleteSpaceInvite(spaceID: spaceID, id: invite.id) } catch { await model.report(error) }
        await load()
    }
}

private struct PersonRow: View {
    let person: Silo_V1_SpacePerson
    let callerRole: String
    let remove: () -> Void
    let setRole: (String) async -> Void

    private var editable: Bool { !person.you && mayChangePerson(caller: callerRole, theirs: person.role) }

    var body: some View {
        HStack(spacing: 12) {
            PersonCircle(email: person.email)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(person.email).font(.silo(15, .medium)).lineLimit(1).truncationMode(.middle)
                    if person.you { Text("you").font(.silo(12)).foregroundStyle(Theme.ink3) }
                    if person.disabled { WellChip(text: "Disabled", tone: Theme.vermilion) }
                }
                Text(person.bots == 1 ? "On 1 Bot" : "On \(person.bots) Bots").font(.silo(12)).foregroundStyle(Theme.ink3)
            }
            Spacer(minLength: 6)
            if editable {
                Menu {
                    Picker("Role", selection: Binding(get: { person.role }, set: { next in Task { await setRole(next) } })) {
                        Text("Admin").tag("admin").selectionDisabled(callerRole != "owner")
                        Text("Member").tag("member")
                        Text("Guest").tag("guest")
                    }
                    if callerRole != "owner" { Text("Only the owner makes admins.") }
                } label: {
                    HStack(spacing: 3) {
                        Text(spaceRoleWord(person.role))
                        Image(systemName: "chevron.up.chevron.down").font(.system(size: 10))
                    }
                    .font(.silo(13, .medium))
                    .foregroundStyle(Theme.ink2)
                }
            } else {
                WellChip(text: person.role == "owner" ? "Owner" : spaceRoleWord(person.role))
            }
        }
        .padding(.vertical, 2)
        .swipeActions {
            if editable || (person.you && person.role != "owner") {
                Button(action: remove) {
                    Label(person.you ? "Leave" : "Remove", systemImage: person.you ? "rectangle.portrait.and.arrow.right" : "person.badge.minus")
                }
                .tint(Theme.vermilion)
            }
        }
        .contextMenu {
            if editable || (person.you && person.role != "owner") {
                Button(action: remove) {
                    Label(person.you ? "Leave workspace…" : "Remove…", systemImage: "person.badge.minus")
                }
            }
        }
    }
}

/// Make an invite link: the role they join as, then the link, shown once, to share.
struct SpaceInviteSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let space: Silo_V1_Space
    @State private var role = "member"
    @State private var url = ""
    @State private var busy = false
    @State private var failure: String?

    private let hints = [
        "admin": "Sees every Bot in the workspace, invites people and removes them. Cannot open a Bot they are not on.",
        "member": "Makes Bots, adds connectors, drives and skills, and uses the Bots they are given.",
        "guest": "Only uses the Bots they are given. Does not see the rest of the workspace.",
    ]

    var body: some View {
        NavigationStack {
            Form {
                if url.isEmpty {
                    Section {
                        Picker("They join as", selection: $role) {
                            if space.role == "owner" { Text("Admin").tag("admin") }
                            Text("Member").tag("member")
                            Text("Guest").tag("guest")
                        }
                        .pickerStyle(.segmented)
                    } header: {
                        Text("They join as")
                    } footer: {
                        Text(hints[role] ?? "")
                    }
                    Section {
                        Button(busy ? "Making…" : "Make link") { Task { await make() } }
                            .disabled(busy)
                    }
                } else {
                    Section {
                        Text(url).font(.siloMono(13)).textSelection(.enabled).lineLimit(3)
                        ShareLink(item: url) { Label("Share link", systemImage: "square.and.arrow.up") }
                        Button { UIPasteboard.general.string = url } label: { Label("Copy link", systemImage: "doc.on.doc") }
                    } footer: {
                        Text("Send this link to the person you are inviting. It works once, for 7 days, and is shown only now.")
                    }
                    Section {
                        Button("Make another") { url = "" }
                    }
                }
                if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
            }
            .siloList()
            .navigationTitle("Invite to \(space.name)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }

    private func make() async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            url = try await model.client.createSpaceInvite(spaceID: space.id, role: role).url
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

/// Removing someone, or leaving: what happens to the Bots only they manage, what leaves with them,
/// then one confirmed press.
struct RemovePersonPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    let userID: String
    let leaving: Bool
    @State private var plan: Silo_V1_SpaceRemovalPlan?
    @State private var failure: String?
    @State private var choices: [String: Handoff] = [:]
    @State private var confirming = false
    @State private var busy = false

    var body: some View {
        Form {
            if let plan {
                if !plan.lastManaged.isEmpty {
                    Section {
                        ForEach(plan.lastManaged, id: \.botID) { bot in handoffRow(bot) }
                    } header: {
                        Text("Bots nobody else manages")
                    } footer: {
                        Text("A Bot always has a manager. Choose what happens to each.")
                    }
                }
                let personal = plan.lent.filter { !$0.shared }
                let shared = plan.lent.filter(\.shared)
                if !personal.isEmpty {
                    Section {
                        ForEach(personal, id: \.id) { lentRow($0) }
                    } header: { Text("Deleted with them") } footer: {
                        Text("Personal sign-ins carry their account, so they go too.")
                    }
                }
                if !shared.isEmpty {
                    Section {
                        ForEach(shared, id: \.id) { lentRow($0) }
                    } header: { Text("Signed out") } footer: {
                        Text("Shared sign-ins stay in the workspace and wait for someone to sign them in again.")
                    }
                }
                if plan.lastManaged.isEmpty && plan.lent.isEmpty {
                    Section {
                        Text(leaving
                             ? "You will lose access to the workspace and its Bots."
                             : "They will lose access to the workspace and its Bots. Their chats stay with the Bots.")
                            .font(.silo(15)).foregroundStyle(Theme.ink2)
                    }
                }
                Section {
                    Button(leaving ? "Leave workspace" : "Remove \(plan.email)", role: .destructive) { confirming = true }
                        .disabled(busy)
                }
            } else if failure == nil {
                ProgressView().frame(maxWidth: .infinity)
            }
            if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
        }
        .siloList()
        .navigationTitle(leaving ? "Leave \(spaces.current?.name ?? "")" : "Remove \(plan?.email ?? "")")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .confirmationDialog(leaving ? "Leave this workspace?" : "Remove them?", isPresented: $confirming, titleVisibility: .visible) {
            Button(leaving ? "Leave" : "Remove", role: .destructive) { Task { await remove() } }
        }
    }

    @ViewBuilder
    private func handoffRow(_ bot: Silo_V1_LastManaged) -> some View {
        let heirs = bot.others.filter { $0.spaceRole != "guest" }
        let choice = choiceOf(bot)
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                CrestView(index: bot.crest, size: 26)
                Text(bot.name).font(.silo(15, .medium))
            }
            Picker(bot.name, selection: Binding(get: { choice.action }, set: { action in
                choices[bot.botID] = Handoff(action: action, userID: action == "manager" ? (heirs.first?.userID ?? "") : "")
            })) {
                ForEach(handoffActions(hasHeirs: !heirs.isEmpty, leaving: leaving), id: \.self) { action in
                    Text(handoffWord(action)).tag(action)
                }
            }
            .pickerStyle(.segmented)
            if choice.action == "manager" {
                Picker("New manager", selection: Binding(get: { choice.userID }, set: { choices[bot.botID] = Handoff(action: "manager", userID: $0) })) {
                    ForEach(heirs, id: \.userID) { Text($0.email).tag($0.userID) }
                }
            }
            if choice.action == "delete" {
                Text("Its machine, files, memories and chats are deleted.").font(.silo(13)).foregroundStyle(Theme.vermilion)
            }
        }
        .padding(.vertical, 4)
    }

    private func lentRow(_ item: Silo_V1_Lent) -> some View {
        HStack {
            Text(item.name).font(.silo(15, .medium))
            Text(item.kind == "drive" ? "Drive" : "Connector").font(.silo(13)).foregroundStyle(Theme.ink3)
            Spacer()
            let count = item.bots.count + Int(item.botsHidden)
            if count > 0 { Text("in \(count) Bot\(count == 1 ? "" : "s")").font(.silo(12)).foregroundStyle(Theme.ink3) }
        }
    }

    private func handoffWord(_ action: String) -> String {
        switch action {
        case "manager": return "New manager"
        case "take": return "Take it over"
        default: return "Delete it"
        }
    }

    private func choiceOf(_ bot: Silo_V1_LastManaged) -> Handoff {
        if let choice = choices[bot.botID] { return choice }
        return defaultHandoff(heirs: bot.others.filter { $0.spaceRole != "guest" }.map(\.userID), leaving: leaving)
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do { plan = try await model.client.planSpaceRemoval(spaceID: spaceID, userID: userID) } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }

    private func remove() async {
        guard let plan, let spaceID = spaces.currentID else { return }
        busy = true
        defer { busy = false }
        let handoffs = plan.lastManaged.map { bot in
            let choice = choiceOf(bot)
            return Silo_V1_Handoff.with {
                $0.botID = bot.botID
                $0.action = choice.action
                $0.userID = choice.userID
            }
        }
        do {
            try await model.client.removeSpacePerson(spaceID: spaceID, userID: userID, plan: handoffs)
            if leaving {
                await spaces.gone(spaceID)
            } else {
                await model.refreshBots()
                dismiss()
            }
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

/// What happened in the Workspace: people joining and leaving, Bots made and deleted, what was
/// given to which Bot, and sign-ins. Never what a Bot did with it.
struct SpaceActivityPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var events: [Silo_V1_SpaceActivity]?

    var body: some View {
        List {
            Section {
                ForEach(events ?? [], id: \.id) { event in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(spaceActivityLine(kind: event.kind, subject: event.subject, detail: event.detail, botName: event.botName))
                            .font(.silo(15))
                        Text([event.actor, relativeTime(event.createdAt)].filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.silo(12)).foregroundStyle(Theme.ink3)
                    }
                    .padding(.vertical, 1)
                }
            } footer: {
                Text("People joining and leaving, Bots made and deleted, what was given to which Bot, and sign-ins. What a Bot does with it stays with its managers.")
            }
        }
        .siloList()
        .overlay {
            if events == nil { ProgressView() } else if events?.isEmpty == true {
                ContentUnavailableView("Nothing yet", systemImage: "clock.arrow.circlepath")
            }
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(15))
            }
        }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do { events = try await model.client.listSpaceActivity(spaceID: spaceID) } catch { await model.report(error) }
    }
}
