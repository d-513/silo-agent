import SiloClient
import SwiftUI

/// The Workspace page's body, set inside a `SiloPage` by whoever shows it: a tab for everything that
/// is the Workspace's, each shown only to the roles it is for, then the Bots the user is on as a list
/// of conversations, the newest message first (the web's wide sidebar, `nav/Sidebar.tsx`).
struct WorkspacePage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var creating = false
    @State private var inviting = false

    private var space: Silo_V1_Space? { spaces.current }
    private var role: String { spaces.role }

    var body: some View {
        PageScroll(spacing: 0) {
            head
            tabs.padding(.top, 18)
            if let error = model.botsError {
                ErrorLine(text: error).padding(.top, 14)
            }
            if space == nil && !spaces.loaded {
                SkeletonRows(count: 3, height: 148).padding(.top, 24)
            } else if spaces.bots.isEmpty {
                empty
            } else {
                list
            }
        }
        .scrollIndicators(.hidden)
        .refreshable {
            await model.refreshBots()
            await spaces.load()
        }
        .task { await spaces.load() }
        .sheet(isPresented: $creating) { NewBotView().siloSheet() }
        .sheet(isPresented: $inviting) { if let space { SpaceInviteSheet(space: space) } }
    }

    // MARK: head

    private var head: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 14) {
                SpaceMarkView(mark: space?.mark ?? "", size: 52)
                VStack(alignment: .leading, spacing: 2) {
                    Text(space?.name ?? "Workspace")
                        .font(.silo(26, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    if let space {
                        Text([plural(Int(space.bots), "Bot", "Bots"), plural(Int(space.people), "person", "people"), spaceRoleWord(role)]
                            .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.silo(13))
                            .foregroundStyle(Theme.ink3)
                            .lineLimit(1)
                    }
                }
            }
            if canBuildSpace(role) || canAdminSpace(role) {
                HStack(spacing: 8) {
                    if canBuildSpace(role) {
                        Button { creating = true } label: { headButton("New Bot", "plus") }
                            .buttonStyle(.ink)
                            .controlSize(.small)
                    }
                    if canAdminSpace(role) {
                        Button { inviting = true } label: { headButton("Invite people", "person.badge.plus") }
                            .buttonStyle(.quiet)
                            .controlSize(.small)
                    }
                }
            }
        }
    }

    private func headButton(_ title: String, _ symbol: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).font(.system(size: 12, weight: .medium))
            Text(title)
        }
    }

    /// The Workspace's tabs as chips; each opens its page.
    private var tabs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(SpaceTab.visible(role: role)) { tab in
                    let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    NavigationLink(value: BotRoute.space(tab)) {
                        HStack(spacing: 6) {
                            Image(systemName: tab.symbol).font(.system(size: 13))
                            Text(tab.title).font(.silo(13, .medium))
                        }
                        .foregroundStyle(Theme.ink2)
                        .padding(.horizontal, 11)
                        .frame(height: 34)
                        .background(Theme.surface, in: shape)
                        .overlay(shape.stroke(Theme.line))
                    }
                    .buttonStyle(.press)
                }
            }
        }
        .scrollClipDisabled()
    }

    // MARK: the Bots

    private var list: some View {
        VStack(alignment: .leading, spacing: 10) {
            SectionLabel(text: "Bots")
            Panel {
                ForEach(byLastMessage(spaces.bots) { $0.lastMessageAt }, id: \.id) { BotListRow(bot: $0) }
            }
        }
        .padding(.top, 24)
        .rise()
    }

    private var empty: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("No Bots yet").font(.silo(30, .medium)).foregroundStyle(Theme.ink)
            Text(canBuildSpace(role)
                 ? "A Bot is its own machine. It does not share files with the others."
                 : "Nobody has given you a Bot in this workspace yet.")
                .font(.silo(15))
                .foregroundStyle(Theme.ink2)
            if canBuildSpace(role) {
                Button { creating = true } label: { headButton("New Bot", "plus") }
                    .buttonStyle(.ink)
                    .padding(.top, 6)
            }
        }
        .padding(.top, 48)
        .rise()
    }
}

