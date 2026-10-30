import Foundation

/// Where the session cookie lives. The app and its extensions share one store in the App Group, so
/// a widget or the share sheet is signed in as the app is; set `cookies` first thing in each process.
public enum SiloSession {
    nonisolated(unsafe) public static var cookies: HTTPCookieStorage = .shared

    public static func configuration() -> URLSessionConfiguration {
        let config = URLSessionConfiguration.default
        config.httpCookieStorage = cookies
        config.httpCookieAcceptPolicy = .always
        config.httpShouldSetCookies = true
        return config
    }

    /// Moves a session from before the shared store, once.
    public static func adopt(from old: HTTPCookieStorage = .shared) {
        guard cookies !== old, cookies.cookies?.isEmpty != false else { return }
        for cookie in old.cookies ?? [] { cookies.setCookie(cookie) }
    }
}
