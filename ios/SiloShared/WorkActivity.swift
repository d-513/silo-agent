import ActivityKit
import Foundation
import SiloClient

/// A Bot at work, on the Dynamic Island and the lock screen, with what is waiting on you. The app
/// updates it while it runs; with no push server it can go quiet, and then it says so (`isStale`).
struct WorkActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var look: StatusLook
        var step: String
        /// The approval a run is paused on: it can be answered from here.
        var approvalID: String
        var approvalTitle: String
        var done: Bool
    }

    var botID: String
    var botName: String
    var crest: Int32
    var chatID: String
}
