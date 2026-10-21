import SiloClient
import SwiftUI

/// The "Needs you" slip: what the Bot wants to do and the choice. Docked in place of the composer
/// in the chat whose run it paused (`here`), and listed on the Dashboard with the rest. Allow once
/// and Deny are one tap; changing the rule is one tap further.
struct ApprovalSlip: View {
    @Environment(AppModel.self) private var model
    let approval: Silo_V1_Approval
    var here = false
    @State private var deciding = false
    @State private var details = false

    var body: some View {
        let description = describeApproval(approval)
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                StatusDot(status: "needs_you", size: 7)
                Text(source).lineLimit(1)
            }
            .font(.silo(12))
            .foregroundStyle(Theme.ink3)

            Text(description.title)
                .font(.silo(17, .medium))
                .foregroundStyle(Theme.ink)
            if !description.summary.isEmpty {
                Text(description.summary)
                    .font(.silo(14))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(details ? nil : 3)
            }
            if !description.fields.isEmpty {
                fields(description.fields)
            }

            HStack(spacing: 8) {
                Button { decide("allow_once") } label: {
                    Text(approval.auto ? "Allow" : "Allow once").frame(maxWidth: .infinity)
                }
                .buttonStyle(.ink)
                Button { decide("deny") } label: { Text("Deny").frame(maxWidth: .infinity) }
                    .buttonStyle(.quiet(tint: Theme.vermilion))
            }
            .padding(.top, 4)

            if approval.auto {
                Text("Auto-approval was not sure about this one. The rule is in Rules.")
                    .font(.silo(12))
                    .foregroundStyle(Theme.ink3)
            } else {
                Menu {
                    Button { decide("always") } label: { Label("Always allow this action", systemImage: "checkmark.circle") }
                    Button { decide("auto") } label: { Label("Auto-approve this action", systemImage: "sparkles") }
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
        }
        .disabled(deciding)
        .padding(.leading, 18)
        .padding([.trailing, .vertical], 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(alignment: .leading) { Theme.vermilion.frame(width: 3) }
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
        .shadow(color: .black.opacity(here ? 0.08 : 0), radius: 16, y: 6)
        .sensoryFeedback(.selection, trigger: deciding)
    }

    private var source: String {
        var parts = ["Needs you"]
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
                    Text(field.label).font(.silo(11, .medium)).foregroundStyle(Theme.ink3)
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
        .onTapGesture { withAnimation(.snappy) { details.toggle() } }
        if !details && (list.count > 2 || list.contains { $0.value.count > 160 }) {
            Button { withAnimation(.snappy) { details = true } } label: {
                Text("Show everything").font(.silo(12, .medium)).foregroundStyle(Theme.ink2)
            }
            .buttonStyle(.plain)
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
