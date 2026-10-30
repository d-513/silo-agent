import Foundation
import SiloClient

/// What the app and its extensions share: the App Group's defaults, and the session cookie in its
/// store, so a widget or the share sheet is signed in as the app is.
enum SiloGroup {
    static var id: String { Bundle.main.object(forInfoDictionaryKey: "SiloAppGroup") as? String ?? "" }
    static var defaults: UserDefaults { UserDefaults(suiteName: id) ?? .standard }

    /// First thing in each process.
    static func boot() {
        guard !id.isEmpty else { return }
        SiloSession.cookies = HTTPCookieStorage.sharedCookieStorage(forGroupContainerIdentifier: id)
        SiloSession.adopt()
    }
}
