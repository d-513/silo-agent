import XCTest
@testable import SiloClient

final class ToolSummaryTests: XCTestCase {
    func testPartialJSONField() {
        XCTAssertEqual(extractStringField(#"{"command":"ls -la"#, "command"), "ls -la")
        XCTAssertEqual(extractStringField(#"{"command": "echo \"hi\"\nnext"}"#, "command"), "echo \"hi\"\nnext")
        XCTAssertNil(extractStringField(#"{"other":"x"}"#, "command"))
    }

    func testActions() {
        XCTAssertEqual(toolAction(name: "terminal", args: #"{"command":"ls\npwd"}"#), "$ ls")
        XCTAssertEqual(toolAction(name: "read", args: #"{"path":"a/b.txt","offset":3}"#), "a/b.txt")
        XCTAssertEqual(toolAction(name: "grep", args: #"{"pattern":"TODO"}"#), "TODO")
        XCTAssertEqual(toolAction(name: "spawn_agent", args: #"{"name":"scout"}"#), "start scout")
        XCTAssertEqual(toolAction(name: "agent_status", args: "{}"), "check all")
        XCTAssertEqual(toolAction(name: "exec_python", args: #"{"code":"\nimport os\nprint(1)"#), "import os")
    }

    func testBotScratch() {
        XCTAssertTrue(isBotScratch("bot/shot.png"))
        XCTAssertTrue(isBotScratch("/workspace/bot"))
        XCTAssertFalse(isBotScratch("reports/q3.pdf"))
        XCTAssertFalse(isBotScratch("robot/x"))
    }
}
