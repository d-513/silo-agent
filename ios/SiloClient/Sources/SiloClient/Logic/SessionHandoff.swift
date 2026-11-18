import Foundation

/// The phone handing its sign-in to the watch over WatchConnectivity: the server, who is signed
/// in, the session cookies for that server, and what the phone last saw (so the watch has
/// something to show before its first request). Property-list types only, as an application
/// context must be. An empty server means signed out.
public struct SessionHandoff: Equatable {
    public var server: String
    public var email: String
    public var cookies: [HTTPCookie]
    /// The phone's shared state, JSON-encoded (`SharedState`); opaque here.
    public var snapshot: Data?
    public var at: Date

    public init(server: String, email: String, cookies: [HTTPCookie], snapshot: Data? = nil, at: Date = Date()) {
        self.server = server
        self.email = email
        self.cookies = cookies
        self.snapshot = snapshot
        self.at = at
    }

    public static let signedOut = SessionHandoff(server: "", email: "", cookies: [])

    public var signedIn: Bool { !server.isEmpty && !cookies.isEmpty }

    /// What goes over the wire.
    public var context: [String: Any] {
        var out: [String: Any] = [
            "v": 1, "server": server, "email": email, "at": at,
            "cookies": cookies.map(Self.pack),
        ]
        if let snapshot { out["snapshot"] = snapshot }
        return out
    }

    /// Reads a context back; nil for anything that is not one.
    public init?(context: [String: Any]) {
        guard context["v"] as? Int == 1, let server = context["server"] as? String else { return nil }
        self.server = server
        email = context["email"] as? String ?? ""
        at = context["at"] as? Date ?? .distantPast
        snapshot = context["snapshot"] as? Data
        cookies = (context["cookies"] as? [[String: Any]] ?? []).compactMap(Self.unpack)
    }

    /// The cookies a store holds for `server`'s host: the session and nothing from elsewhere.
    public static func cookies(_ all: [HTTPCookie], for server: String) -> [HTTPCookie] {
        guard let host = URL(string: server)?.host?.lowercased() else { return [] }
        return all.filter { cookie in
            let domain = cookie.domain.lowercased()
            let bare = domain.hasPrefix(".") ? String(domain.dropFirst()) : domain
            return host == bare || host.hasSuffix("." + bare)
        }
    }

    private static func pack(_ cookie: HTTPCookie) -> [String: Any] {
        var out: [String: Any] = [
            "name": cookie.name, "value": cookie.value, "domain": cookie.domain, "path": cookie.path,
            "secure": cookie.isSecure, "httpOnly": cookie.isHTTPOnly,
        ]
        if let expires = cookie.expiresDate { out["expires"] = expires }
        if let sameSite = cookie.sameSitePolicy { out["sameSite"] = sameSite.rawValue }
        return out
    }

    private static func unpack(_ dict: [String: Any]) -> HTTPCookie? {
        guard let name = dict["name"] as? String, let value = dict["value"] as? String,
              let domain = dict["domain"] as? String else { return nil }
        var properties: [HTTPCookiePropertyKey: Any] = [
            .name: name, .value: value, .domain: domain, .path: dict["path"] as? String ?? "/",
            // A cookie with no expiry would not outlive the watch app's process; the server
            // decides how long the session really lasts.
            .expires: dict["expires"] as? Date ?? Date().addingTimeInterval(30 * 86400),
        ]
        if dict["secure"] as? Bool == true { properties[.secure] = "TRUE" }
        if dict["httpOnly"] as? Bool == true { properties[HTTPCookiePropertyKey("HttpOnly")] = "TRUE" }
        if let sameSite = dict["sameSite"] as? String { properties[.sameSitePolicy] = sameSite }
        return HTTPCookie(properties: properties)
    }

    public static func == (a: SessionHandoff, b: SessionHandoff) -> Bool {
        a.server == b.server && a.email == b.email && a.snapshot == b.snapshot
            && a.cookies.map { "\($0.name)=\($0.value)@\($0.domain)" } == b.cookies.map { "\($0.name)=\($0.value)@\($0.domain)" }
    }
}
