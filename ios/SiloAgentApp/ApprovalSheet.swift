import SiloClient
import SwiftUI

/// The "Needs you" slip: what the Bot wants to do and the choice. Docked in place of the composer in
/// the chat whose run it paused (`here`), and listed on the Dashboard with the rest. Allow once and
/// Deny are one press; changing the rule is one further.
struct ApprovalSlip: View {
    @Environment(AppModel.self) private var model
    let approval: Silo_V1_Approval
    var here = false
    @State private var deciding = false
    @State private var details = false

    var body: some View {
        let description = describeApproval(approval)
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.panel, style: .continuous)
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 7) {
                if let bot = model.bot(approval.botID) { CrestView(index: bot.crest, size: 20) }
                Lamp(status: .needsYou)
                Text(source).font(.silo(12.5, .medium)).foregroundStyle(Theme.vermilion).lineLimit(1)
            }
            Text(description.title).font(.silo(19, .medium)).foregroundStyle(Theme.ink)
            if !description.summary.isEmpty {
                Text(description.summary)
                    .font(.silo(14))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(details ? nil : 3)
            }
            if !description.fields.isEmpty { fields(description.fields) }
            HStack(spacing: 8) {
                Button { decide("allow_once") } label: {
                    Text(approval.auto ? "Allow" : "Allow once").frame(maxWidth: .infinity)
                }
                .buttonStyle(.ink)
                Button { decide("deny") } label: { Text("Deny").frame(maxWidth: .infinity) }
                    .buttonStyle(.ghost(tint: Theme.vermilion))
            }
            .padding(.top, 2)
            foot
        }
        .disabled(deciding)
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(Theme.line))
        .slipShadow()
        .sensoryFeedback(.warning, trigger: approval.id)
        .sensoryFeedback(.selection, trigger: deciding)
    }

    @ViewBuilder
    private var foot: some View {
        if approval.auto {
            Text("Auto-approval was not sure about this one.")
                .font(.silo(12.5))
                .foregroundStyle(Theme.ink3)
                .frame(maxWidth: .infinity)
        } else if !approval.onceOnly {
            // Someone who uses the Bot but does not manage it answers their own chat's call once:
            // Always would change a rule, which is the managers'.
            SiloMenu {
                MenuRow(title: "Always allow this action", symbol: "checkmark.shield") { decide("always") }
                MenuRow(title: "Auto-approve this action", symbol: "sparkles") { decide("auto") }
            } label: {
                HStack(spacing: 4) {
                    Text("Change the rule")
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 9, weight: .semibold))
                }
                .font(.silo(13, .medium))
                .foregroundStyle(Theme.ink2)
                .frame(maxWidth: .infinity)
                .frame(height: 30)
            }
        }
        HStack(spacing: 7) {
            Circle().fill(Theme.vermilion).frame(width: 6, height: 6).breathing()
            Text("This run is paused until you choose.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
        }
        .frame(maxWidth: .infinity)
    }

    private var source: String {
        var parts = ["Needs you"]
        // On a Bot several people use: whose chat asked.
        if !approval.person.isEmpty { parts.append(approval.person) }
        if here { parts.append("this chat") } else if !approval.source.isEmpty { parts.append(approval.source) }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder
    private func fields(_ list: [ApprovalDescription.Field]) -> some View {
        let shown = details ? list : Array(list.prefix(2))
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(shown.enumerated()), id: \.offset) { index, field in
                if index > 0 { Theme.line.frame(height: 1) }
                VStack(alignment: .leading, spacing: 2) {
                    Text(field.label).font(.silo(11.5, .medium)).foregroundStyle(Theme.ink3)
                    Text(field.value)
                        .font(.siloMono(12.5))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(details ? nil : 4)
                        .textSelection(.enabled)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 10)
                .padding(.vertical, 7)
            }
        }
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        .onTapGesture { withAnimation(Motion.fold) { details.toggle() } }
        if !details && (list.count > 2 || list.contains { $0.value.count > 160 }) {
            Button { withAnimation(Motion.fold) { details = true } } label: {
                Text("Show everything").font(.silo(12.5, .medium)).foregroundStyle(Theme.ink2)
            }
            .buttonStyle(.press)
        }
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
