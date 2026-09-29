#if DEBUG
import Foundation
import SiloClient

/// Simulator-only automation: sign in from the environment and open a screen, so a build can be
/// screenshotted without tapping. Set through `SIMCTL_CHILD_SILO_DEV_*` when launching with simctl.
///
///   SILO_DEV_EMAIL / SILO_DEV_PASSWORD / SILO_DEV_SERVER   sign in
///   SILO_DEV_BOT=<index>                                   select that Bot
///   SILO_DEV_PATH=feed,files,chat:0                        push pages / the nth chat
enum DebugLaunch {
    @MainActor
    static func run(_ model: AppModel) async {
        let env = ProcessInfo.processInfo.environment
        guard let email = env["SILO_DEV_EMAIL"], let password = env["SILO_DEV_PASSWORD"] else { return }
        if model.user == nil {
            if let server = env["SILO_DEV_SERVER"] { model.serverURL = server }
            model.email = email
            model.password = password
            await model.signIn()
        }
        guard let index = env["SILO_DEV_BOT"].flatMap(Int.init), model.bots.indices.contains(index) else { return }
        model.selectedBotID = model.bots[index].id
        try? await Task.sleep(for: .seconds(1.5))
        for step in (env["SILO_DEV_PATH"] ?? "").split(separator: ",") {
            if step.hasPrefix("chat:"), let n = Int(step.dropFirst(5)), model.chats.indices.contains(n) {
                model.path.append(.chat(model.chats[n].id))
            } else if let tab = BotTab(rawValue: String(step)) {
                model.path.append(.page(tab))
            } else if step == "chats" {
                model.path.append(.chats)
            }
            try? await Task.sleep(for: .milliseconds(600))
        }
    }
}
#endif
