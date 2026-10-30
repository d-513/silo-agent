import ActivityKit
import AppIntents
import SiloClient

/// A call on the Dynamic Island and the lock screen.
struct CallActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var tone: CallTone
        /// What is going on, in the words the call screen uses.
        var words: String
        /// The sentence the Bot is on, or what it heard from you.
        var caption: String
        var muted: Bool
        var routeSymbol: String
        var speaker: Bool
        var startedAt: Date
        var ended: Bool
    }

    var botID: String
    var botName: String
    var crest: Int32
}

/// The call's buttons on the Island and the lock screen. They run in the app, which holds the call.
enum CallControl: String, AppEnum {
    case mute, speaker, end, stopTalking

    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Call control"
    static let caseDisplayRepresentations: [CallControl: DisplayRepresentation] = [
        .mute: "Mute",
        .speaker: "Speaker",
        .end: "End call",
        .stopTalking: "Stop talking",
    ]
}

struct CallControlIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Control the call"
    static let isDiscoverable = false
    static var allowedExecutionTargets: IntentExecutionTargets { .main }

    @Parameter(title: "Action") var action: CallControl

    init() {}
    init(_ action: CallControl) { self.action = action }

    func perform() async throws -> some IntentResult {
        #if !SILO_EXTENSION
        await CallModel.shared.control(action)
        #endif
        return .result()
    }
}
