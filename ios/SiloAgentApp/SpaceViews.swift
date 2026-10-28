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

/// A person's letters on a `well` circle (People, a Bot's people, the overview's cards).
struct PersonCircle: View {
    let email: String
    var size: CGFloat = 28

    var body: some View {
        Text(personInitials(email))
            .font(.silo(size >= 28 ? 11 : 9.5, .semibold))
            .foregroundStyle(Theme.ink2)
            .frame(width: size, height: size)
            .background(Theme.well, in: Circle())
            .overlay(Circle().stroke(Theme.line))
            .accessibilityHidden(true)
    }
}

/// A small `well` chip: a role, Personal or Shared, Read-only.
struct WellChip: View {
    let text: String
    var tone: Color = Theme.ink2

    var body: some View {
        Text(text)
            .font(.silo(11, .medium))
            .foregroundStyle(tone)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: 5, style: .continuous))
    }
}

/// "Used by Scout, Ada +2": the Bots a connection, drive or skill is on.
struct UsedByLine: View {
    @Environment(AppModel.self) private var model
    let ids: [String]
    var hidden: Int32 = 0

    var body: some View {
        let names = ids.compactMap { id in model.bots.first { $0.id == id }?.name }
        let more = Int(hidden) + (ids.count - names.count)
        Group {
            if names.isEmpty && more == 0 {
                Text("No Bot uses it")
            } else {
                Text("Used by " + (names + (more > 0 ? ["+\(more)"] : [])).joined(separator: ", "))
            }
        }
        .font(.silo(12))
        .foregroundStyle(Theme.ink3)
        .lineLimit(2)
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
        Section {
            if spaces.managedBots.isEmpty {
                Text("You manage no Bot in this workspace.").font(.silo(14)).foregroundStyle(Theme.ink3)
            }
            ForEach(spaces.managedBots, id: \.id) { bot in
                HStack(spacing: 10) {
                    CrestView(index: bot.crest, size: 22)
                    Text(bot.name).font(.silo(15)).lineLimit(1)
                    Spacer()
                    if busy == bot.id { ProgressView().controlSize(.small) }
                    Toggle(bot.name, isOn: Binding(get: { granted.contains(bot.id) }, set: { on in
                        Task {
                            busy = bot.id
                            await toggle(bot.id, on)
                            busy = nil
                        }
                    }))
                    .labelsHidden()
                    .disabled(busy == bot.id)
                }
            }
        } header: {
            Text("Choose Bots")
        } footer: {
            Text("Each switch gives it to that Bot or takes it away at once.")
        }
    }
}

/// Which Bots get a new connection or drive: every Bot the user manages starts ticked.
struct BotsStep: View {
    @Environment(SpaceModel.self) private var spaces
    @Binding var picked: Set<String>

    var body: some View {
        Section {
            if spaces.managedBots.isEmpty {
                Text("No Bot can use it yet. Give it to one later from Choose Bots.")
                    .font(.silo(14)).foregroundStyle(Theme.ink3)
            }
            ForEach(spaces.managedBots, id: \.id) { bot in
                Toggle(isOn: Binding(get: { picked.contains(bot.id) }, set: { on in
                    if on { picked.insert(bot.id) } else { picked.remove(bot.id) }
                })) {
                    HStack(spacing: 10) {
                        CrestView(index: bot.crest, size: 22)
                        Text(bot.name).font(.silo(15)).lineLimit(1)
                    }
                }
            }
        } header: {
            Text("Which Bots can use it")
        }
    }
}

/// Whose identity a connection or drive carries, and, to its lender, the switch between Personal
/// (only Bots they alone use) and Shared (any Bot of the Workspace, acting as them).
struct LendSection: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    let lentBy: String
    let shared: Bool
    let set: (Bool) async -> Void
    @State private var confirming = false

    private var solo: Bool { (spaces.current?.people ?? 1) <= 1 }

    var body: some View {
        if !lentBy.isEmpty, !solo {
            Section {
                HStack {
                    Text("Signed in by \(lentBy)").font(.silo(14)).foregroundStyle(Theme.ink2).lineLimit(1)
                    Spacer()
                    WellChip(text: shared ? "Shared" : "Personal")
                }
                if lentBy == model.user?.email {
                    if shared {
                        Button("Make personal") { Task { await set(false) } }
                    } else {
                        Button("Share with workspace") { confirming = true }
                    }
                }
            } footer: {
                Text(shared ? "Any Bot in the workspace may get it, and acts as \(lentBy)." : "Only Bots that \(lentBy) alone uses may get it.")
            }
            .confirmationDialog("Share with the workspace?", isPresented: $confirming, titleVisibility: .visible) {
                Button("Share: Bots act as me") { Task { await set(true) } }
            } message: {
                Text("Any Bot in \(spaces.current?.name ?? "the workspace") may then get it, and acts as you.")
            }
        }
    }
}
