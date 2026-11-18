import Foundation
import SiloClient
import WatchConnectivity

/// Hands the watch the phone's sign-in, so nobody types a password on a wrist: the server, the
/// session cookies for it and what the widgets last saw, as the WatchConnectivity application
/// context (the newest one wins, and it waits for the watch if it is away). Signing out sends an
/// empty one. The watch also asks for it when it has none, or after the server turned it away.
final class PhoneWatchLink: NSObject {
    static let shared = PhoneWatchLink()

    private var session: WCSession? { WCSession.isSupported() ? WCSession.default : nil }

    func activate() {
        guard let session else { return }
        session.delegate = self
        session.activate()
    }

    /// After a sign-in, a sign-out, or a change to what the widgets show.
    func push() {
        guard let session, session.activationState == .activated, session.isPaired, session.isWatchAppInstalled else { return }
        do {
            try session.updateApplicationContext(Self.current().context)
        } catch {
            // The watch keeps what it had and asks again when it next needs to.
        }
    }

    /// The sign-in as the phone has it now.
    static func current() -> SessionHandoff {
        let state = SiloStore.load()
        guard state.signedIn, !state.server.isEmpty else { return .signedOut }
        let cookies = SessionHandoff.cookies(SiloSession.cookies.cookies ?? [], for: state.server)
        guard !cookies.isEmpty else { return .signedOut }
        return SessionHandoff(server: state.server, email: state.email, cookies: cookies, snapshot: try? JSONEncoder().encode(state))
    }
}

extension PhoneWatchLink: WCSessionDelegate {
    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        guard activationState == .activated else { return }
        Task { @MainActor in self.push() }
    }

    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}

    /// Switching to another watch: start again so the new one gets the context.
    nonisolated func sessionDidDeactivate(_ session: WCSession) { session.activate() }

    /// The watch app was installed, or a watch paired.
    nonisolated func sessionWatchStateDidChange(_ session: WCSession) {
        Task { @MainActor in self.push() }
    }

    /// The watch asking for the sign-in now: answered directly while it is in reach.
    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        Task { @MainActor in replyHandler(Self.current().context) }
    }
}
