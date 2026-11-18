import Foundation
@testable import SiloClient
import XCTest

final class WatchTests: XCTestCase {
    private let server = "https://silo.example.com"

    func testNetworkFailuresReadAsPlainWords() {
        let offline = reachProblem(SiloError.network(code: URLError.notConnectedToInternet.rawValue, message: "x"), server: server)
        XCTAssertEqual(offline.kind, .offline)
        XCTAssertTrue(offline.retryable)
        XCTAssertTrue(offline.isConnection)

        let dns = reachProblem(SiloError.network(code: URLError.cannotFindHost.rawValue, message: "x"), server: server)
        XCTAssertEqual(dns.kind, .noSuchHost)
        XCTAssertTrue(dns.detail.contains("silo.example.com"), dns.detail)

        XCTAssertEqual(reachProblem(SiloError.network(code: URLError.timedOut.rawValue, message: "x"), server: server).kind, .timedOut)
        XCTAssertEqual(reachProblem(SiloError.network(code: URLError.serverCertificateUntrusted.rawValue, message: "x"), server: server).kind, .insecure)
        XCTAssertEqual(reachProblem(SiloError.network(code: URLError.cannotConnectToHost.rawValue, message: "x"), server: server).kind, .refused)
        XCTAssertEqual(reachProblem(URLError(.dnsLookupFailed), server: server).kind, .noSuchHost)
        XCTAssertEqual(reachProblem(SiloError.unavailable("Cannot reach the Control Plane. 503"), server: server).kind, .refused)
    }

    func testSessionAndRefusalsAreNotRetried() {
        let out = reachProblem(SiloError.unauthenticated, server: server)
        XCTAssertEqual(out.kind, .signedOut)
        XCTAssertFalse(out.retryable)
        let refused = reachProblem(SiloError.precondition("That approval was already answered. Nothing changed."), server: server)
        XCTAssertEqual(refused.detail, "That approval was already answered.")
        XCTAssertFalse(refused.retryable)
        XCTAssertFalse(refused.isConnection)
    }

    func testNoRawErrorReachesTheWords() {
        for code in [-1, -999, -1001, -1003, -1004, -1005, -1009, -1200, -1202, -9999] {
            let p = reachProblem(SiloError.network(code: code, message: "NSURLErrorDomain -\(code)"), server: server)
            XCTAssertFalse(p.detail.contains("NSURLError"), p.detail)
            XCTAssertFalse(p.title.contains("Error Domain"), p.title)
        }
    }

    func testHandoffRoundTrips() throws {
        let session = try XCTUnwrap(HTTPCookie(properties: [
            .name: "__Host-silo_session", .value: "abc", .domain: "silo.example.com", .path: "/",
            .secure: "TRUE", .expires: Date(timeIntervalSince1970: 2_000_000_000),
        ]))
        let other = try XCTUnwrap(HTTPCookie(properties: [.name: "x", .value: "y", .domain: "elsewhere.org", .path: "/"]))
        let mine = SessionHandoff.cookies([session, other], for: server)
        XCTAssertEqual(mine.map(\.name), ["__Host-silo_session"])

        let sent = SessionHandoff(server: server, email: "a@b.c", cookies: mine, snapshot: Data([1, 2]))
        let context = sent.context
        XCTAssertTrue(PropertyListSerialization.propertyList(context, isValidFor: .binary), "an application context must be a plist")
        let got = try XCTUnwrap(SessionHandoff(context: context))
        XCTAssertEqual(got, sent)
        XCTAssertTrue(got.cookies[0].isSecure)
        XCTAssertTrue(got.signedIn)

        let out = try XCTUnwrap(SessionHandoff(context: SessionHandoff.signedOut.context))
        XCTAssertFalse(out.signedIn)
        XCTAssertNil(SessionHandoff(context: ["server": server]))
    }
}
