import Foundation

/// What a person types as their server ("silo.example.com", "192.168.1.20:8080",
/// "https://silo.example.com/") as the base URL the client talks to, or nil when it
/// cannot be one. With no scheme, a name on the internet gets https and a local one
/// (localhost, an IP address, a `.local` or single-label name) gets http, since that is
/// how each is usually served.
public func serverAddress(_ input: String) -> String? {
    var value = input.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !value.isEmpty, !value.contains(where: \.isWhitespace) else { return nil }
    if !value.contains("://") {
        value = (isLocalHost(value) ? "http://" : "https://") + value
    }
    while value.hasSuffix("/") { value.removeLast() }
    guard let url = URL(string: value), let scheme = url.scheme?.lowercased(),
          scheme == "http" || scheme == "https", let host = url.host, !host.isEmpty,
          url.query == nil, url.fragment == nil else { return nil }
    return value
}

/// The host and port without the scheme, for showing the address back to the person.
public func serverLabel(_ address: String) -> String {
    guard let url = URL(string: address), let host = url.host else { return address }
    var label = host
    if let port = url.port { label += ":\(port)" }
    if !url.path.isEmpty && url.path != "/" { label += url.path }
    return label
}

private func isLocalHost(_ value: String) -> Bool {
    let hostPort = value.split(separator: "/", maxSplits: 1).first.map(String.init) ?? value
    var host = hostPort
    if host.hasPrefix("[") { return true } // an IPv6 literal
    if let colon = host.lastIndex(of: ":") { host = String(host[..<colon]) }
    host = host.lowercased()
    if host == "localhost" || host.hasSuffix(".local") || host.hasSuffix(".localhost") || !host.contains(".") { return true }
    let parts = host.split(separator: ".")
    return parts.count == 4 && parts.allSatisfy { UInt8($0) != nil }
}
