import Foundation
import SwiftProtobuf

// Typed calls for the Bot pages added after the first parity pass: Dashboard, Mail, Knowledge,
// Changes, Tunnels, Artifacts, Audit, the time zone, and the account's own settings.
extension SiloClient {
    // MARK: - Dashboard

    public func listDashboard(botID: String) async throws -> Silo_V1_ListDashboardResponse {
        try unwrap(await ui.listDashboard(request: .with { $0.botID = botID }, headers: [:]))
    }

    public func markDashboardRead(botID: String) async throws {
        _ = try unwrap(await ui.markDashboardRead(request: .with { $0.botID = botID }, headers: [:]))
    }

    public func refreshWidget(botID: String, id: String) async throws -> Silo_V1_DashboardWidget {
        try unwrap(await ui.refreshWidget(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func setWidgetState(botID: String, id: String, stateJSON: String) async throws -> Silo_V1_DashboardWidget {
        try unwrap(await ui.setWidgetState(request: .with {
            $0.botID = botID
            $0.id = id
            $0.stateJson = stateJSON
        }, headers: [:]))
    }

    public func runWidgetAction(botID: String, id: String, action: String, argsJSON: String) async throws -> Silo_V1_RunWidgetActionResponse {
        try unwrap(await ui.runWidgetAction(request: .with {
            $0.botID = botID
            $0.id = id
            $0.action = action
            $0.argsJson = argsJSON
        }, headers: [:]))
    }

    public func removeWidget(botID: String, id: String) async throws {
        _ = try unwrap(await ui.removeWidget(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func arrangeDashboard(botID: String, places: [Silo_V1_WidgetPlace]) async throws {
        _ = try unwrap(await ui.arrangeDashboard(request: .with { $0.botID = botID; $0.places = places }, headers: [:]))
    }

    public func askDashboard(botID: String, text: String, widgetID: String = "") async throws -> Silo_V1_AskDashboardResponse {
        try unwrap(await ui.askDashboard(request: .with {
            $0.botID = botID
            $0.text = text
            $0.widgetID = widgetID
        }, headers: [:]))
    }

    public func dismissDashboardCard(botID: String, id: String, suppressKind: Bool) async throws {
        _ = try unwrap(await ui.dismissDashboardCard(request: .with {
            $0.botID = botID
            $0.id = id
            $0.suppressKind = suppressKind
        }, headers: [:]))
    }

    public func submitDashboardCard(botID: String, id: String, valuesJSON: String, note: String, newChat: Bool = false) async throws -> Silo_V1_SubmitDashboardCardResponse {
        try unwrap(await ui.submitDashboardCard(request: .with {
            $0.botID = botID
            $0.id = id
            $0.valuesJson = valuesJSON
            $0.note = note
            $0.newChat = newChat
        }, headers: [:]))
    }

    // MARK: - Mail

    public func listMail(botID: String) async throws -> Silo_V1_ListMailResponse {
        try unwrap(await ui.listMail(request: .with { $0.botID = botID }, headers: [:]))
    }

    public func getMail(botID: String, id: String) async throws -> Silo_V1_Mail {
        try unwrap(await ui.getMail(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func deleteMail(botID: String, id: String) async throws {
        _ = try unwrap(await ui.deleteMail(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func updateMailbox(botID: String, wake: Bool, wakeFrom: String) async throws -> Silo_V1_Mailbox {
        try unwrap(await ui.updateMailbox(request: .with {
            $0.botID = botID
            $0.wake = wake
            $0.wakeFrom = wakeFrom
        }, headers: [:]))
    }

    public func rotateMailbox(botID: String) async throws -> Silo_V1_Mailbox {
        try unwrap(await ui.rotateMailbox(request: .with { $0.botID = botID }, headers: [:]))
    }

    /// The path of a message's original `.eml` (always an attachment).
    public func rawMailPath(botID: String, id: String) -> String {
        "/mail/raw?" + query([("bot_id", botID), ("id", id)])
    }

    // MARK: - Knowledge

    public func listKnowledge(botID: String) async throws -> Silo_V1_ListKnowledgeResponse {
        try unwrap(await ui.listKnowledge(request: .with { $0.botID = botID }, headers: [:]))
    }

    public func addKnowledgeFolder(botID: String, path: String) async throws -> Silo_V1_KnowledgeFolder {
        try unwrap(await ui.addKnowledgeFolder(request: .with { $0.botID = botID; $0.path = path }, headers: [:]))
    }

    public func removeKnowledgeFolder(botID: String, id: String) async throws {
        _ = try unwrap(await ui.removeKnowledgeFolder(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func syncKnowledge(botID: String, id: String) async throws -> Silo_V1_KnowledgeFolder {
        try unwrap(await ui.syncKnowledge(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func searchKnowledge(botID: String, query: String) async throws -> [Silo_V1_KnowledgeHit] {
        try unwrap(await ui.searchKnowledge(request: .with {
            $0.botID = botID
            $0.query = query
            $0.limit = 12
        }, headers: [:])).hits
    }

    // MARK: - Changes

    public func listChanges(botID: String) async throws -> Silo_V1_ListChangesResponse {
        try unwrap(await ui.listChanges(request: .with { $0.botID = botID }, headers: [:]))
    }

    public func listChangeFiles(botID: String, base: String, head: String) async throws -> Silo_V1_ListChangeFilesResponse {
        try unwrap(await ui.listChangeFiles(request: .with {
            $0.botID = botID
            $0.base = base
            $0.head = head
        }, headers: [:]))
    }

    public func getChangePatch(botID: String, base: String, head: String, file: Silo_V1_ChangeFile) async throws -> Silo_V1_GetChangePatchResponse {
        try unwrap(await ui.getChangePatch(request: .with {
            $0.botID = botID
            $0.base = base
            $0.head = head
            $0.path = file.path
            $0.oldPath = file.oldPath
        }, headers: [:]))
    }

    /// Puts files back as they were before a change: one file, or every file when `file` is nil.
    public func restoreChange(botID: String, base: String, head: String, file: Silo_V1_ChangeFile? = nil) async throws -> Silo_V1_RestoreChangeResponse {
        try unwrap(await ui.restoreChange(request: .with {
            $0.botID = botID
            $0.base = base
            $0.head = head
            $0.path = file?.path ?? ""
            $0.oldPath = file?.oldPath ?? ""
        }, headers: [:]))
    }

    public func listDriveChanges(botID: String) async throws -> Silo_V1_ListDriveChangesResponse {
        try unwrap(await ui.listDriveChanges(request: .with { $0.botID = botID }, headers: [:]))
    }

    // MARK: - Tunnels

    public func listTunnels(botID: String) async throws -> Silo_V1_ListTunnelsResponse {
        try unwrap(await ui.listTunnels(request: .with { $0.botID = botID }, headers: [:]))
    }

    public func createTunnel(botID: String, port: Int32, isPublic: Bool) async throws -> Silo_V1_Tunnel {
        try unwrap(await ui.createTunnel(request: .with {
            $0.botID = botID
            $0.port = port
            $0.public = isPublic
        }, headers: [:]))
    }

    public func updateTunnel(botID: String, id: String, isPublic: Bool) async throws -> Silo_V1_Tunnel {
        try unwrap(await ui.updateTunnel(request: .with {
            $0.botID = botID
            $0.id = id
            $0.public = isPublic
        }, headers: [:]))
    }

    public func deleteTunnel(botID: String, id: String) async throws {
        _ = try unwrap(await ui.deleteTunnel(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    public func listArtifactShares(botID: String) async throws -> [Silo_V1_ArtifactShare] {
        try unwrap(await ui.listArtifactShares(request: .with { $0.botID = botID }, headers: [:])).shares
    }

    public func deleteArtifactShare(botID: String, id: String) async throws {
        _ = try unwrap(await ui.deleteArtifactShare(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    // MARK: - Artifacts

    public func listArtifacts(botID: String) async throws -> [Silo_V1_ArtifactItem] {
        try unwrap(await ui.listArtifacts(request: .with { $0.botID = botID }, headers: [:])).artifacts
    }

    public func deleteArtifact(botID: String, id: String) async throws {
        _ = try unwrap(await ui.deleteArtifact(request: .with { $0.botID = botID; $0.id = id }, headers: [:]))
    }

    /// A signed address for a live app artifact on its own origin (no cookie needed to open it).
    public func openArtifactApp(botID: String, artifactID: String, version: Int32 = 0) async throws -> String {
        try unwrap(await ui.openArtifactApp(request: .with {
            $0.botID = botID
            $0.artifactID = artifactID
            $0.version = version
        }, headers: [:])).url
    }

    // MARK: - Audit

    public func auditSummary(botID: String) async throws -> Silo_V1_AuditSummaryResponse {
        try unwrap(await ui.auditSummary(request: .with { $0.botID = botID }, headers: [:]))
    }

    /// `view` is empty for everything or `attention` for what was flagged for a human.
    public func listAuditEvents(botID: String, view: String, query: String = "", cursor: String = "") async throws -> Silo_V1_ListAuditEventsResponse {
        try unwrap(await ui.listAuditEvents(request: .with {
            $0.botID = botID
            $0.view = view
            $0.query = query
            $0.cursor = cursor
            $0.limit = 50
        }, headers: [:]))
    }

    public func markAuditRead(botID: String) async throws {
        _ = try unwrap(await ui.markAuditRead(request: .with { $0.botID = botID }, headers: [:]))
    }

    // MARK: - Rules (both sides)

    public func listRules(botID: String) async throws -> Silo_V1_ListRulesResponse {
        try unwrap(await ui.listRules(request: .with { $0.botID = botID }, headers: [:]))
    }

    /// Sets only the output side of a rule (what happens to the action's result).
    public func setRuleOutput(botID: String, connector: String, action: String, output: String) async throws {
        _ = try unwrap(await ui.setRule(request: .with {
            $0.botID = botID
            $0.connector = connector
            $0.action = action
            $0.output = output
        }, headers: [:]))
    }

    public func setOutputPolicy(_ bot: Silo_V1_Bot, policy: String) async throws -> Silo_V1_Bot {
        // ListBots leaves SOUL and the output policy out: send what the Bot has now, not that.
        let bot = try await getBot(bot.id)
        return try unwrap(await ui.updateBot(request: .with {
            $0.id = bot.id
            $0.name = bot.name
            $0.description_p = bot.description_p
            $0.soul = bot.soul
            $0.autoApprove = bot.autoApprove
            $0.model = bot.model
            $0.outputPolicy = policy
        }, headers: [:]))
    }

    // MARK: - Bot time zone and mail settings

    /// Pins the Bot to `zone`, or lets it follow the owner's device again (`pinned` false).
    public func setBotTimeZone(_ bot: Silo_V1_Bot, zone: String, pinned: Bool) async throws -> Silo_V1_Bot {
        // ListBots leaves SOUL and the output policy out: send what the Bot has now, not that.
        let bot = try await getBot(bot.id)
        return try unwrap(await ui.updateBot(request: .with {
            $0.id = bot.id
            $0.name = bot.name
            $0.description_p = bot.description_p
            $0.soul = bot.soul
            $0.autoApprove = bot.autoApprove
            $0.model = bot.model
            $0.timeZone = zone
            $0.timeZonePinned = pinned
        }, headers: [:]))
    }

    // MARK: - Account

    public func changePassword(current: String, password: String) async throws -> Silo_V1_User {
        try unwrap(await ui.changePassword(request: .with { $0.current = current; $0.password = password }, headers: [:]))
    }

    public func changeEmail(_ email: String) async throws -> Silo_V1_User {
        try unwrap(await ui.changeEmail(request: .with { $0.email = email }, headers: [:]))
    }

    public func listSessions() async throws -> [Silo_V1_Session] {
        try unwrap(await ui.listSessions(request: Silo_V1_ListSessionsRequest(), headers: [:])).sessions
    }

    public func revokeSession(id: String) async throws -> [Silo_V1_Session] {
        try unwrap(await ui.revokeSession(request: .with { $0.id = id }, headers: [:])).sessions
    }

    public func revokeOtherSessions() async throws -> [Silo_V1_Session] {
        try unwrap(await ui.revokeOtherSessions(request: Silo_V1_RevokeOtherSessionsRequest(), headers: [:])).sessions
    }

    public func startTOTP(password: String) async throws -> Silo_V1_StartTOTPResponse {
        try unwrap(await ui.startTotp(request: .with { $0.password = password }, headers: [:]))
    }

    public func confirmTOTP(code: String) async throws -> Silo_V1_RecoveryCodes {
        try unwrap(await ui.confirmTotp(request: .with { $0.code = code }, headers: [:]))
    }

    public func disableTOTP(code: String) async throws -> Silo_V1_User {
        try unwrap(await ui.disableTotp(request: .with { $0.code = code }, headers: [:]))
    }

    public func newRecoveryCodes(code: String) async throws -> Silo_V1_RecoveryCodes {
        try unwrap(await ui.newRecoveryCodes(request: .with { $0.code = code }, headers: [:]))
    }
}
