import ActivityKit
import BackgroundTasks
import CoreSpotlight
import SiloClient
import UIKit
import UniformTypeIdentifiers
import UserNotifications
import WidgetKit

// The app's reach outside itself: the shared state widgets read, Spotlight, long-press quick
// actions, notifications, and a background look at what needs you.

extension AppModel {
    /// Publishes the signed-in state for every surface: called when the Bots change.
    func publish() {
        guard let user else { return }
        WorkTracker.shared.update(self)
        var state = SiloStore.load()
        state.server = serverURL
        state.email = user.email
        state.callsOn = callEnabled || state.callsOn
        state.lastBotID = selectedBotID ?? state.lastBotID
        let known = state.approvals
        state.bots = bots.map { bot in
            let waiting = bot.id == selectedBotID ? approvals.count : known.filter { $0.botID == bot.id }.count
            var row = SharedBot(bot, approvals: waiting)
            if bot.id == selectedBotID, let main = mainChat { row.mainChatID = main.id }
            else if let old = state.bots.first(where: { $0.id == bot.id }) { row.mainChatID = old.mainChatID }
            return row
        }
        if let selectedBotID, let bot = selectedBot {
            state.approvals = known.filter { $0.botID != selectedBotID } + approvals.map { SharedApproval($0, bot: bot) }
        }
        state.at = Date()
        guard SiloStore.save(state) else { return }
        WidgetCenter.shared.reloadAllTimelines()
        SiloSurfaces.quickActions(state)
        SiloSurfaces.index(state, chats: chats, botID: selectedBotID)
        SiloSurfaces.notify(state, known: known)
    }

    /// Follows a link from a widget, a quick action, a notification or Spotlight.
    func open(_ link: SiloLink) {
        // A surface that started the app got here before the session did.
        guard user != nil else {
            SiloSceneDelegate.pending = link
            return
        }
        switch link {
        case .needsYou:
            guard let bot = SiloStore.load().approvals.first?.botID else { return }
            go(bot) { $0.path = [.page(.dashboard)] }
        case .mainChat(let bot):
            go(bot) { $0.showMainChat() }
        case .chat(let bot, let chat):
            go(bot) { $0.showChat(chat) }
        case .newChat(let bot):
            go(bot) { $0.newChat() }
        case .dashboard(let bot):
            go(bot) { $0.path = [.page(.dashboard)] }
        case .feed(let bot):
            go(bot) { $0.path = [.page(.feed)] }
        case .call(let bot):
            guard let row = SiloStore.load().bots.first(where: { $0.id == bot }) else { return }
            Task { await CallModel.shared.start(.init(id: row.id, name: row.name, crest: row.crest), screen: true, host: serverURL) }
        }
    }

    /// What a surface asked for while nobody was signed in yet.
    func openPending() {
        guard let link = SiloSceneDelegate.pending else { return }
        SiloSceneDelegate.pending = nil
        open(link)
    }

    // Lands on a Bot, then does the rest once its chats are in.
    private func go(_ bot: String, then: @escaping (AppModel) -> Void) {
        sidebarOpen = false
        machineOpen = false
        selectedBotID = bot
        Task {
            await chooseBot(bot)
            if selectedBotID == bot { then(self) }
        }
    }
}

enum SiloSurfaces {
    /// Long press on the app icon: call the Bot you were last with, a new chat, what needs you, and
    /// the other Bots.
    static func quickActions(_ state: SharedState) {
        var items: [UIApplicationShortcutItem] = []
        if let bot = state.lastBot {
            if !state.approvals.isEmpty {
                items.append(item(.needsYou, title: state.approvals.count == 1 ? "1 needs you" : "\(state.approvals.count) need you",
                                  subtitle: state.approvals.first?.title, symbol: "hand.raised"))
            }
            if state.callsOn { items.append(item(.call(bot: bot.id), title: "Call \(bot.name)", subtitle: nil, symbol: "phone")) }
            items.append(item(.newChat(bot: bot.id), title: "New chat", subtitle: bot.name, symbol: "plus.bubble"))
            items.append(item(.dashboard(bot: bot.id), title: "Dashboard", subtitle: bot.name, symbol: "square.grid.2x2"))
        }
        for bot in state.bots where bot.id != state.lastBot?.id && items.count < 4 {
            items.append(item(.mainChat(bot: bot.id), title: bot.name, subtitle: bot.look.word, symbol: "bubble.left"))
        }
        UIApplication.shared.shortcutItems = items
    }

    private static func item(_ link: SiloLink, title: String, subtitle: String?, symbol: String) -> UIApplicationShortcutItem {
        UIApplicationShortcutItem(type: link.url.absoluteString, localizedTitle: title, localizedSubtitle: subtitle,
                                  icon: UIApplicationShortcutIcon(systemImageName: symbol))
    }

