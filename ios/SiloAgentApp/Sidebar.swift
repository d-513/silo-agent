import Observation
import SiloClient
import SwiftUI

/// The web's sidebar (`web/src/nav/Sidebar.tsx`): Silo, the Workspace the app is in (its name opens
/// the switcher, the gear its page), that Workspace's Bots as a list of conversations (the newest
/// message first, each with its Call), and the account at the foot. On a phone it is the sheet from
/// the left; on an iPad the column beside the page.
struct Sidebar: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var creating = false
    @State private var account = false
    @State private var newSpace = false

    var body: some View {
        VStack(spacing: 0) {
            head
            workspace
            botsHead
            ScrollView {
                VStack(spacing: 2) {
                    if !spaces.loaded && model.bots.isEmpty {
                        SkeletonRows(count: 2, height: 54)
                    }
                    ForEach(byLastMessage(spaces.bots) { $0.lastMessageAt }, id: \.id) { BotRow(bot: $0) }
                    if spaces.loaded && spaces.bots.isEmpty {
                        Text(canBuildSpace(spaces.role) ? "No Bots here yet. Make one with the plus above." : "You are not on any Bot in this workspace yet.")
                            .font(.silo(12.5))
                            .foregroundStyle(Theme.ink3)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 12)
                    }
                }
                .padding(.horizontal, 8)
                .padding(.bottom, 8)
            }
            .scrollIndicators(.hidden)
            ProfileRow { account = true }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
                .overlay(alignment: .top) { Theme.line.frame(height: 1) }
        }
        .background(Theme.well.ignoresSafeArea())
        .sheet(isPresented: $creating) { NewBotView().siloSheet() }
        .sheet(isPresented: $account) { AccountView(modal: true).siloSheet() }
        .sheet(isPresented: $newSpace) { NewSpaceSheet().siloSheet() }
    }

    private var head: some View {
        HStack(spacing: 10) {
            Image("silo-mark").resizable().scaledToFit().frame(width: 28, height: 28)
            Text("Silo").font(.silo(15, .semibold)).foregroundStyle(Theme.ink)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .frame(height: 56)
    }

    /// The Workspace's tile and name, which open the switcher, and the gear to its page.
    private var workspace: some View {
        HStack(spacing: 4) {
            SpaceSwitcher(newSpace: { newSpace = true }) {
                HStack(spacing: 10) {
                    SpaceMarkView(mark: spaces.current?.mark ?? "", size: 28)
                    Text(spaces.current?.name ?? "Workspace")
                        .font(.silo(14.5, .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(Theme.ink3)
                }
                .padding(.horizontal, 8)
                .frame(height: 44)
            }
            if spaces.current != nil {
                let on = model.workspaceOpen
                Button { model.openWorkspace() } label: {
                    Image(systemName: "gearshape")
                        .font(.system(size: 15))
                        .foregroundStyle(on ? Theme.ink : Theme.ink2)
                        .frame(width: 40, height: 40)
                        .background(on ? Theme.surface : .clear, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                }
                .buttonStyle(.press)
                .accessibilityLabel("Workspace settings")
            }
        }
        .padding(.horizontal, 8)
    }

    private var botsHead: some View {
        HStack(spacing: 8) {
            SectionLabel(text: "Bots")
            if canBuildSpace(spaces.role) {
                Button { creating = true } label: {
                    Image(systemName: "plus").font(.system(size: 15, weight: .medium))
                }
                .buttonStyle(.glyph(tint: Theme.ink2, side: 36))
                .accessibilityLabel("New Bot")
            }
        }
        .padding(.leading, 16)
        .padding(.trailing, 8)
        .padding(.top, 14)
        .frame(minHeight: 36)
    }
}

/// One Bot as a conversation: its crest and lamp, its name with when it last said something, and
/// under it what it is doing now, else the last thing said (yours marked), else that there is none.
/// The row opens the Bot; Call sits at its end. The open Bot's row is lifted, with its work that
/// has no tab of its own under it. Web: `BotRow` in `nav/Sidebar.tsx`.
private struct BotRow: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    private var on: Bool { bot.id == model.selectedBotID && !model.workspaceOpen }

    var body: some View {
        let look = botLook(bot)
        let unread = Int(bot.dashboardAsks + bot.feedUnread)
        let when = shortWhen(bot.lastMessageAt)
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Button { model.openBot(bot.id) } label: {
                    HStack(spacing: 12) {
                        CrestView(index: bot.crest, size: 36, state: crestState(bot))
                            .overlay(alignment: .topTrailing) {
                                Lamp(status: look.lamp, ground: on ? Theme.surface : Theme.well).offset(x: 2, y: -2)
                            }
                        VStack(alignment: .leading, spacing: 2) {
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text(bot.name).font(.silo(14.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                                Spacer(minLength: 0)
                                if !when.isEmpty {
                                    Text(when).font(.silo(11.5)).monospacedDigit().foregroundStyle(Theme.ink3)
                                }
                            }
                            HStack(spacing: 8) {
                                line.frame(maxWidth: .infinity, alignment: .leading)
                                if unread > 0 && !on { UnreadBadge(count: unread) }
                            }
                        }
                    }
                    .padding(.leading, 12)
                    .padding(.vertical, 10)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.press)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("\(bot.name), \(look.word)")
                .accessibilityAddTraits(on ? .isSelected : [])
                if model.callEnabled { CallBotButton(bot: bot, lit: on) }
            }
            .padding(.trailing, model.callEnabled ? 8 : 12)
            if on {
                ActivityStrip(bot: bot)
                    .padding(.leading, 52)
                    .padding(.trailing, 8)
                    .padding(.bottom, 8)
                    .overlay(alignment: .top) { Theme.line.frame(height: 1).padding(.leading, 52).padding(.trailing, 8) }
            }
        }
        .background(on ? Theme.surface : .clear, in: shape)
        .shadow(color: Theme.shade.opacity(on ? 0.06 : 0), radius: 1, y: 1)
        .overlay(alignment: .leading) { ActiveBar(on: on).padding(.vertical, 10) }
        .animation(Motion.state, value: on)
    }

    /// What the Bot is up to while that is anything, else the last message, else that there is none.
    @ViewBuilder
    private var line: some View {
        if let now = botNow(bot) {
            Text(now.word).font(.silo(12.5, .medium)).foregroundStyle(Theme.color(now.tone)).lineLimit(1)
        } else if bot.lastMessage.isEmpty {
            Text("No messages yet").font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
        } else {
            let you = Text("You:").foregroundStyle(Theme.ink2)
            (bot.lastMessageMine ? Text("\(you) \(bot.lastMessage)") : Text(bot.lastMessage))
                .font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
        }
    }
}

/// Rings the Bot in your main chat. It carries the call: a breathing dot while it connects, then
/// the clock, which leads back. On the open Bot's row it is ink; elsewhere a `surface` circle.
struct CallBotButton: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot
    var lit = true

    var body: some View {
        let call = CallModel.shared
        if call.live, call.who?.id == bot.id, let s = call.snap {
            Button { call.expanded = true } label: {
                HStack(spacing: 6) {
                    if s.state == .connecting {
                        Circle().fill(Theme.ink3).frame(width: 7, height: 7).breathing()
                    } else {
                        Lamp(status: .online)
                        CallClock(since: s.startedAt)
                    }
                }
                .padding(.horizontal, s.state == .connecting ? 0 : 10)
                .frame(minWidth: 32, minHeight: 32)
                .background(Theme.well, in: Capsule())
                .floatShadow()
            }
            .buttonStyle(.press)
            .accessibilityLabel(s.state == .connecting ? "Calling…" : "Back to the call")
        } else {
            Button { model.call(bot: bot) } label: {
                Image(systemName: "phone.fill")
                    .font(.system(size: 13))
                    .foregroundStyle(lit ? Theme.onInk : Theme.ink)
                    .frame(width: 32, height: 32)
                    .background(lit ? Theme.ink : Theme.surface, in: Circle())
                    .shadow(color: Theme.shade.opacity(lit ? 0 : 0.08), radius: 1, y: 1)
            }
            .buttonStyle(.press)
            .sensoryFeedback(.impact(weight: .medium), trigger: call.live)
            .accessibilityLabel("Call \(bot.name)")
        }
    }
}