private func plural(_ n: Int, _ one: String, _ many: String) -> String { "\(n) \(n == 1 ? one : many)" }

/// One Bot as a conversation: its crest and lamp, its name with when it last said something, and
/// under it what it is doing now, else the last thing said (yours marked), else that there is none.
/// The row opens the Bot; Call sits at its end. Web: `BotRow` in `nav/Sidebar.tsx`.
private struct BotListRow: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

    private var on: Bool { bot.id == model.selectedBotID }

    var body: some View {
        let look = botLook(bot)
        let unread = Int(bot.dashboardAsks + bot.feedUnread)
        let when = shortWhen(bot.lastMessageAt)
        HStack(spacing: 8) {
            Button(action: open) {
                HStack(spacing: 12) {
                    CrestView(index: bot.crest, size: 36, state: crestState(bot))
                        .overlay(alignment: .topTrailing) { Lamp(status: look.lamp, ground: Theme.surface).offset(x: 2, y: -2) }
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(bot.name).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
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
                .padding(.leading, 14)
                .padding(.vertical, 11)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(bot.name), \(look.word)")
            if model.callEnabled { CallBotButton(bot: bot) }
        }
        .padding(.trailing, model.callEnabled ? 10 : 14)
    }

    /// What the Bot is up to while that is anything, else the last message, else that there is none.
    @ViewBuilder
    private var line: some View {
        if let now = botNow(bot) {
            Text(now.word).font(.silo(13, .medium)).foregroundStyle(Theme.color(now.tone)).lineLimit(1)
        } else if bot.lastMessage.isEmpty {
            Text("No messages yet").font(.silo(13)).foregroundStyle(Theme.ink3).lineLimit(1)
        } else {
            let you = Text("You:").foregroundStyle(Theme.ink2)
            (bot.lastMessageMine ? Text("\(you) \(bot.lastMessage)") : Text(bot.lastMessage))
                .font(.silo(13)).foregroundStyle(Theme.ink3).lineLimit(1)
        }
    }

    /// Opens the Bot where it opens: the last chat used with it, else its main chat.
    private func open() {
        model.sidebarOpen = false
        if on { model.path = [] } else { model.selectedBotID = bot.id }
    }
}

/// Rings the Bot in your main chat. It carries the call: a breathing dot while it connects, then
/// the clock, which leads back.
private struct CallBotButton: View {
    @Environment(AppModel.self) private var model
    let bot: Silo_V1_Bot

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
                .frame(minWidth: 34, minHeight: 34)
                .background(Theme.well, in: Capsule())
            }
            .buttonStyle(.press)
            .accessibilityLabel(s.state == .connecting ? "Calling…" : "Back to the call")
        } else {
            Button { model.call(bot: bot) } label: {
                Image(systemName: "phone.fill")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.onInk)
                    .frame(width: 34, height: 34)
                    .background(Theme.ink, in: Circle())
            }
            .buttonStyle(.press)
            .sensoryFeedback(.impact(weight: .medium), trigger: call.live)
            .accessibilityLabel("Call \(bot.name)")
        }
    }
}

/// What waits on a Bot you are not looking at: its questions and unread posts, as a cobalt count.
private struct UnreadBadge: View {
    let count: Int

    var body: some View {
        Text(count > 99 ? "99+" : "\(count)")
            .font(.siloMono(11, .medium))
            .foregroundStyle(Theme.onAccent)
            .padding(.horizontal, 6)
            .frame(height: 18)
            .background(Theme.cobalt, in: Capsule())
    }
}

/// One tab of the Workspace page, in the page frame; each tab puts its actions in the head.
struct SpaceTabPage: View {
    let tab: SpaceTab

    var body: some View {
        SiloPage(title: tab.title) { content }
    }

    @ViewBuilder
    private var content: some View {
        switch tab {
        case .connectors: SpaceConnectorsPage()
        case .skills: SpaceSkillsPage()
        case .drives: SpaceDrivesPage()
        case .people: SpacePeoplePage()
        case .rules: SpaceRulesPage()
        case .activity: SpaceActivityPage()
        case .settings: SpaceSettingsPage()
        }
    }
}
