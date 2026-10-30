import SiloClient
import SwiftUI

/// The name of a Workspace and the letters on its tile, with the tile as it will look.
private struct SpaceNameFields: View {
    @Binding var name: String
    @Binding var mark: String

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            SpaceMarkView(mark: spaceMark(name: name, given: mark), size: 44)
                .padding(.top, 22)
            VStack(alignment: .leading, spacing: 12) {
                SiloField(label: "Name", prompt: "Acme labs", text: $name)
                SiloField(label: "Letters on its tile", prompt: spaceMark(name: name), text: $mark)
                    .textInputAutocapitalization(.characters)
                    .onChange(of: mark) { _, value in if value.count > 2 { mark = String(value.prefix(2)) } }
            }
        }
    }
}

/// The Workspace's own settings: its name and letters (admins), leaving it (anyone but the owner)
/// or handing it to an admin (the owner), the Bots an admin is not on, and deleting it (the owner).
struct SpaceSettingsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var name = ""
    @State private var mark = ""
    @State private var people: [Silo_V1_SpacePerson] = []
    @State private var heir = ""
    @State private var others: [Silo_V1_OtherBot] = []
    @State private var leaving: Silo_V1_SpacePerson?
    @State private var failure: String?

    private var space: Silo_V1_Space? { spaces.current }
    private var role: String { spaces.role }
    private var admins: [Silo_V1_SpacePerson] { people.filter { $0.role == "admin" } }
    private var changed: Bool {
        guard let space else { return false }
        return name.trimmingCharacters(in: .whitespaces) != space.name || spaceMark(name: name, given: mark) != space.mark
    }

    var body: some View {
        PageScroll {
            PanelSection(title: "General") {
                VStack(alignment: .leading, spacing: 14) {
                    SpaceNameFields(name: $name, mark: $mark)
                        .disabled(!canAdminSpace(role))
                    if canAdminSpace(role) {
                        SaveButton(enabled: changed && !name.trimmingCharacters(in: .whitespaces).isEmpty) { await save() }
                            .controlSize(.small)
                    }
                }
                .padding(14)
            }

            membership

            if canAdminSpace(role) && !others.isEmpty {
                PanelSection(title: "Other Bots", footer: "Bots in this workspace you are not on. As an admin you can delete one, with its machine, files, memories and chats, but not open it.") {
                    ForEach(others, id: \.id) { otherRow($0) }
                }
            }

            if role == "owner" {
                PanelSection(title: "Dangerous", footer: "Only an empty workspace can go, and never your last one. Move or delete its Bots first.") {
                    ArmedButton(title: "Delete workspace", armedTitle: "Tap again to delete \(space?.name ?? "it")", wide: true) {
                        Task { await deleteSpace() }
                    }
                    .padding(6)
                }
            }
            if let failure { ErrorLine(text: failure).transition(.rise) }
        }
        .task(id: spaces.currentID) { await load() }
        .refreshable { await load() }
        .navigationDestination(item: $leaving) { me in RemovePersonPage(userID: me.userID, leaving: true) }
        .sensoryFeedback(.impact(weight: .medium), trigger: others.count) { old, new in new < old }
    }

    @ViewBuilder
    private var membership: some View {
        if role == "owner" {
            if people.count > 1 {
                PanelSection(title: "Ownership", footer: admins.isEmpty ? nil : "The admin you pick becomes the owner; you become an admin.") {
                    if admins.isEmpty {
                        Text("Make someone an admin first: only an admin can become the owner.")
                            .font(.silo(14))
                            .foregroundStyle(Theme.ink2)
                            .padding(14)
                    } else {
                        VStack(alignment: .leading, spacing: 12) {
                            SiloPicker(label: "New owner", options: admins.map { (value: $0.userID, label: $0.email) }, selection: $heir)
                            ArmedButton(title: "Transfer ownership", armedTitle: "Tap again: \(people.first { $0.userID == heir }?.email ?? "they") owns it", wide: true) {
                                Task { await transfer() }
                            }
                            .disabled(heir.isEmpty)
                        }
                        .padding(14)
                    }
                }
            }
        } else if let me = people.first(where: \.you) {
            PanelSection(title: "Membership", footer: "You lose access to its Bots. Bots only you manage need a new manager first.") {
                PanelRow(symbol: "rectangle.portrait.and.arrow.right", title: "Leave workspace…", chevron: true) { leaving = me }
            }
        }
    }

    /// A Bot the admin is not on: dimmed, never a link, and only Delete on it.
    private func otherRow(_ bot: Silo_V1_OtherBot) -> some View {
        HStack(spacing: 12) {
            CrestView(index: bot.crest, size: 40).opacity(0.6)
            VStack(alignment: .leading, spacing: 4) {
                Text(bot.name).font(.silo(15, .medium)).foregroundStyle(Theme.ink2).lineLimit(1)
                HStack(spacing: 6) {
                    HStack(spacing: -4) {
                        ForEach(bot.people.prefix(3), id: \.self) { Initials(email: $0, size: 20) }
                    }
                    if bot.people.count > 3 {
                        Text("+\(bot.people.count - 3)").font(.silo(11.5)).foregroundStyle(Theme.ink3)
                    }
                    Text("\(bot.grants) grant\(bot.grants == 1 ? "" : "s")").font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            ArmedButton(title: "Delete", armedTitle: "Tap again", symbol: "trash", small: true) {
                Task { await deleteBot(bot) }
            }
            .layoutPriority(1)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .accessibilityElement(children: .contain)
        .accessibilityHint("Only people on \(bot.name) can open it.")
    }

    private func load() async {
        guard let space else { return }
        if name.isEmpty {
            name = space.name
            mark = space.mark == spaceMark(name: space.name) ? "" : space.mark
        }
        do {
            people = try await model.client.listSpacePeople(spaceID: space.id).people
            if heir.isEmpty { heir = admins.first?.userID ?? "" }
            if canAdminSpace(role) { others = try await model.client.listBots(spaceID: space.id).others }
        } catch { await model.report(error) }
    }

    private func save() async -> Bool {
        guard let space else { return false }
        do {
            _ = try await model.client.updateSpace(id: space.id, name: name.trimmingCharacters(in: .whitespaces), mark: mark.trimmingCharacters(in: .whitespaces))
            await spaces.load()
            failure = nil
            return true
        } catch {
            failure = describe(error)
            return false
        }
    }

    private func transfer() async {
        guard let space else { return }
        do {
            _ = try await model.client.transferSpace(spaceID: space.id, userID: heir)
            await spaces.load()
            await load()
        } catch { failure = describe(error) }
    }

    private func deleteBot(_ bot: Silo_V1_OtherBot) async {
        do {
            try await model.client.deleteBot(bot.id)
            withAnimation(Motion.state) { others.removeAll { $0.id == bot.id } }
        } catch { failure = describe(error) }
    }

    private func deleteSpace() async {
        guard let space else { return }
        do {
            try await model.client.deleteSpace(id: space.id)
            await spaces.gone(space.id)
        } catch { failure = describe(error) }
    }

    private func describe(_ error: Error) -> String { (error as? SiloError)?.errorDescription ?? error.localizedDescription }
}

/// A new Workspace: its name and the letters on its tile. It opens empty, ready for its first Bot.
struct NewSpaceSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var mark = ""
    @State private var busy = false
    @State private var failure: String?

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: "New workspace", onCancel: { dismiss() }) {
                Button(busy ? "Creating…" : "Create") { Task { await create() } }
                    .buttonStyle(.ink)
                    .disabled(busy || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            PageScroll {
                Text("A workspace holds Bots and what they share. Each Bot still has its own machine.")
                    .font(.silo(13.5))
                    .foregroundStyle(Theme.ink2)
                    .fixedSize(horizontal: false, vertical: true)
                SpaceNameFields(name: $name, mark: $mark)
                if let failure { ErrorLine(text: failure) }
            }
        }
    }

    private func create() async {
        busy = true
        defer { busy = false }
        do {
            let space = try await model.client.createSpace(name: name.trimmingCharacters(in: .whitespaces), mark: mark.trimmingCharacters(in: .whitespaces))
            await spaces.load()
            spaces.open(space.id)
            dismiss()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
