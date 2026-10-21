import XCTest
@testable import SiloClient

final class RuleRankTests: XCTestCase {
    func testStrictness() {
        XCTAssertLessThan(ruleStrictness("allow"), ruleStrictness("auto"))
        XCTAssertLessThan(ruleStrictness("ask"), ruleStrictness("deny"))
        XCTAssertEqual(ruleStrictness("bogus"), ruleStrictness("ask"))
        XCTAssertLessThan(ruleStrictness("flag", side: .output), ruleStrictness("check", side: .output))
        XCTAssertEqual(ruleStrictness("bogus", side: .output), ruleStrictness("check", side: .output))
    }

    func testFloor() {
        XCTAssertTrue(ruleModeAllowed("allow", floor: "", side: .decision))
        XCTAssertFalse(ruleModeAllowed("auto", floor: "ask", side: .decision))
        XCTAssertTrue(ruleModeAllowed("deny", floor: "ask", side: .decision))
        XCTAssertFalse(ruleModeAllowed("pass", floor: "flag", side: .output))
    }
}
