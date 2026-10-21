import XCTest
@testable import SiloClient

final class ChatGroupsTests: XCTestCase {
    func testGroupsByAgeInOrder() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let now = ISO8601DateFormatter().date(from: "2026-10-21T12:00:00Z")!
        func ago(_ days: Double) -> Date { now.addingTimeInterval(-days * 86400) }
        let items: [(String, Date?)] = [
            ("a", ago(0.1)), ("b", ago(1)), ("c", ago(3)), ("d", ago(20)), ("e", ago(60)), ("f", nil), ("g", ago(0.2)),
        ]
        let groups = chatGroups(items, updatedAt: { $0.1 }, now: now, calendar: calendar)
        XCTAssertEqual(groups.map(\.title), ["Today", "Yesterday", "Previous 7 days", "Previous 30 days", "August 2026", "Older"])
        XCTAssertEqual(groups[0].items.map(\.0), ["a", "g"])
    }
}