    /// Bots and the open Bot's chats, for system search. The Bot's words stay on the server.
    static func index(_ state: SharedState, chats: [Silo_V1_Chat], botID: String?) {
        var items = state.bots.map { bot -> CSSearchableItem in
            let set = CSSearchableItemAttributeSet(contentType: .text)
            set.title = bot.name
            set.contentDescription = bot.look.word
            return CSSearchableItem(uniqueIdentifier: SiloLink.mainChat(bot: bot.id).url.absoluteString, domainIdentifier: "bots", attributeSet: set)
        }
        if let botID, let bot = state.bots.first(where: { $0.id == botID }) {
            items += chats.filter { $0.mine && !$0.main && !$0.title.isEmpty }.prefix(100).map { chat in
                let set = CSSearchableItemAttributeSet(contentType: .text)
                set.title = chat.title
                set.contentDescription = "Chat with \(bot.name)"
                return CSSearchableItem(uniqueIdentifier: SiloLink.chat(bot: botID, chat: chat.id).url.absoluteString, domainIdentifier: "chats", attributeSet: set)
            }
        }
        CSSearchableIndex.default().indexSearchableItems(items) { _ in }
    }

    // MARK: notifications

    static let approvalCategory = "silo.approval"

    /// Asks once, with the actions an approval carries.
    static func prepareNotifications() {
        let allow = UNNotificationAction(identifier: "allow_once", title: "Allow once", options: [.authenticationRequired])
        let deny = UNNotificationAction(identifier: "deny", title: "Deny", options: [.destructive, .authenticationRequired])
        let category = UNNotificationCategory(identifier: approvalCategory, actions: [allow, deny], intentIdentifiers: [])
        let center = UNUserNotificationCenter.current()
        center.setNotificationCategories([category])
        center.requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    /// A new approval while you are away is the one thing worth a banner.
    static func notify(_ state: SharedState, known: [SharedApproval]) {
        let center = UNUserNotificationCenter.current()
        guard UIApplication.shared.applicationState != .active else {
            center.setBadgeCount(state.approvals.count)
            return
        }
        center.setBadgeCount(state.approvals.count)
        for approval in state.approvals where !known.contains(where: { $0.id == approval.id }) {
            let content = UNMutableNotificationContent()
            content.title = "\(approval.botName) needs you"
            content.body = approval.summary.isEmpty ? approval.title : "\(approval.title). \(approval.summary)"
            content.sound = .default
            content.categoryIdentifier = approvalCategory
            content.userInfo = ["approval": approval.id, "link": SiloLink.mainChat(bot: approval.botID).url.absoluteString]
            content.threadIdentifier = approval.botID
            center.add(UNNotificationRequest(identifier: approval.id, content: content, trigger: nil))
        }
    }

    // MARK: a look in the background

    static var refreshID: String { (Bundle.main.bundleIdentifier ?? "silo") + ".refresh" }

    static func registerRefresh() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: refreshID, using: nil) { task in
            scheduleRefresh()
            let work = Task {
                let known = SiloStore.load()
                let next = await SiloFeed.fresh(known)
                if SiloStore.save(next) {
                    await MainActor.run {
                        WidgetCenter.shared.reloadAllTimelines()
                        notify(next, known: known.approvals)
                    }
                }
                task.setTaskCompleted(success: true)
            }
            task.expirationHandler = { work.cancel() }
        }
    }

    /// Whatever the last launch left up. It was killed, and the Island would go on saying so. Read
    /// here rather than in the task, so a call this launch starts is not one of them.
    static func endLeftovers() {
        let calls = Activity<CallActivityAttributes>.activities
        let work = Activity<WorkActivityAttributes>.activities
        Task {
            for activity in calls { await activity.end(nil, dismissalPolicy: .immediate) }
            for activity in work { await activity.end(nil, dismissalPolicy: .immediate) }
        }
    }

    static func scheduleRefresh() {
        let request = BGAppRefreshTaskRequest(identifier: refreshID)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60)
        try? BGTaskScheduler.shared.submit(request)
    }
}

/// Quick actions, and answering a notification's Allow or Deny.
final class SiloAppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        SiloSurfaces.registerRefresh()
        SiloSurfaces.endLeftovers()
        return true
    }

    func application(_ application: UIApplication, configurationForConnecting session: UISceneSession, options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let config = UISceneConfiguration(name: nil, sessionRole: session.role)
        config.delegateClass = SiloSceneDelegate.self
        if let item = options.shortcutItem { SiloSceneDelegate.pending = SiloLink(url: URL(string: item.type) ?? URL(fileURLWithPath: "/")) }
        return config
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        if let approval = info["approval"] as? String, ["allow_once", "deny"].contains(response.actionIdentifier) {
            try? await SiloClient(host: SiloStore.load().server).decideApproval(id: approval, decision: response.actionIdentifier)
            return
        }
        if let link = (info["link"] as? String).flatMap(URL.init(string:)).flatMap(SiloLink.init(url:)) {
            await MainActor.run { AppModel.shared.open(link) }
        }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}

final class SiloSceneDelegate: NSObject, UIWindowSceneDelegate {
    /// A surface that started the app: taken once the session is back.
    static var pending: SiloLink?

    func windowScene(_ windowScene: UIWindowScene, performActionFor shortcutItem: UIApplicationShortcutItem) async -> Bool {
        guard let url = URL(string: shortcutItem.type), let link = SiloLink(url: url) else { return false }
        await MainActor.run { AppModel.shared.open(link) }
        return true
    }
}
