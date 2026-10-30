import AppIntents
import SiloClient
import SwiftUI
import WidgetKit

// Controls: Control Center, the Lock Screen's corners, and the Action Button, which takes any of
// them. Call rings a Bot with no app in the way.

struct CallControlConfiguration: ControlConfigurationIntent {
    static let title: LocalizedStringResource = "Call"
    @Parameter(title: "Bot") var bot: BotEntity?
}

struct CallControlWidget: ControlWidget {
    var body: some ControlWidgetConfiguration {
        AppIntentControlConfiguration(kind: "silo.control.call", intent: CallControlConfiguration.self) { configuration in
            ControlWidgetButton(action: CallBotIntent(bot: configuration.bot)) {
                Label(configuration.bot.map { "Call \($0.name)" } ?? "Call your Bot", systemImage: "phone.fill")
            }
        }
        .displayName("Call a Bot")
        .description("Starts a voice call. Works from the Action Button.")
    }
}

struct NewChatControlWidget: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "silo.control.chat") {
            ControlWidgetButton(action: OpenBotIntent(bot: nil, place: .newChat)) {
                Label("New chat", systemImage: "plus.bubble")
            }
        }
        .displayName("New chat")
        .description("Opens a new chat with your Bot.")
    }
}

struct NeedsYouControlWidget: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "silo.control.needsyou") {
            ControlWidgetButton(action: OpenBotIntent(bot: nil, place: .needsYou)) {
                Label("Needs you", systemImage: "hand.raised")
            }
        }
        .displayName("Needs you")
        .description("Opens what a Bot is waiting on.")
    }
}
