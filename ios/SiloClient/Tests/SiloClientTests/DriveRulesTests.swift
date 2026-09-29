import XCTest
@testable import SiloClient

final class DriveRulesTests: XCTestCase {
    func testNames() {
        XCTAssertTrue(DriveRules.isValidName("my-drive-2"))
        XCTAssertTrue(DriveRules.isValidName("2fa"))
        XCTAssertFalse(DriveRules.isValidName("-x"))
        XCTAssertFalse(DriveRules.isValidName("Drive"))
        XCTAssertFalse(DriveRules.isValidName(""))
        XCTAssertFalse(DriveRules.isValidName(String(repeating: "a", count: 41)))
    }

    func testUniqueName() {
        XCTAssertEqual(DriveRules.uniqueName("Google Drive", taken: []), "google-drive")
        XCTAssertEqual(DriveRules.uniqueName("Google Drive", taken: ["google-drive"]), "google-drive-2")
        XCTAssertEqual(DriveRules.uniqueName("!!!", taken: []), "drive")
    }

    func testVisibility() {
        XCTAssertTrue(DriveRules.isVisible(visibleIf: [:], values: [:], defaults: [:]))
        XCTAssertTrue(DriveRules.isVisible(visibleIf: ["auth": "key"], values: [:], defaults: ["auth": "key"]))
        XCTAssertFalse(DriveRules.isVisible(visibleIf: ["auth": "key"], values: ["auth": "pass"], defaults: ["auth": "key"]))
    }
}
