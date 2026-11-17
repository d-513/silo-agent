import XCTest
@testable import SiloClient

final class ChatTabsTests: XCTestCase {
    private struct Row: TabChat {
        let id: String
        var main = false
        var mine = true
        var hidden = false
        let createdAt: String
        init(_ id: String, day: Int, main: Bool = false, mine: Bool = true, hidden: Bool = false) {
            self.id = id
            self.main = main
            self.mine = mine
            self.hidden = hidden
            createdAt = String(format: "2026-11-%02dT10:00:00Z", day)
        }
    }

    // The list arrives by last activity; tabs keep the order the chats were made.
    private let chats = [
        Row("c", day: 3), Row("main", day: 1, main: true), Row("a", day: 1), Row("closed", day: 2, hidden: true),
        Row("theirs", day: 2, mine: false), Row("b", day: 2),
    ]

    private func ids(_ rows: [Row]) -> [String] { rows.map(\.id) }

    func testTabsAreYoursOldestFirst() {
        XCTAssertEqual(ids(chatTabs(chats)), ["a", "b", "c"])
        XCTAssertEqual(ids(chatTabs(chats, open: "b")), ["a", "b", "c"], "the open tab keeps its place")
        XCTAssertEqual(ids(chatTabs(chats, open: "main")), ["a", "b", "c"], "the main chat has its own tab")
        XCTAssertEqual(ids(chatTabs(chats, open: "gone")), ["a", "b", "c"], "an unknown chat adds nothing")
    }

    func testTheOpenChatIsATabWhileOpen() {
        XCTAssertEqual(ids(chatTabs(chats, open: "closed")), ["a", "b", "c", "closed"])
        XCTAssertEqual(ids(chatTabs(chats, open: "theirs")), ["a", "b", "c", "theirs"])
        let more = chats + [Row("their-main", day: 1, main: true, mine: false)]
        XCTAssertEqual(ids(chatTabs(more, open: "their-main")), ["a", "b", "c", "their-main"])
    }

    func testClosedAndAfterClose() {
        XCTAssertEqual(ids(closedChats(chats)), ["closed"])
        let tabs = ["a", "b", "c"]
        XCTAssertEqual([afterClose(tabs, closing: "a"), afterClose(tabs, closing: "b"), afterClose(tabs, closing: "c"),
                        afterClose(["a"], closing: "a"), afterClose(tabs, closing: "x")], ["b", "c", "b", "", ""])
    }
}
