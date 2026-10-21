import Foundation

// Pure helpers for the Tunnels page. Port of `web/src/tunnels.ts`; keep them in step.

public enum PortCheck: Equatable, Sendable {
    case port(Int32)
    case error(String)
}

/// The add form's port check. 5900 and 9222 are the machine's own and cannot be tunnelled.
public func parseTunnelPort(_ raw: String) -> PortCheck {
    let s = raw.trimmingCharacters(in: .whitespaces)
    if s.isEmpty { return .error("Enter the port the service listens on.") }
    guard s.allSatisfy(\.isASCII), s.allSatisfy(\.isNumber), let port = Int(s) else { return .error("The port must be a number.") }
    if port < 1 { return .error("Ports start at 1.") }
    if port > 65535 { return .error("Ports go up to 65535.") }
    let reserved = [5900: "the Bot's desktop (x11vnc)", 9222: "Chromium's debugging port"]
    if let use = reserved[port] { return .error("Port \(port) is \(use), which cannot be tunnelled.") }
    return .port(Int32(port))
}

/// The ListTunnels state as words, or nil when tunnels work.
public func tunnelNotice(_ state: String) -> String? {
    switch state {
    case "off": return "Tunnels are turned off by the operator (tunnels.enabled)."
    case "no_host": return "Tunnels need a domain: the operator sets content.host, the suffix tunnels are served under."
    default: return nil
    }
}
