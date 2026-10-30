import ActivityKit
import SiloClient
import SwiftUI

/// Keeps a Live Activity for the Bot at work. An activity can only be started while the app is in
/// front, so it starts with the run and the Island shows it once you leave. Silo has no push
/// service, so when the app is suspended it goes stale, and says so.
@MainActor
final class WorkTracker {
    static let shared = WorkTracker()
    private var activity: Activity<WorkActivityAttributes>?
    private var shownFor: String?

    func update(_ model: AppModel) {
        guard let bot = model.selectedBot, !CallModel.shared.live else { return finish() }
        let row = bot.activity.first { $0.kind != "call" && !$0.chatID.isEmpty && $0.state != "on_call" }
        let approval = model.approvals.first
        guard let row, ActivityAuthorizationInfo().areActivitiesEnabled else { return finish() }
        let content = WorkActivityAttributes.ContentState(
            look: runLook(Doing(row)), step: progress(row), approvalID: approval?.id ?? "",
            approvalTitle: approval.map { describeApproval($0).title } ?? "", done: false)
        let next = ActivityContent(state: content, staleDate: Date().addingTimeInterval(180))
        if let activity, shownFor == bot.id {
            Task { await activity.update(next) }
        } else {
            finish()
            let attributes = WorkActivityAttributes(botID: bot.id, botName: bot.name, crest: bot.crest, chatID: row.leadChatID.isEmpty ? row.chatID : row.leadChatID)
            activity = try? Activity.request(attributes: attributes, content: next)
            shownFor = bot.id
        }
    }

    private func finish() {
        guard let activity else { return }
        self.activity = nil
        shownFor = nil
        Task { await activity.end(nil, dismissalPolicy: .immediate) }
    }
}
