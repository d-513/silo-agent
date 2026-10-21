import XCTest
@testable import SiloClient

final class TunnelsTests: XCTestCase {
    func testParsePort() {
        XCTAssertEqual(parseTunnelPort(" 8080 "), .port(8080))
        XCTAssertEqual(parseTunnelPort(""), .error("Enter the port the service listens on."))
        XCTAssertEqual(parseTunnelPort("80a"), .error("The port must be a number."))
        XCTAssertEqual(parseTunnelPort("0"), .error("Ports start at 1."))
        XCTAssertEqual(parseTunnelPort("70000"), .error("Ports go up to 65535."))
        XCTAssertEqual(parseTunnelPort("5900"), .error("Port 5900 is the Bot's desktop (x11vnc), which cannot be tunnelled."))
    }
}
