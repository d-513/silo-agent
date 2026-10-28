import Foundation
import Observation
import SiloClient

/// The Workspaces the user is in and the one the app is in. The current one follows the open Bot;
/// with no Bot open it is the one last used here, else the first. The sidebar's crests are its
/// Bots, and switching opens its first Bot (or, with none, the Workspace page).
@MainActor
@Observable
final class SpaceModel {
    /// The UserDefaults key of the Workspace last used here (`AppModel.pickBot` reads it too).
    static let lastSpaceKey = "lastSpace"

    private(set) var spaces: [Silo_V1_Space] = []
    private(set) var loaded = false
    private(set) var currentID: String?
    private unowned let app: AppModel

    init(app: AppModel) {
        self.app = app
        currentID = UserDefaults.standard.string(forKey: Self.lastSpaceKey)
    }

    var current: Silo_V1_Space? {
        guard let currentID else { return nil }
        return spaces.first { $0.id == currentID }
    }

    /// The caller's role in the current Workspace ("" until the list has come).
    var role: String { current?.role ?? "" }

    func space(_ id: String) -> Silo_V1_Space? { spaces.first { $0.id == id } }

    /// The current Workspace's Bots the user is on, in the server's order.
    var bots: [Silo_V1_Bot] {
        guard let currentID else { return app.bots }
        return app.bots.filter { $0.spaceID == currentID }
    }

    /// The Bots of the current Workspace the user manages: the ones they can give something to.
    var managedBots: [Silo_V1_Bot] { bots.filter { $0.role == "manage" } }

    /// Lists the Workspaces; a remembered one that is gone (left, deleted) gives way to the first.
    func load() async {
        do {
            spaces = try await app.client.listSpaces()
            loaded = true
        } catch {
            await app.report(error)
            return
        }
        let id = pickSpace(ids: spaces.map(\.id), named: app.selectedBot?.spaceID, last: currentID)
        if id != currentID {
            if app.selectedBot == nil, !id.isEmpty { open(id) } else { remember(id) }
        }
    }

    /// Forgets the list (sign-out): the next user starts from their own.
    func reset() {
        spaces = []
        loaded = false
    }

    /// The open Bot's Workspace becomes the current one.
    func follow(_ bot: Silo_V1_Bot?) {
        guard let id = bot?.spaceID, !id.isEmpty, id != currentID else { return }
        remember(id)
    }

    /// Switches to a Workspace: its first Bot opens, or with none the Workspace page.
    func open(_ id: String) {
        remember(id)
        app.sidebarOpen = false
        let first = app.bots.first { $0.spaceID == id }
        if first?.id != app.selectedBotID { app.selectedBotID = first?.id }
        if first != nil { app.path = [] }
    }

    /// After a Workspace is deleted or left: the first other one.
    func gone(_ id: String) async {
        if currentID == id { currentID = nil }
        await app.refreshBots()
        await load()
        if currentID == nil || currentID == id, let next = spaces.first(where: { $0.id != id }) { open(next.id) }
    }

    private func remember(_ id: String) {
        currentID = id.isEmpty ? nil : id
        UserDefaults.standard.set(currentID, forKey: Self.lastSpaceKey)
    }
}
