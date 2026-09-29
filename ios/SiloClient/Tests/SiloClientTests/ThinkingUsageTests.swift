import XCTest
@testable import SiloClient

final class ThinkingUsageTests: XCTestCase {
    func testFitKeepsSupportedLevel() {
        XCTAssertEqual(fitThinking("high", ["low", "high"]), "high")
    }

    func testFitPicksNearestAndTiesGoCheaper() {
        XCTAssertEqual(fitThinking("medium", ["low", "high"]), "low")
        XCTAssertEqual(fitThinking("max", ["low", "high"]), "high")
        XCTAssertEqual(fitThinking("off", ["low", "high"]), "low")
    }

    func testFitDefaultsToEmpty() {
        XCTAssertEqual(fitThinking("", ["low"]), "")
        XCTAssertEqual(fitThinking("high", []), "")
        XCTAssertEqual(fitThinking("bogus", ["low"]), "")
    }

    func testLabels() {
        XCTAssertEqual(thinkingLabel(""), "Default")
        XCTAssertEqual(thinkingLabel("xhigh"), "Extra high")
    }

    func testUsageParse() {
        let u = Usage(eventBody: #"{"input":1000,"output":234,"cache_read":50,"window":2000}"#)
        XCTAssertEqual(u?.used, 1234)
        XCTAssertEqual(u?.cacheRead, 50)
        XCTAssertEqual(u?.fraction ?? 0, 0.617, accuracy: 0.001)
        XCTAssertNil(Usage(eventBody: "nope"))
        XCTAssertEqual(Usage(eventBody: "{}")?.fraction, 0)
    }

    func testFormatTokens() {
        XCTAssertEqual(formatTokens(850), "850")
        XCTAssertEqual(formatTokens(1234), "1.2k")
        XCTAssertEqual(formatTokens(48000), "48k")
    }
}
