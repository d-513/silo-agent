import SiloClient
import SwiftUI

extension Silo_V1_Space: @retroactive Identifiable {}

/// The letters of a Workspace on a `well` square with a `line` ring: the sidebar's tile, the
/// switcher's rows, the Workspace page's head.
struct SpaceMarkView: View {
    let mark: String
    var size: CGFloat = 32

    var body: some View {
        Text(mark)
            .font(.silo(size >= 32 ? 13 : 11, .semibold))
            .tracking(0.3)
            .foregroundStyle(Theme.ink)
            .frame(width: size, height: size)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: max(6, size * 0.22), style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: max(6, size * 0.22), style: .continuous).stroke(Theme.line))
            .accessibilityHidden(true)
    }
}

/// A small `well` chip: a role, Personal or Shared, Read-only.
struct WellChip: View {
    let text: String
    var tone: Color = Theme.ink2

    var body: some View { Chip(text: text, tint: tone) }
}

/// "Used by" and the crests of the Bots a connection, drive or skill is on, five at most, then a
/// count. The names are for VoiceOver.
struct UsedByLine: View {
    @Environment(AppModel.self) private var model
    let ids: [String]
    var hidden: Int32 = 0

    var body: some View {
        let shown = ids.compactMap { id in model.bots.first { $0.id == id } }
        let more = max(0, shown.count - 5) + Int(hidden) + (ids.count - shown.count)
        HStack(spacing: 6) {
            if shown.isEmpty && more == 0 {
                Text("No Bot uses it").font(.silo(12))
            } else {
                Text("Used by").font(.silo(11.5, .medium))
                ForEach(shown.prefix(5), id: \.id) { CrestView(index: $0.crest, size: 20) }
                if more > 0 { Text("+\(more)").font(.silo(11.5, .medium)).monospacedDigit() }
            }
        }
        .foregroundStyle(Theme.ink3)
        .lineLimit(1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(shown.isEmpty && more == 0
            ? "No Bot uses it"
            : "Used by " + (shown.map(\.name) + (more > 0 ? ["\(more) more"] : [])).joined(separator: ", "))
    }
}

/// Choose Bots: a switch per Bot of the Workspace the user manages; each one gives the thing to
/// that Bot or takes it away at once.
struct ChooseBotsSection: View {
    @Environment(SpaceModel.self) private var spaces
    let granted: [String]
    let toggle: (String, Bool) async -> Void
    @State private var busy: String?

    var body: some View {
        PanelSection(title: "Choose Bots", footer: "Each switch gives it to that Bot or takes it away at once.") {
            if spaces.managedBots.isEmpty {
                Text("You manage no Bot in this workspace.")
                    .font(.silo(14))
                    .foregroundStyle(Theme.ink3)
                    .padding(14)
            }
            ForEach(spaces.managedBots, id: \.id) { bot in
                HStack(spacing: 10) {
                    CrestView(index: bot.crest, size: 22)
                    Text(bot.name)
                        .font(.silo(15))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if busy == bot.id { Spinner(size: 14) }
                    Toggle(bot.name, isOn: Binding(get: { granted.contains(bot.id) }, set: { on in
                        Task {
                            busy = bot.id
                            await toggle(bot.id, on)
                            busy = nil
                        }
                    }))
                    .labelsHidden()
                    .fixedSize()
                    .disabled(busy == bot.id)
                }
                .padding(.horizontal, 14)
                .frame(minHeight: 48)
            }
        }
    }
}

/// Which Bots get a new connection or drive: every Bot the user manages starts ticked.
struct BotsStep: View {
    @Environment(SpaceModel.self) private var spaces
    @Binding var picked: Set<String>

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Which Bots can use it").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink)
            if spaces.managedBots.isEmpty {
                Text("No Bot can use it yet. Give it to one later from Choose Bots.")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                FlowRow(spacing: 8) {
                    ForEach(spaces.managedBots, id: \.id) { bot in chip(bot) }
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        .sensoryFeedback(.selection, trigger: picked)
    }

    private func chip(_ bot: Silo_V1_Bot) -> some View {
        let on = picked.contains(bot.id)
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        return Button {
            withAnimation(Motion.quiet(0.16)) {
                if on { picked.remove(bot.id) } else { picked.insert(bot.id) }
            }
        } label: {
            HStack(spacing: 8) {
                CrestView(index: bot.crest, size: 20)
                Text(bot.name)
                    .font(.silo(13, .medium))
                    .foregroundStyle(on ? Theme.ink : Theme.ink2)
                    .lineLimit(1)
                if on {
                    Image(systemName: "checkmark")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Theme.cobalt)
                }
            }
            .padding(.horizontal, 10)
            .frame(height: 38)
            .background(Theme.surface, in: shape)
            .overlay(shape.strokeBorder(on ? Theme.cobalt : Theme.line, lineWidth: 1))
        }
        .buttonStyle(.press)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// Whose identity a connection or drive carries, and, to its lender, the switch between Personal
/// (only Bots they alone use) and Shared (any Bot of the Workspace, acting as them). Sharing is a
/// second press, because every Bot it reaches then acts as them.
struct LendSection: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    let lentBy: String
    let shared: Bool
    let set: (Bool) async -> Void

    private var solo: Bool { (spaces.current?.people ?? 1) <= 1 }
    private var mine: Bool { lentBy == model.user?.email }

    private var note: String {
        if shared { return "Any Bot in the workspace may get it, and acts as \(lentBy)." }
        let now = "Only Bots that \(lentBy) alone uses may get it."
        return mine ? now + " Shared, any Bot in \(spaces.current?.name ?? "the workspace") may get it, and acts as you." : now
    }

    var body: some View {
        if !lentBy.isEmpty, !solo {
            PanelSection(footer: note) {
                PanelRow(title: "Signed in by \(lentBy)", trailing: { Chip(text: shared ? "Shared" : "Personal") })
                if mine {
                    if shared {
                        PanelRow(symbol: "person", title: "Make personal") { Task { await set(false) } }
                    } else {
                        ArmedButton(title: "Share with workspace", armedTitle: "Tap again: Bots act as you", wide: true) {
                            Task { await set(true) }
                        }
                        .padding(6)
                    }
                }
            }
        }
    }
}

/// "Before you add": a provider's own words on a `well` with a 3pt cobalt rule. Geist, not
/// `ProseView`: the serif is the Bot's voice, and these are the catalog's.
struct GuideWell: View {
    let text: String

    private var words: AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            SectionLabel(text: "Before you add")
            Text(words)
                .font(.silo(14))
                .lineSpacing(3)
                .foregroundStyle(Theme.ink)
                .tint(Theme.cobalt)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well)
        .overlay(alignment: .leading) { Theme.cobalt.frame(width: 3) }
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}
