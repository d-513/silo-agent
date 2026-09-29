import XCTest
@testable import SiloClient

final class ScheduleTests: XCTestCase {
    func testRoundTrips() {
        for cron in ["", "*/30 * * * *", "15 * * * *", "0 */6 * * *", "0 9 * * *", "30 8 * * 1-5", "0 10 * * 0,6", "0 7 * * 1,3", "0 9 1 * *"] {
            XCTAssertEqual(Schedule(cron: cron).toCron, cron, "round-trip \(cron)")
        }
    }

    func testModes() {
        XCTAssertEqual(Schedule(cron: "0 9 * * 1-5").mode, .weekly)
        XCTAssertEqual(Schedule(cron: "0 9 * * 0-6").mode, .daily)
        XCTAssertEqual(Schedule(cron: "*/7 * * * *").mode, .custom)
        XCTAssertEqual(Schedule(cron: "@daily").mode, .custom)
        XCTAssertEqual(Schedule(cron: "0 9 * 1 *").mode, .custom)
    }

    func testDescribe() {
        XCTAssertEqual(describeSchedule(""), "No schedule")
        XCTAssertEqual(describeSchedule("*/30 * * * *"), "Every 30 minutes")
        XCTAssertEqual(describeSchedule("0 * * * *"), "Every hour at :00")
        XCTAssertEqual(describeSchedule("30 8 * * 1-5"), "Weekdays at 08:30")
        XCTAssertEqual(describeSchedule("0 7 * * 1,3"), "Mon, Wed at 07:00")
        XCTAssertEqual(describeSchedule("0 9 2 * *"), "Monthly on the 2nd at 09:00")
        XCTAssertEqual(describeSchedule("0 9 * 1 *"), "Custom: 0 9 * 1 *")
        XCTAssertEqual(describeSchedule("0 9 11 * *"), "Monthly on the 11th at 09:00")
    }
}
