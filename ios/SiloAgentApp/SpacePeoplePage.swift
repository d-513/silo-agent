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

    private var note: String {
        var words = people?.count == 1
            ? "Only you are in \(spaces.current?.name ?? "this workspace"). Invite people to share its Bots and what they use, without sharing your own."
            : "Everyone here can be given Bots to use. Admins see every Bot in the workspace, but cannot open one they are not on."
        if admin, role != "owner", (people?.count ?? 0) > 1 { words += " Only the owner makes admins." }
        return words
    }

    var body: some View {
        PageScroll {
            if let people {
                PanelSection(footer: note) {
                    ForEach(people, id: \.userID) { person in
                        PersonRow(person: person, callerRole: role, remove: { removing = person }) { next in
                            await setRole(person, next)
                        }
                    }
                }
                .rise()
            } else {
                SkeletonRows(count: 2, height: 56)
            }
            if admin && !invites.isEmpty {
                PanelSection(title: "Open invite links") {
                    ForEach(invites, id: \.id) { invite in
                        HStack(spacing: 10) {
                            Image(systemName: "link").font(.system(size: 13)).foregroundStyle(Theme.ink3)
                            Text("\(spaceRoleWord(invite.role)) · made by \(invite.createdBy) · until \(shortDay(invite.expiresAt))")
                                .font(.silo(13.5))
                                .foregroundStyle(Theme.ink)
                                .lineLimit(2)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            ArmedButton(title: "Delete", armedTitle: "Tap again", symbol: "trash", small: true) {
                                Task { await deleteInvite(invite) }
                            }
                            .layoutPriority(1)
                        }
                        .padding(.horizontal, 14)
                        .padding(.vertical, 8)
                        .transition(.rise)
                    }
                }
                .animation(Motion.arrival, value: invites.map(\.id))
            }
        }
        .pageActions {
            if admin {
                Button { inviting = true } label: { Image(systemName: "person.badge.plus") }
                    .buttonStyle(.glyph)
                    .accessibilityLabel("Invite")
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

/// One person: their letters, email and how many Bots they are on, then the role as a chip they
/// cannot change or a small choice they can, and Remove or Leave.
private struct PersonRow: View {
    let person: Silo_V1_SpacePerson
    let callerRole: String
    let remove: () -> Void
    let setRole: (String) async -> Void

    private var editable: Bool { !person.you && mayChangePerson(caller: callerRole, theirs: person.role) }
    private var leaves: Bool { person.you && person.role != "owner" }

    private var roles: [(value: String, label: String)] {
        (callerRole == "owner" ? ["admin", "member", "guest"] : ["member", "guest"]).map { (value: $0, label: spaceRoleWord($0)) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                Initials(email: person.email)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(person.email)
                            .font(.silo(14.5, .medium))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        if person.you { Text("you").font(.silo(12)).foregroundStyle(Theme.ink3) }
                        if person.disabled { Chip(text: "Disabled", tint: Theme.vermilion, fill: Theme.vermilionPale) }
                    }
                    Text(person.bots == 1 ? "On 1 Bot" : "On \(person.bots) Bots").font(.silo(12)).foregroundStyle(Theme.ink3)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if !editable {
                    Chip(text: person.role == "owner" ? "Owner" : spaceRoleWord(person.role))
                }
                if leaves {
                    Button("Leave", action: remove).buttonStyle(.ghost).controlSize(.small)
                }
            }
            if editable {
                HStack(spacing: 8) {
                    ChipRadio(options: roles, selection: Binding(get: { person.role }, set: { next in Task { await setRole(next) } }))
                    Button("Remove", action: remove).buttonStyle(.ghost).controlSize(.small)
                }
                .padding(.leading, 40)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
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

    private var roles: [(value: String, label: String)] {
        (space.role == "owner" ? ["admin", "member", "guest"] : ["member", "guest"]).map { (value: $0, label: spaceRoleWord($0)) }
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: "Invite to \(space.name)") {
                Button("Done") { dismiss() }.buttonStyle(.ghost)
            }
            PageScroll {
                if url.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("They join as").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                        ChipRadio(options: roles, selection: $role)
                        Text(hints[role] ?? "")
                            .font(.silo(13))
                            .foregroundStyle(Theme.ink2)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Button { Task { await make() } } label: {
                        HStack(spacing: 6) {
                            Image(systemName: "link").font(.system(size: 13, weight: .medium))
                            Text(busy ? "Making…" : "Make link")
                        }
                    }
                    .buttonStyle(.ink)
                    .disabled(busy)
                } else {
                    Text("Send this link to the person you are inviting. It works once, for 7 days, and is shown only now.")
                        .font(.silo(13.5))
                        .foregroundStyle(Theme.ink2)
                        .fixedSize(horizontal: false, vertical: true)
                    HStack(spacing: 4) {
                        Text(url)
                            .font(.siloMono(13))
                            .foregroundStyle(Theme.ink)
                            .textSelection(.enabled)
                            .lineLimit(3)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        CopyButton(text: url)
                    }
                    .padding(.leading, 12)
                    .padding(.vertical, 4)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                    .rise()
                    HStack(spacing: 8) {
                        ShareLink(item: url) {
                            HStack(spacing: 6) {
                                Image(systemName: "square.and.arrow.up").font(.system(size: 13, weight: .medium))
                                Text("Share link")
                            }
                        }
                        .buttonStyle(.ink)
                        Button("Make another") { url = "" }.buttonStyle(.quiet)
                    }
                }
                if let failure { ErrorLine(text: failure) }
            }
        }
        .sensoryFeedback(.success, trigger: url) { _, new in !new.isEmpty }
        .siloSheet(detents: [.medium, .large])
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
/// then one armed press.
struct RemovePersonPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    let userID: String
    let leaving: Bool
    @State private var plan: Silo_V1_SpaceRemovalPlan?
    @State private var failure: String?
    @State private var choices: [String: Handoff] = [:]
    @State private var busy = false

    var body: some View {
        SiloPage(title: leaving ? "Leave \(spaces.current?.name ?? "")" : "Remove \(plan?.email ?? "")") {
            PageScroll {
                if let plan {
                    if !plan.lastManaged.isEmpty {
                        PanelSection(title: "Bots nobody else manages", footer: "A Bot always has a manager. Choose what happens to each.") {
                            ForEach(plan.lastManaged, id: \.botID) { bot in handoffRow(bot) }
                        }
                    }
                    let personal = plan.lent.filter { !$0.shared }
                    let shared = plan.lent.filter(\.shared)
                    if !personal.isEmpty {
                        PanelSection(title: "Deleted with them", footer: "Personal sign-ins carry their account, so they go too.") {
                            ForEach(personal, id: \.id) { lentRow($0) }
                        }
                    }
                    if !shared.isEmpty {
                        PanelSection(title: "Signed out", footer: "Shared sign-ins stay in the workspace and wait for someone to sign them in again.") {
                            ForEach(shared, id: \.id) { lentRow($0) }
                        }
                    }
                    if plan.lastManaged.isEmpty && plan.lent.isEmpty {
                        Text(leaving
                             ? "You will lose access to the workspace and its Bots."
                             : "They will lose access to the workspace and its Bots. Their chats stay with the Bots.")
                            .font(.silo(15))
                            .foregroundStyle(Theme.ink2)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    ArmedButton(title: leaving ? "Leave workspace" : "Remove \(plan.email)", armedTitle: "Tap again to confirm", wide: true) {
                        Task { await remove() }
                    }
                    .disabled(busy)
                } else if failure == nil {
                    SkeletonRows(count: 2, height: 64)
                }
                if let failure { ErrorLine(text: failure) }
            }
        }
        .task { await load() }
    }

    @ViewBuilder
    private func handoffRow(_ bot: Silo_V1_LastManaged) -> some View {
        let heirs = bot.others.filter { $0.spaceRole != "guest" }
        let choice = choiceOf(bot)
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                CrestView(index: bot.crest, size: 26)
                Text(bot.name).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
            }
            ChipRadio(
                options: handoffActions(hasHeirs: !heirs.isEmpty, leaving: leaving).map { (value: $0, label: handoffWord($0)) },
                selection: Binding(get: { choice.action }, set: { action in
                    choices[bot.botID] = Handoff(action: action, userID: action == "manager" ? (heirs.first?.userID ?? "") : "")
                }))
            if choice.action == "manager" {
                SiloPicker(
                    label: "New manager",
                    options: heirs.map { (value: $0.userID, label: $0.email) },
                    selection: Binding(get: { choice.userID }, set: { choices[bot.botID] = Handoff(action: "manager", userID: $0) }))
                    .transition(.rise)
            }
            if choice.action == "delete" {
                Text("Its machine, files, memories and chats are deleted.")
                    .font(.silo(13))
                    .foregroundStyle(Theme.vermilion)
                    .transition(.rise)
            }
        }
        .padding(14)
        .animation(Motion.fold, value: choice.action)
    }

    private func lentRow(_ item: Silo_V1_Lent) -> some View {
        HStack(spacing: 8) {
            Text(item.name).font(.silo(14.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
            Text(item.kind == "drive" ? "Drive" : "Connector").font(.silo(13)).foregroundStyle(Theme.ink3)
            Spacer(minLength: 8)
            let count = item.bots.count + Int(item.botsHidden)
            if count > 0 { Text("in \(count) Bot\(count == 1 ? "" : "s")").font(.silo(12)).foregroundStyle(Theme.ink3) }
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 44)
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

    private let note = "People joining and leaving, Bots made and deleted, what was given to which Bot, and sign-ins. What a Bot does with it stays with its managers."

    var body: some View {
        PageScroll {
            if let events {
                if events.isEmpty {
                    EmptyWell(symbol: "clock.arrow.circlepath", title: "Nothing yet", message: note)
                } else {
                    PanelSection(footer: note) {
                        ForEach(events, id: \.id) { event in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(spaceActivityLine(kind: event.kind, subject: event.subject, detail: event.detail, botName: event.botName))
                                    .font(.silo(14))
                                    .foregroundStyle(Theme.ink)
                                    .fixedSize(horizontal: false, vertical: true)
                                Text([event.actor, relativeTime(event.createdAt)].filter { !$0.isEmpty }.joined(separator: " · "))
                                    .font(.silo(12))
                                    .foregroundStyle(Theme.ink3)
                            }
                            .padding(.horizontal, 14)
                            .padding(.vertical, 9)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .transition(.rise)
                        }
                    }
                    .animation(Motion.arrival, value: events.map(\.id))
                }
            } else {
                SkeletonRows(count: 6, height: 52)
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
