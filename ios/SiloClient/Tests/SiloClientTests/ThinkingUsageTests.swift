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

final class DictationTests: XCTestCase {
    func testAppend() {
        XCTAssertEqual(appendDictation("", " hello "), "hello")
        XCTAssertEqual(appendDictation("hi", "there"), "hi there")
        XCTAssertEqual(appendDictation("hi ", "there"), "hi there")
        XCTAssertEqual(appendDictation("hi", "  "), "hi")
    }

    func testClock() {
        XCTAssertEqual(formatClock(7.9), "0:07")
        XCTAssertEqual(formatClock(723), "12:03")
        XCTAssertEqual(formatClock(-1), "0:00")
    }
}

final class MatchTests: XCTestCase {
    func testMatchPercent() {
        XCTAssertEqual(matchPercent(0), 100)
        XCTAssertEqual(matchPercent(0.25), 75)
        XCTAssertEqual(matchPercent(1.4), 0)
        XCTAssertEqual(matchPercent(-1), 100)
    }
}
