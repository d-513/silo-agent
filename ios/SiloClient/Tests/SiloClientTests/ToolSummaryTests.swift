import XCTest
@testable import SiloClient

final class ToolSummaryTests: XCTestCase {
    func testPartialJSONField() {
        XCTAssertEqual(extractStringField(#"{"command":"ls -la"#, "command"), "ls -la")
        XCTAssertEqual(extractStringField(#"{"command": "echo \"hi\"\nnext"}"#, "command"), "echo \"hi\"\nnext")
        XCTAssertNil(extractStringField(#"{"other":"x"}"#, "command"))
    }

    func testDesktopReadsAsItsAction() {
        XCTAssertEqual(toolAction(name: "desktop", args: #"{"action":"click","x":10,"y":20}"#), "click 10,20")
        XCTAssertEqual(toolAction(name: "desktop", args: #"{"action":"key","text":"ctrl+l"}"#), "key ctrl+l")
        XCTAssertEqual(toolAction(name: "desktop", args: #"{"action":"type","text":"hel"#), "type")
        XCTAssertEqual(toolAction(name: "desktop", args: #"{"action":"chrome"}"#), "")
        XCTAssertEqual(toolApp("desktop"), "Desktop")
        XCTAssertEqual(toolSymbol("desktop"), "display")
        XCTAssertEqual(activityStepApp("desktop"), "Desktop")
    }

    func testActions() {
        XCTAssertEqual(toolAction(name: "terminal", args: #"{"command":"ls\npwd"}"#), "$ ls")
        XCTAssertEqual(toolAction(name: "read", args: #"{"path":"a/b.txt","offset":3}"#), "read a/b.txt")
        XCTAssertEqual(toolAction(name: "grep", args: #"{"pattern":"TODO"}"#), "grep TODO")
        XCTAssertEqual(toolAction(name: "spawn_agent", args: #"{"name":"scout"}"#), "start scout")
        XCTAssertEqual(toolAction(name: "agent_status", args: "{}"), "check all")
        XCTAssertEqual(toolAction(name: "exec_python", args: #"{"code":"\nimport os\nprint(1)"#), "import os")
    }

    func testWebBatches() {
        XCTAssertEqual(toolAction(name: "web_search", args: #"{"query":"swift actors"}"#), "swift actors")
        XCTAssertEqual(toolAction(name: "web_search", args: #"{"queries":["a","b","c"]}"#), "a +2 more")
        XCTAssertEqual(toolAction(name: "web_read", args: #"{"urls":["https://x.dev"]}"#), "https://x.dev")
        XCTAssertEqual(toolAction(name: "web_read", args: #"{"url":"https://y.dev","query":"price"}"#), "https://y.dev")
        XCTAssertEqual(toolApp("web_read"), "Web page")
    }

    func testBotScratch() {
        XCTAssertTrue(isBotScratch("bot/shot.png"))
        XCTAssertTrue(isBotScratch("/workspace/bot"))
        XCTAssertFalse(isBotScratch("reports/q3.pdf"))
        XCTAssertFalse(isBotScratch("robot/x"))
    }
}
