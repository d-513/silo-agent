#if DEBUG
import Foundation
import SiloClient

/// Simulator-only automation: sign in from the environment and open a screen, so a build can be
/// screenshotted without tapping. Set through `SIMCTL_CHILD_SILO_DEV_*` when launching with simctl.
///
///   SILO_DEV_EMAIL / SILO_DEV_PASSWORD / SILO_DEV_SERVER   sign in
///   SILO_DEV_BOT=<index>                                   select that Bot
///   SILO_DEV_PATH=feed,files,chat:0                        push pages / open the nth chat
///                 new, sidebar, machine, workspace, space:<tab>, settings-list
enum DebugLaunch {
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
            } else if let tab = BotTab(rawValue: String(step)) {
                model.path.append(.page(tab))
            } else if step == "new" {
                model.newChat()
            } else if step == "sidebar" {
                model.sidebarOpen = true
            } else if step == "machine" {
                model.machineOpen = true
            } else if step == "workspace" {
                model.path.append(.workspace)
            } else if step.hasPrefix("space:"), let tab = SpaceTab(rawValue: String(step.dropFirst(6))) {
                model.path.append(.space(tab))
            } else if step == "settings-list" {
                model.path.append(.settings)
            }
            try? await Task.sleep(for: .milliseconds(600))
        }
    }
}
#endif
