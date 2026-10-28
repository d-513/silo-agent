import SiloClient
import SwiftUI

/// The Workspace's own settings: its name and letters (admins), leaving it (anyone but the owner)
/// or handing it to an admin (the owner), the Bots an admin is not on, and deleting it (the owner).
struct SpaceSettingsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var name = ""
    @State private var mark = ""
    @State private var saving = false
    @State private var people: [Silo_V1_SpacePerson] = []
    @State private var heir = ""
    @State private var transferring = false
    @State private var others: [Silo_V1_OtherBot] = []
    @State private var deletingBot: Silo_V1_OtherBot?
    @State private var deleting = false
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
        Form {
            Section {
                HStack(spacing: 14) {
                    SpaceMarkView(mark: spaceMark(name: name, given: mark), size: 44)
                    VStack(spacing: 8) {
                        TextField("Name", text: $name)
                        TextField("Letters on its tile", text: $mark)
                            .textInputAutocapitalization(.characters)
                            .onChange(of: mark) { _, value in if value.count > 2 { mark = String(value.prefix(2)) } }
                    }
                }
                .disabled(!canAdminSpace(role))
                if changed {
                    Button(saving ? "Saving…" : "Save") { Task { await save() } }
                        .disabled(saving || name.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            } header: { Text("General") }

            membership

            if canAdminSpace(role) && !others.isEmpty {
                Section {
                    ForEach(others, id: \.id) { bot in
                        HStack(spacing: 12) {
                            CrestView(index: bot.crest, size: 34).opacity(0.6)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(bot.name).font(.silo(15, .medium)).foregroundStyle(Theme.ink2)
                                Text(([bot.people.prefix(3).joined(separator: ", ")] + (bot.people.count > 3 ? ["+\(bot.people.count - 3)"] : []))
                                    .joined(separator: " ") + " · \(bot.grants) grant\(bot.grants == 1 ? "" : "s")")
                                    .font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                            }
                        }
                        .swipeActions {
                            Button(role: .destructive) { deletingBot = bot } label: { Label("Delete", systemImage: "trash") }
                        }
                    }
                } header: { Text("Other Bots") } footer: {
                    Text("Bots in this workspace you are not on. As an admin you can delete one (swipe), but not open it.")
                }
            }

            if role == "owner" {
                Section {
                    Button("Delete workspace", role: .destructive) { deleting = true }
                } header: { Text("Dangerous") } footer: {
                    Text("Only an empty workspace can go, and never your last one. Move or delete its Bots first.")
                }
            }
            if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
        }
        .siloList()
        .task(id: spaces.currentID) { await load() }
        .refreshable { await load() }
        .navigationDestination(item: $leaving) { me in RemovePersonPage(userID: me.userID, leaving: true) }
        .confirmationDialog("Transfer ownership?", isPresented: $transferring, titleVisibility: .visible) {
            Button("Transfer") { Task { await transfer() } }
        } message: {
            Text("\(people.first { $0.userID == heir }?.email ?? "They") becomes the owner; you become an admin.")
        }
        .confirmationDialog("Delete \(deletingBot?.name ?? "this Bot")?", isPresented: Binding(get: { deletingBot != nil }, set: { if !$0 { deletingBot = nil } }), titleVisibility: .visible, presenting: deletingBot) { bot in
            Button("Delete \(bot.name)", role: .destructive) { Task { await deleteBot(bot) } }
        } message: { _ in Text("Its machine, files, memories and chats are deleted.") }
        .confirmationDialog("Delete \(space?.name ?? "this workspace")?", isPresented: $deleting, titleVisibility: .visible) {
            Button("Delete workspace", role: .destructive) { Task { await deleteSpace() } }
        }
    }

    @ViewBuilder
    private var membership: some View {
        if role == "owner" {
            if people.count > 1 {
                Section {
                    if admins.isEmpty {
                        Text("Make someone an admin first: only an admin can become the owner.")
                            .font(.silo(14)).foregroundStyle(Theme.ink2)
                    } else {
                        Picker("New owner", selection: $heir) {
                            ForEach(admins, id: \.userID) { Text($0.email).tag($0.userID) }
                        }
                        Button("Transfer ownership") { transferring = true }.disabled(heir.isEmpty)
                    }
                } header: { Text("Ownership") } footer: {
                    if !admins.isEmpty { Text("The admin you pick becomes the owner; you become an admin.") }
                }
            }
        } else if let me = people.first(where: \.you) {
            Section {
                Button("Leave workspace…", role: .destructive) { leaving = me }
            } header: { Text("Membership") } footer: {
                Text("You lose access to its Bots. Bots only you manage need a new manager first.")
            }
        }
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

    private func save() async {
        guard let space else { return }
        saving = true
        defer { saving = false }
        do {
            _ = try await model.client.updateSpace(id: space.id, name: name.trimmingCharacters(in: .whitespaces), mark: mark.trimmingCharacters(in: .whitespaces))
            await spaces.load()
        } catch { failure = describe(error) }
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
            others.removeAll { $0.id == bot.id }
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
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: 14) {
                        SpaceMarkView(mark: spaceMark(name: name, given: mark), size: 44)
                        VStack(spacing: 8) {
                            TextField("Acme labs", text: $name)
                            TextField("Letters on its tile", text: $mark)
                                .textInputAutocapitalization(.characters)
                                .onChange(of: mark) { _, value in if value.count > 2 { mark = String(value.prefix(2)) } }
                        }
                    }
                } footer: {
                    Text("A workspace holds Bots and what they share. Each Bot still has its own machine.")
                }
                if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
            }
            .siloList()
            .navigationTitle("New workspace")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(busy ? "Creating…" : "Create") { Task { await create() } }
                        .disabled(busy || name.trimmingCharacters(in: .whitespaces).isEmpty)
                }
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
