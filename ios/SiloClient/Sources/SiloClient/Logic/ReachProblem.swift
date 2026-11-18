import Foundation

/// Why a request to the Control Plane failed, in words a person can act on: offline, the address
/// does not resolve, the server refused or is slow, the certificate was refused, the session ended.
/// The watch shows it as is; never a raw `URLError` or a gRPC code.
public struct ReachProblem: Error, Equatable, Sendable {
    public enum Kind: String, Sendable {
        case offline, noSuchHost, refused, timedOut, insecure, signedOut, serverError, refusedAction, cancelled
    }

    public var kind: Kind
    public var title: String
    public var detail: String
    /// An SF Symbol for it.
    public var symbol: String

    /// Trying again could help: everything but a session that ended or a request the server refused.
    public var retryable: Bool { ![.signedOut, .refusedAction, .cancelled].contains(kind) }
    /// The network, not what was asked: the last good data is still worth showing under it.
    public var isConnection: Bool { [.offline, .noSuchHost, .refused, .timedOut, .insecure].contains(kind) }
}

/// Words `error` for someone holding a watch (or a phone) that could not reach `server`.
public func reachProblem(_ error: Error, server: String) -> ReachProblem {
    let host = serverLabel(server).isEmpty ? "Your Silo" : serverLabel(server)
    if error is CancellationError { return .init(kind: .cancelled, title: "", detail: "", symbol: "") }
    if let silo = error as? SiloError {
        switch silo {
        case .network(let code, _):
            return problem(urlCode: code, host: host)
        case .unauthenticated:
            return .init(kind: .signedOut, title: "Signed out",
                         detail: "Open Silo on your iPhone to sign in again.", symbol: "person.crop.circle.badge.xmark")
        case .unavailable(let message):
            // No URLError behind it: a proxy in front of Silo answered 502/503, or a deadline passed.
            if message.localizedCaseInsensitiveContains("deadline") { return problem(urlCode: URLError.timedOut.rawValue, host: host) }
            return problem(urlCode: URLError.cannotConnectToHost.rawValue, host: host)
        case .invalidArgument(let message), .precondition(let message):
            return .init(kind: .refusedAction, title: "Couldn't do that", detail: firstSentence(message, or: "Silo said no."),
                         symbol: "exclamationmark.circle")
        case .server(let message):
            return .init(kind: .serverError, title: "Silo had a problem",
                         detail: firstSentence(message, or: "Try again in a moment."), symbol: "exclamationmark.triangle")
        }
    }
    let ns = error as NSError
    if ns.domain == NSURLErrorDomain { return problem(urlCode: ns.code, host: host) }
    return .init(kind: .serverError, title: "Something went wrong",
                 detail: firstSentence(error.localizedDescription, or: "Try again in a moment."), symbol: "exclamationmark.triangle")
}

private func problem(urlCode code: Int, host: String) -> ReachProblem {
    switch URLError.Code(rawValue: code) {
    case .cancelled:
        return .init(kind: .cancelled, title: "", detail: "", symbol: "")
    case .notConnectedToInternet, .dataNotAllowed, .internationalRoamingOff, .callIsActive:
        return .init(kind: .offline, title: "No connection",
                     detail: "Your watch is offline. Check Wi-Fi, or keep your iPhone nearby.", symbol: "wifi.slash")
    case .networkConnectionLost:
        return .init(kind: .offline, title: "Connection dropped",
                     detail: "The network went away mid-way. Try again in a moment.", symbol: "wifi.exclamationmark")
    case .cannotFindHost, .dnsLookupFailed:
        return .init(kind: .noSuchHost, title: "Can't find your Silo",
                     detail: "\(host) didn't resolve. If it's behind a VPN, check the tunnel is up.", symbol: "network.slash")
    case .timedOut:
        return .init(kind: .timedOut, title: "Silo is slow to answer",
                     detail: "\(host) didn't answer in time. Try again in a moment.", symbol: "clock.badge.exclamationmark")
    case .secureConnectionFailed, .serverCertificateHasBadDate, .serverCertificateUntrusted, .serverCertificateHasUnknownRoot,
         .serverCertificateNotYetValid, .clientCertificateRejected, .clientCertificateRequired, .appTransportSecurityRequiresSecureConnection:
        return .init(kind: .insecure, title: "Can't connect securely",
                     detail: "\(host)'s certificate wasn't accepted, so nothing was sent.", symbol: "lock.slash")
    default:
        // Refused, reset, a bad answer from a proxy: the address is right, nobody answered.
        return .init(kind: .refused, title: "Silo isn't answering",
                     detail: "\(host) didn't respond. It may be down or restarting.", symbol: "bolt.horizontal.circle")
    }
}

/// A server message cut to its first sentence, short enough for a watch.
private func firstSentence(_ text: String, or fallback: String) -> String {
    let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return fallback }
    let end = trimmed.firstIndex { ".!?\n".contains($0) }.map { trimmed.index(after: $0) } ?? trimmed.endIndex
    let sentence = String(trimmed[..<end]).trimmingCharacters(in: .whitespacesAndNewlines)
    return sentence.count > 140 ? String(sentence.prefix(139)) + "…" : sentence
}
