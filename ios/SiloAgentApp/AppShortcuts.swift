import AppIntents
import SiloClient

/// What Siri, Spotlight and the Action Button offer with no setup.
struct SiloShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: CallBotIntent(),
            phrases: ["Call my Bot in \(.applicationName)", "Call \(\.$bot) in \(.applicationName)", "Talk to \(\.$bot) in \(.applicationName)"],
            shortTitle: "Call a Bot",
            systemImageName: "phone"
        )
        AppShortcut(
            intent: AskBotIntent(),
            phrases: ["Ask \(\.$bot) in \(.applicationName)", "Ask my Bot in \(.applicationName)"],
            shortTitle: "Ask a Bot",
            systemImageName: "bubble.left"
        )
        AppShortcut(
            intent: BotStatusIntent(),
            phrases: ["Check on \(\.$bot) in \(.applicationName)", "What is my Bot doing in \(.applicationName)"],
            shortTitle: "Check on a Bot",
            systemImageName: "waveform.path.ecg"
        )
        AppShortcut(
            intent: OpenBotIntent(),
            phrases: ["Open \(\.$bot) in \(.applicationName)"],
            shortTitle: "Open a Bot",
            systemImageName: "square.grid.2x2"
        )
    }
}

/// Holding the side button, where Apple lets a third-party voice app have it: today that is iPhone
/// in Japan, and it needs Apple's `com.apple.developer.side-button-access.allow` entitlement. Add it
/// to SiloApp.entitlements once granted; nothing else changes. Everywhere else the Action Button
/// and Control Center carry `CallBotIntent`.
@AppIntent(schema: .assistant.activate)
struct ActivateSiloIntent {
    static let supportedModes: IntentModes = .foreground

    func perform() async throws -> some IntentResult {
        let state = SiloStore.load()
        if let bot = state.lastBot {
            await CallModel.shared.start(.init(id: bot.id, name: bot.name, crest: bot.crest), screen: true, host: state.server)
        }
        return .result()
    }
}
