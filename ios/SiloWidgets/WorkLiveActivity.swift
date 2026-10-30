import AppIntents
import ActivityKit
import SiloClient
import SwiftUI
import WidgetKit

/// A Bot at work while you are elsewhere: the task and its place on the board, and an approval it is
/// paused on, answered from the Island. A stale one says so rather than pretending.
struct WorkLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: WorkActivityAttributes.self) { context in
            LockWork(context: context)
                .activityBackgroundTint(Theme.hatch)
                .activitySystemActionForegroundColor(Theme.onHatch)
        } dynamicIsland: { context in
            let state = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    CrestView(index: context.attributes.crest, size: 36).padding(.leading, 4)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(context.attributes.botName).font(.silo(14, .semibold)).foregroundStyle(Theme.onHatch).lineLimit(1)
                        WorkLine(state: state, stale: context.isStale)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    if !state.approvalID.isEmpty { Answer(state: state) }
                }
            } compactLeading: {
                CrestView(index: context.attributes.crest, size: 22)
            } compactTrailing: {
                if !state.approvalID.isEmpty {
                    Image(systemName: "hand.raised.fill").font(.system(size: 12)).foregroundStyle(Theme.vermilionOnHatch)
                } else if !state.step.isEmpty {
                    Text(state.step).font(.siloMono(11)).foregroundStyle(Theme.onHatch)
                } else {
                    Circle().fill(Color(hex: 0x2FB880)).frame(width: 8, height: 8)
                }
            } minimal: {
                CrestView(index: context.attributes.crest, size: 20)
            }
            .widgetURL(SiloLink.chat(bot: context.attributes.botID, chat: context.attributes.chatID).url)
        }
    }
}

private struct WorkLine: View {
    let state: WorkActivityAttributes.ContentState
    let stale: Bool

    var body: some View {
        HStack(spacing: 6) {
            Text(stale ? "Open Silo to catch up" : state.look.word)
                .font(.silo(12.5))
                .foregroundStyle(state.approvalID.isEmpty ? Theme.onHatch.opacity(0.75) : Theme.vermilionOnHatch)
                .lineLimit(1)
            if !state.step.isEmpty && !stale { Text(state.step).font(.siloMono(11.5)).foregroundStyle(Theme.onHatch.opacity(0.6)) }
        }
    }
}

private struct Answer: View {
    let state: WorkActivityAttributes.ContentState

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(state.approvalTitle).font(.silo(13.5, .medium)).foregroundStyle(Theme.onHatch).lineLimit(1)
            HStack(spacing: 8) {
                Button(intent: DecideApprovalIntent(approvalID: state.approvalID, decision: "allow_once")) {
                    Text("Allow once").font(.silo(13, .medium)).foregroundStyle(Color(hex: 0x141413))
                        .frame(maxWidth: .infinity).frame(height: 36)
                        .background(Theme.onHatch, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                }
                Button(intent: DecideApprovalIntent(approvalID: state.approvalID, decision: "deny")) {
                    Text("Deny").font(.silo(13, .medium)).foregroundStyle(Theme.vermilionOnHatch)
                        .frame(maxWidth: .infinity).frame(height: 36)
                        .background(Theme.onHatch.opacity(0.14), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                }
            }
            .buttonStyle(.plain)
        }
    }
}

private struct LockWork: View {
    let context: ActivityViewContext<WorkActivityAttributes>

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                CrestView(index: context.attributes.crest, size: 38)
                VStack(alignment: .leading, spacing: 1) {
                    Text(context.attributes.botName).font(.silo(15, .semibold)).foregroundStyle(Theme.onHatch).lineLimit(1)
                    WorkLine(state: context.state, stale: context.isStale)
                }
                Spacer(minLength: 0)
            }
            if !context.state.approvalID.isEmpty { Answer(state: context.state) }
        }
        .padding(16)
    }
}