/// The menu glyph that pulls the sidebar out on a phone; nothing on an iPad, where the sidebar is a
/// column. Its dot says something waits on another Bot of this Workspace: vermilion when one needs
/// you, else cobalt for what is unread on one. Web: `NavButton`.
struct MenuGlyph: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        if sizeClass != .regular {
            let others = spaces.bots.filter { $0.id != model.selectedBotID || model.workspaceOpen }
            let needs = others.contains { botLook($0).lamp == .needsYou }
            let fresh = !needs && others.contains { $0.dashboardAsks + $0.feedUnread > 0 }
            Button { model.sidebarOpen = true } label: {
                Image(systemName: "line.3.horizontal")
                    .font(.system(size: 17, weight: .regular))
                    .overlay(alignment: .topTrailing) {
                        if needs || fresh {
                            Circle().fill(needs ? Theme.vermilion : Theme.cobalt)
                                .frame(width: 8, height: 8)
                                .overlay(Circle().stroke(Theme.well, lineWidth: 2))
                                .offset(x: 5, y: -4)
                        }
                    }
            }
            .buttonStyle(.glyph(tint: Theme.ink2))
            .accessibilityLabel(needs ? "Bots, one needs you" : "Bots")
        }
    }
}

/// Who is signed in, at the foot: an initial, the email, and the way to the account.
private struct ProfileRow: View {
    @Environment(AppModel.self) private var model
    let action: () -> Void

