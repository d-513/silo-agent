#if DEBUG
import AVFoundation
import Foundation
import SiloClient

/// Simulator-only automation: sign in from the environment and open a screen, so a build can be
/// screenshotted without tapping. Set through `SIMCTL_CHILD_SILO_DEV_*` when launching with simctl.
///
///   SILO_DEV_EMAIL / SILO_DEV_PASSWORD / SILO_DEV_SERVER   sign in
///   SILO_DEV_BOT=<index>                                   select that Bot
///   SILO_DEV_PATH=feed,files,chat:0                        push pages / open the nth chat
///                 new, sidebar, machine, workspace, space:<tab>, settings-list, call, bar,
///                 link:<silo url> (as a surface would, in a running app)
///   SILO_DEV_LINK=<silo url>                               as a surface that started the app would
enum DebugLaunch {
    /// Goes in before the session is back, where a cold start's link does.
    @MainActor
    static func coldLink(_ model: AppModel) {
        guard let url = ProcessInfo.processInfo.environment["SILO_DEV_LINK"].flatMap(URL.init(string:)),
              let link = SiloLink(url: url) else { return }
        model.open(link)
    }

    @MainActor
    static func run(_ model: AppModel) async {
        let env = ProcessInfo.processInfo.environment
        // A server alone opens the sign-in step without signing in.
        if model.user == nil, let server = env["SILO_DEV_SERVER"], env["SILO_DEV_EMAIL"] == nil {
            model.serverURL = server
            return
        }
        guard let email = env["SILO_DEV_EMAIL"], let password = env["SILO_DEV_PASSWORD"] else { return }
        if model.user == nil {
            if let server = env["SILO_DEV_SERVER"] {
                model.serverURL = server
            } else if model.serverURL.isEmpty {
                model.serverURL = "http://localhost:8080"
            }
            model.email = email
            model.password = password
            await model.signIn()
        }
        guard let index = env["SILO_DEV_BOT"].flatMap(Int.init), model.bots.indices.contains(index) else { return }
        model.selectedBotID = model.bots[index].id
        try? await Task.sleep(for: .seconds(1.5))
        for step in (env["SILO_DEV_PATH"] ?? "").split(separator: ",") {
            if step.hasPrefix("chat:"), let n = Int(step.dropFirst(5)), model.chats.indices.contains(n) {
                model.showChat(model.chats[n].id)
            } else if step == "workspace" {
                model.path.append(.workspace)
            } else if let tab = BotTab(rawValue: String(step)) {
                model.path.append(.page(tab))
            } else if step == "new" {
                model.newChat()
            } else if step == "sidebar" {
                model.sidebarOpen = true
            } else if step == "machine" {
                model.machineOpen = true
            } else if step.hasPrefix("space:"), let tab = SpaceTab(rawValue: String(step.dropFirst(6))) {
                model.path.append(.space(tab))
            } else if step == "settings-list" {
                model.path.append(.settings)
            } else if step == "call" {
                model.call(chat: "new")
            } else if step == "gallery" {
                model.showGallery = true
            } else if step == "call-test" {
                await callTest(model)
            } else if step == "bar" {
                CallModel.shared.expanded = false
            } else if step.hasPrefix("link:"), let url = URL(string: String(step.dropFirst(5))), let link = SiloLink(url: url) {
                model.open(link)
            }
            try? await Task.sleep(for: .milliseconds(600))
        }
    }

    /// A real call in a throwaway chat: the Bot says one line, and the chat goes again after.
    @MainActor
    private static func callTest(_ model: AppModel) async {
        NSLog("silo-test start, bot \(model.selectedBot?.name ?? "none"), calls \(model.callEnabled)")
        model.call(chat: "new")
        let call = CallModel.shared
        for _ in 0..<40 where call.snap?.callID.isEmpty != false {
            try? await Task.sleep(for: .milliseconds(250))
        }
        NSLog("silo-test snap \(call.snap?.state.rawValue ?? "none") id \(call.snap?.callID ?? "") chat \(call.snap?.chatID ?? "") error \(call.snap?.error ?? "")")
        guard let chat = call.snap?.chatID, !chat.isEmpty, let botID = model.selectedBotID else { return }
        NSLog("silo-test call up, chat \(chat)")
        await recoveryTest(call)
        _ = try? await model.client.send(botID: botID, chatID: chat, text: "Reply with exactly one short sentence: testing the phone speaker.")
        var heard: Float = 0
        for _ in 0..<160 {
            try? await Task.sleep(for: .milliseconds(250))
            heard = max(heard, call.snap?.level ?? 0)
            NSLog("silo-test state \(call.snap?.state.rawValue ?? "-") level \(call.snap?.level ?? 0) saying \(call.snap?.saying ?? "")")
            if heard > 0, call.snap?.state == .listening { break }
        }
        NSLog("silo-test loudest \(heard)")
        call.hangUp()
        try? await Task.sleep(for: .seconds(3))
        await model.refreshChats()
        await model.deleteChat(chat)
        NSLog("silo-test done")
    }

    /// The system stopping the engine, and an interruption and its end, on a live call.
    @MainActor
    private static func recoveryTest(_ call: CallModel) async {
        let center = NotificationCenter.default
        func interruption(_ type: AVAudioSession.InterruptionType) {
            center.post(name: AVAudioSession.interruptionNotification, object: nil, userInfo: [AVAudioSessionInterruptionTypeKey: type.rawValue])
        }
        func report(_ label: String) { NSLog("silo-test \(label): engine \(call.debugEngineRunning) muted \(call.snap?.muted ?? false)") }
        try? await Task.sleep(for: .seconds(1))
        report("before")

        call.debugStopEngine()
        try? await Task.sleep(for: .milliseconds(200))
        report("stopped behind its back")
        try? await Task.sleep(for: .seconds(2))
        report("after the watchdog")

        interruption(.began)
        call.debugStopEngine()
        try? await Task.sleep(for: .seconds(2.5))
        report("interrupted, left alone")
        interruption(.ended)
        try? await Task.sleep(for: .seconds(0.7))
        report("interruption over")

        call.setMuted(true)
        interruption(.began)
        interruption(.ended)
        try? await Task.sleep(for: .seconds(0.7))
        report("muted before it, stays muted")
        call.setMuted(false)
        try? await Task.sleep(for: .seconds(0.3))
    }
}
#endif
