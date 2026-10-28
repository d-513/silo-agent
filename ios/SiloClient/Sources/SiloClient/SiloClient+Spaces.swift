import Foundation
import SwiftProtobuf

// Typed calls for Workspaces: the Workspaces themselves, their people and invite links, the people
// on a Bot, what a Workspace shares with its Bots (connections, drives, skills), its rules and
// activity, and moving a Bot between Workspaces.
extension SiloClient {
    // MARK: - Workspaces

    /// The caller's Workspaces; listing makes their first one, so it is never empty once it answers.
    public func listSpaces() async throws -> [Silo_V1_Space] {
        try unwrap(await ui.listSpaces(request: Silo_V1_ListSpacesRequest(), headers: [:])).spaces
    }

    public func createSpace(name: String, mark: String) async throws -> Silo_V1_Space {
        try unwrap(await ui.createSpace(request: .with { $0.name = name; $0.mark = mark }, headers: [:]))
    }

    public func updateSpace(id: String, name: String, mark: String) async throws -> Silo_V1_Space {
        try unwrap(await ui.updateSpace(request: .with { $0.id = id; $0.name = name; $0.mark = mark }, headers: [:]))
    }

    public func deleteSpace(id: String) async throws {
        _ = try unwrap(await ui.deleteSpace(request: .with { $0.id = id }, headers: [:]))
    }

    public func transferSpace(spaceID: String, userID: String) async throws -> Silo_V1_Space {
        try unwrap(await ui.transferSpace(request: .with { $0.spaceID = spaceID; $0.userID = userID }, headers: [:]))
    }

    /// One Workspace's Bots the caller is on, and (for its admins) the ones they are not.
    public func listBots(spaceID: String) async throws -> Silo_V1_ListBotsResponse {
        try unwrap(await ui.listBots(request: .with { $0.spaceID = spaceID }, headers: [:]))
    }

    /// Makes a Bot as the request says; the Bot starts in this device's time zone.
    public func createBot(_ request: Silo_V1_CreateBotRequest) async throws -> Silo_V1_Bot {
        var request = request
        if request.timeZone.isEmpty { request.timeZone = TimeZone.current.identifier }
        return try unwrap(await ui.createBot(request: request, headers: [:]))
    }

    // MARK: - People

    public func listSpacePeople(spaceID: String) async throws -> Silo_V1_ListSpacePeopleResponse {
        try unwrap(await ui.listSpacePeople(request: .with { $0.spaceID = spaceID }, headers: [:]))
    }

    public func setSpaceRole(spaceID: String, userID: String, role: String) async throws -> Silo_V1_SpacePerson {
        try unwrap(await ui.setSpaceRole(request: .with {
            $0.spaceID = spaceID
            $0.userID = userID
            $0.role = role
        }, headers: [:]))
    }

    public func planSpaceRemoval(spaceID: String, userID: String) async throws -> Silo_V1_SpaceRemovalPlan {
        try unwrap(await ui.planSpaceRemoval(request: .with { $0.spaceID = spaceID; $0.userID = userID }, headers: [:]))
    }

    public func removeSpacePerson(spaceID: String, userID: String, plan: [Silo_V1_Handoff]) async throws {
        _ = try unwrap(await ui.removeSpacePerson(request: .with {
            $0.spaceID = spaceID
            $0.userID = userID
            $0.plan = plan
        }, headers: [:]))
    }

    public func createSpaceInvite(spaceID: String, role: String) async throws -> Silo_V1_SpaceInviteLink {
        try unwrap(await ui.createSpaceInvite(request: .with { $0.spaceID = spaceID; $0.role = role }, headers: [:]))
    }

    public func listSpaceInvites(spaceID: String) async throws -> [Silo_V1_SpaceInvite] {
        try unwrap(await ui.listSpaceInvites(request: .with { $0.spaceID = spaceID }, headers: [:])).invites
    }

    public func deleteSpaceInvite(spaceID: String, id: String) async throws {
        _ = try unwrap(await ui.deleteSpaceInvite(request: .with { $0.spaceID = spaceID; $0.id = id }, headers: [:]))
    }

    // MARK: - People on a Bot

    public func listBotPeople(botID: String) async throws -> Silo_V1_ListBotPeopleResponse {
        try unwrap(await ui.listBotPeople(request: .with { $0.botID = botID }, headers: [:]))
    }

    /// Adds someone to a Bot. When they could have it use secrets someone else set, nobody is added
    /// and the answer names those secrets: send again with `confirmed`.
    public func addBotPerson(botID: String, userID: String, role: String, confirmed: Bool) async throws -> Silo_V1_AddBotPersonResponse {
        try unwrap(await ui.addBotPerson(request: .with {
            $0.botID = botID
            $0.userID = userID
            $0.role = role
            $0.confirmed = confirmed
        }, headers: [:]))
    }

    public func setBotRole(botID: String, userID: String, role: String) async throws {
        _ = try unwrap(await ui.setBotRole(request: .with {
            $0.botID = botID
            $0.userID = userID
            $0.role = role
        }, headers: [:]))
    }

    public func removeBotPerson(botID: String, userID: String) async throws {
        _ = try unwrap(await ui.removeBotPerson(request: .with { $0.botID = botID; $0.userID = userID }, headers: [:]))
    }

    // MARK: - What the Workspace shares

    public func listSpaceConnectors(spaceID: String) async throws -> [Silo_V1_Connector] {
        try unwrap(await ui.listSpaceConnectors(request: .with { $0.spaceID = spaceID }, headers: [:])).connectors
    }

