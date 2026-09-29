import SiloClient
import SwiftUI

/// The "Needs you" slip: a native sheet (bottom on iPhone, form sheet on iPad) with the Bot's
/// request and four choices. The run stays paused until one is picked.
struct ApprovalSheet: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot
    let approval: Silo_V1_Approval
    @State private var deciding = false

    var body: some View {
        let description = describeApproval(approval)
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 10) {
                    CrestView(index: bot.crest, size: 28)
                    Text(bot.name).font(.headline).lineLimit(1)
                    Spacer(minLength: 8)
                    StatusLine(status: "needs_you")
                }
                if !approval.source.isEmpty {
                    Text(approval.source)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.secondary)
                }
                Text(description.title)
                    .font(.title2.weight(.medium))
                Text(description.summary)
                    .font(.body)
                    .foregroundStyle(.secondary)
                if !description.fields.isEmpty {
                    VStack(spacing: 0) {
                        ForEach(Array(description.fields.enumerated()), id: \.offset) { index, field in
                            if index > 0 { Divider() }
                            VStack(alignment: .leading, spacing: 2) {
                                Text(field.label).font(.caption.weight(.medium)).foregroundStyle(.secondary)
                                Text(field.value)
                                    .font(.system(.callout, design: .monospaced))
                                    .textSelection(.enabled)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 8)
                        }
                    }
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control))
                }
            }
            .padding(20)
        }
        .safeAreaInset(edge: .bottom) { actions }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private var actions: some View {
        VStack(spacing: 10) {
            Button { decide("allow_once") } label: {
                Text("Allow once").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glassProminent)
            Button { decide("always") } label: {
                Text("Always allow this action").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glass)
            Button { decide("auto") } label: {
                Text("Auto-approve this action").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glass)
            Button(role: .destructive) { decide("deny") } label: {
                Text("Deny").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glass)
            .tint(Theme.vermilion)
            Text(approval.runID.isEmpty ? "The process is waiting for your choice." : "This run is paused until you choose.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.top, 2)
        }
        .controlSize(.large)
        .disabled(deciding)
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
        .sensoryFeedback(.selection, trigger: deciding)
    }

    private func decide(_ decision: String) {
        deciding = true
        Task {
            await model.decide(approval, decision)
            deciding = false
        }
    }
}

extension Silo_V1_Approval: @retroactive Identifiable {}
