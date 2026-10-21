import XCTest
@testable import SiloClient

final class MailboxTests: XCTestCase {
    func testSenderName() {
        XCTAssertEqual(mailSenderName(from: "\"Ada Lovelace\" <ada@example.com>", address: "ada@example.com"), "Ada Lovelace")
        XCTAssertEqual(mailSenderName(from: "Ada <ada@example.com>", address: "ada@example.com"), "Ada")
        XCTAssertEqual(mailSenderName(from: "<ada@example.com>", address: "ada@example.com"), "ada@example.com")
        XCTAssertEqual(mailSenderName(from: "", address: ""), "Unknown sender")
    }

    func testWakeList() {
        XCTAssertNotNil(mailWakeListError(on: true, raw: " "))
        XCTAssertNil(mailWakeListError(on: false, raw: ""))
        XCTAssertNil(mailWakeListError(on: true, raw: "ada@example.com, @example.org\nexample.net"))
        XCTAssertNotNil(mailWakeListError(on: true, raw: "ada@@example.com"))
        XCTAssertNotNil(mailWakeListError(on: true, raw: "localhost"))
        XCTAssertNotNil(mailWakeListError(on: true, raw: "a b@example.com"))
    }
}