    /// A new connection in a Workspace from a library preset, granted to `botIDs`. The preset's
    /// target, headers and environment come along on the server.
    public func addLibraryConnector(spaceID: String, preset: Silo_V1_Connector, name: String, botIDs: [String]) async throws -> Silo_V1_BotConnector {
        try unwrap(await ui.createBotConnector(request: .with {
            $0.spaceID = spaceID
            $0.botIds = botIDs
            $0.sourceID = preset.id
            $0.name = name
            $0.description_p = preset.description_p
            $0.transport = preset.transport
            $0.httpURL = preset.httpURL
            $0.auth = preset.auth
            $0.defaultMode = preset.defaultMode
            $0.category = preset.category
        }, headers: [:]))
    }

    public func deleteConnector(id: String) async throws {
        _ = try unwrap(await ui.deleteConnector(request: .with { $0.id = id }, headers: [:]))
    }

    public func grantConnector(botID: String, connectorID: String) async throws {
        _ = try unwrap(await ui.grantConnector(request: .with { $0.botID = botID; $0.connectorID = connectorID }, headers: [:]))
    }

    public func revokeConnector(botID: String, connectorID: String) async throws {
        _ = try unwrap(await ui.revokeConnector(request: .with { $0.botID = botID; $0.connectorID = connectorID }, headers: [:]))
    }

    /// A lender's say-so on a connection or drive that carries their identity.
    public func setLendShared(connectorID: String = "", driveID: String = "", shared: Bool) async throws {
        _ = try unwrap(await ui.setLendShared(request: .with {
            $0.connectorID = connectorID
            $0.driveID = driveID
            $0.shared = shared
        }, headers: [:]))
    }

    public func listSpaceDrives(spaceID: String, draftID: String = "") async throws -> [Silo_V1_Drive] {
        try unwrap(await ui.listSpaceDrives(request: .with { $0.spaceID = spaceID; $0.draftID = draftID }, headers: [:])).drives
    }

    public func grantDrive(botID: String, driveID: String) async throws {
        _ = try unwrap(await ui.grantDrive(request: .with { $0.botID = botID; $0.driveID = driveID }, headers: [:]))
    }

    public func revokeDrive(botID: String, driveID: String) async throws {
        _ = try unwrap(await ui.revokeDrive(request: .with { $0.botID = botID; $0.driveID = driveID }, headers: [:]))
    }

    /// The sign-in address for a drive; `shared` is who may use the sign-in (see SetLendShared).
    public func beginDriveAuth(id: String, shared: Bool) async throws -> String {
        try unwrap(await ui.beginDriveAuth(request: .with { $0.id = id; $0.shared = shared }, headers: [:])).url
    }

    public func listSkills(scope: String, spaceID: String) async throws -> [Silo_V1_Skill] {
        try unwrap(await ui.listSkills(request: .with { $0.scope = scope; $0.spaceID = spaceID }, headers: [:])).skills
    }

    public func installSkill(spaceID: String, url: String = "", archive: Data = Data(), filename: String = "") async throws -> Silo_V1_InstallSkillResponse {
        try unwrap(await ui.installSkill(request: .with {
            $0.scope = "space"
            $0.spaceID = spaceID
            $0.url = url
            $0.archive = archive
            $0.filename = filename
        }, headers: [:]))
    }

    public func deleteSkill(spaceID: String, name: String) async throws {
        _ = try unwrap(await ui.deleteSkill(request: .with {
            $0.scope = "space"
            $0.spaceID = spaceID
            $0.name = name
        }, headers: [:]))
    }

    public func listSkillFiles(scope: String, name: String, path: String, spaceID: String) async throws -> [Silo_V1_FileEntry] {
        try unwrap(await ui.listSkillFiles(request: .with {
            $0.scope = scope
            $0.name = name
            $0.path = path
            $0.spaceID = spaceID
        }, headers: [:])).entries
    }

    public func readSkillFile(scope: String, name: String, path: String, spaceID: String) async throws -> Silo_V1_ReadFileResponse {
        try unwrap(await ui.readSkillFile(request: .with {
            $0.scope = scope
            $0.name = name
            $0.path = path
            $0.spaceID = spaceID
        }, headers: [:]))
    }

    // MARK: - Rules and activity

    public func listSpaceRules(spaceID: String) async throws -> Silo_V1_ListSpaceRulesResponse {
        try unwrap(await ui.listSpaceRules(request: .with { $0.spaceID = spaceID }, headers: [:]))
    }

    /// Sets both sides of one action's floor (the side not being changed is sent as it stands).
    public func setSpaceRule(spaceID: String, connector: String, action: String, decision: String, output: String) async throws {
        _ = try unwrap(await ui.setSpaceRule(request: .with {
            $0.spaceID = spaceID
            $0.connector = connector
            $0.action = action
            $0.decision = decision
            $0.output = output
        }, headers: [:]))
    }

    public func updateSpacePolicy(spaceID: String, approval: String, output: String) async throws -> Silo_V1_ListSpaceRulesResponse {
        try unwrap(await ui.updateSpacePolicy(request: .with {
            $0.spaceID = spaceID
            $0.approvalPolicy = approval
            $0.outputPolicy = output
        }, headers: [:]))
    }

    public func listSpaceActivity(spaceID: String) async throws -> [Silo_V1_SpaceActivity] {
        try unwrap(await ui.listSpaceActivity(request: .with { $0.spaceID = spaceID }, headers: [:])).events
    }

    // MARK: - Moving a Bot

    public func planMoveBot(botID: String, spaceID: String) async throws -> Silo_V1_MoveBotPlan {
        try unwrap(await ui.planMoveBot(request: .with { $0.botID = botID; $0.spaceID = spaceID }, headers: [:]))
    }

    public func moveBot(botID: String, spaceID: String) async throws -> Silo_V1_Bot {
        try unwrap(await ui.moveBot(request: .with { $0.botID = botID; $0.spaceID = spaceID }, headers: [:]))
    }
}
