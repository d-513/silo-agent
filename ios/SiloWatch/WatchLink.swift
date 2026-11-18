import Foundation
import SiloClient
import WatchConnectivity

/// The watch's end of the sign-in handoff (`PhoneWatchLink` is the phone's): takes the
/// application context the phone left, and asks for it directly when it has none or the server
/// turned the session away (the phone may have signed in again since).
final class WatchLink: NSObject {
    static let shared = WatchLink()

    private var session: WCSession? { WCSession.isSupported() ? WCSession.default : nil }

    func activate() {
        guard let session else { return }
        session.delegate = self
        session.activate()
    }

    /// Asks the phone for its sign-in now. Only works while the phone is in reach; otherwise the
    /// context it pushes later lands on its own.
    func ask() {
        guard let session, session.activationState == .activated else { return }
        WatchModel.shared.phoneNearby = session.isReachable
        guard session.isReachable else { return }
        WatchModel.shared.asking = true
        session.sendMessage(["want": "session"]) { reply in
            Task { @MainActor in
                WatchModel.shared.asking = false
                Self.apply(reply)
            }
        } errorHandler: { _ in
            Task { @MainActor in WatchModel.shared.asking = false }
        }
    }

    @MainActor
    private static func apply(_ context: [String: Any]) {
        guard let handoff = SessionHandoff(context: context) else { return }
        WatchModel.shared.adopt(handoff)
    }
}

extension WatchLink: WCSessionDelegate {
    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        guard activationState == .activated else { return }
        let context = session.receivedApplicationContext
        let reachable = session.isReachable
        Task { @MainActor in
            WatchModel.shared.phoneNearby = reachable
            if !context.isEmpty { Self.apply(context) }
            if !WatchModel.shared.signedIn { self.ask() }
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
        Task { @MainActor in Self.apply(applicationContext) }
    }

    nonisolated func sessionReachabilityDidChange(_ session: WCSession) {
        let reachable = session.isReachable
        Task { @MainActor in
            WatchModel.shared.phoneNearby = reachable
            if reachable, !WatchModel.shared.signedIn || WatchModel.shared.problem?.kind == .signedOut { self.ask() }
        }
    }
}