    private var email: String { model.user?.email ?? "" }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Text(email.first.map { String($0).uppercased() } ?? "?")
                    .font(.silo(13, .semibold))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 28, height: 28)
                    .background(Theme.ink, in: Circle())
                Text(email.isEmpty ? "Account" : email)
                    .font(.silo(14, .medium))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 8)
            .frame(height: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .accessibilityLabel("Account, \(email)")
    }
}

/// The 2pt cobalt bar on the open row; it grows in as the last one shrinks out.
struct ActiveBar: View {
    let on: Bool

    var body: some View {
        Theme.cobalt
            .frame(width: 2)
            .scaleEffect(y: on ? 1 : 0)
            .opacity(on ? 1 : 0)
            .animation(Motion.state, value: on)
    }
}

/// One row of the rail's pages: a glyph and a label; the open one is a flat `surface` with the cobalt bar.
struct SideRow: View {
    let symbol: String
    let title: String
    var on = false
    var trailing: AnyView?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 11) {
                Image(systemName: symbol)
                    .font(.system(size: 15))
                    .foregroundStyle(on ? Theme.ink : Theme.ink2)
                    .frame(width: 20)
                Text(title).font(.silo(14.5, .medium)).foregroundStyle(on ? Theme.ink : Theme.ink2).lineLimit(1)
                Spacer(minLength: 0)
                if let trailing { trailing }
            }
            .padding(.horizontal, 10)
            .frame(height: 40)
            .background(on ? Theme.surface : .clear, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .overlay(alignment: .leading) { ActiveBar(on: on).padding(.vertical, 8) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// The Bot's work with no tab of its own: automations and channels with what they are on, its
/// subagents counted on one line. It shows only while there is some: under the open Bot's row in
/// the sidebar, and in the rail. Web: `bot/ActivityStrip.tsx`.
struct ActivityStrip: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    var body: some View {
        let runs = bot.activity.filter { ["automation", "channel", "hidden"].contains($0.kind) }
        let agents = bot.activity.filter { $0.kind == "subagent" }
        if !runs.isEmpty || !agents.isEmpty {
            VStack(alignment: .leading, spacing: 1) {
                ForEach(Array(runs.prefix(3).enumerated()), id: \.offset) { _, row in
                    let look = runLook(Doing(row))
                    rowView(lamp: look.lamp, place: activitySource(kind: row.kind, title: row.title), word: look.word, step: progress(row),
                            tone: look.lamp == .working ? Theme.ink3 : Theme.color(look.tone)) { open(row.chatID) }
                }
                if let lead = agents.first {
                    rowView(lamp: agents.contains { $0.state == "needs_you" } ? .needsYou : .working,
                            place: agents.count == 1 ? "1 subagent at work" : "\(agents.count) subagents at work",
                            word: "", step: "", tone: Theme.ink3) { open(lead.leadChatID) }
                }
            }
            .padding(.top, 6)
            .transition(.rise)
        }
    }

    private func rowView(lamp: LampStatus, place: String, word: String, step: String, tone: Color, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Lamp(status: lamp)
                Text(place).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                Text(word).font(.silo(13)).foregroundStyle(tone).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                if !step.isEmpty { Text(step).font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
            }
            .padding(.horizontal, 6)
            .frame(height: 30)
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
    }

    /// A chat this person can open; an automation's or a channel's log is not in the chat list.
    private func open(_ chatID: String) {
        if bot.id == model.selectedBotID, model.chats.contains(where: { $0.id == chatID }) { model.showChat(chatID) }
    }
}

/// The faces of a Bot several people use: up to three, then +N.
struct PeopleFaces: View {
    let people: [Silo_V1_BotPerson]

    var body: some View {
        HStack(spacing: -6) {
            ForEach(people.prefix(3), id: \.userID) { Initials(email: $0.email, size: 20) }
            if people.count > 3 {
                Text("+\(people.count - 3)").font(.silo(11)).foregroundStyle(Theme.ink3).padding(.leading, 9)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(people.map { "\($0.email), \($0.role == "manage" ? "can manage" : "can use")" }.joined(separator: "; "))
    }
}

/// What waits somewhere you are not looking: a cobalt count.
struct UnreadBadge: View {
    let count: Int

    var body: some View {
        Text(count > 99 ? "99+" : "\(count)")
            .font(.siloMono(11, .medium))
            .foregroundStyle(Theme.onAccent)
            .padding(.horizontal, 6)
            .frame(minWidth: 18)
            .frame(height: 18)
            .background(Theme.cobalt, in: Capsule())
    }
}
