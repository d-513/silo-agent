import XCTest
@testable import SiloClient

final class ChangesTests: XCTestCase {
    func testParsePatch() {
        let lines = parsePatch("@@ -3,2 +3,3 @@ func main\n ctx\n-old\n+new\n+more\n\\ No newline at end of file\n")
        XCTAssertEqual(lines.map(\.kind), [.hunk, .ctx, .del, .add, .add, .note])
        XCTAssertEqual(lines[0].text, "func main")
        XCTAssertEqual(lines[1].number, 3)
        XCTAssertEqual(lines[2].number, 4, "a deletion carries its old line number")
        XCTAssertEqual(lines[3].number, 4)
        XCTAssertEqual(lines[4].number, 5)
        XCTAssertEqual(lines[5].text, "No newline at end of file")
    }

    func testSources() {
        XCTAssertEqual(changeSourceTitle([]), "Outside a run")
        XCTAssertEqual(changeSourceTitle([], restore: true), "Restored by you")
        let chat = Silo_V1_ChangeSource.with { $0.kind = "chat"; $0.name = "Report" }
        let cron = Silo_V1_ChangeSource.with { $0.kind = "automation"; $0.name = "Nightly" }
        XCTAssertEqual(changeSourceTitle([chat, cron]), "Chat “Report”, Automation “Nightly”")
        XCTAssertEqual(changeSourceHint([chat]), "")
        XCTAssertFalse(changeSourceHint([chat, cron]).isEmpty)
    }

    func testRestoreNotice() {
        var res = Silo_V1_RestoreChangeResponse()
        res.restored = 1
        XCTAssertTrue(changeRestoreNotice(res, path: "a/b.txt").hasPrefix("b.txt is back"))
        res.restored = 0
        res.skippedTotal = 4
        res.skipped = [.with { $0.path = "big.bin"; $0.reason = "large" }]
        XCTAssertEqual(changeRestoreNotice(res, path: ""), "Left as they are: big.bin (over the size limit, so what was in it was never kept) and 3 more.")
    }

    func testSizesAndRestorable() {
        XCTAssertEqual(changeSizes(old: -1, new: -1), "")
        XCTAssertTrue(changeSizes(old: 1024, new: -1).hasPrefix("was "))
        XCTAssertFalse(changeCanRestore(status: "repo", large: false))
        XCTAssertFalse(changeCanRestore(status: "modified", large: true))
        XCTAssertEqual(changeRestoreLabel("added"), "Remove This File")
    }
}
