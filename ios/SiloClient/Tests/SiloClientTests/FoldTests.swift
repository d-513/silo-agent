import XCTest
@testable import SiloClient

private func ev(_ kind: String, _ body: String = "", tool: String = "", run: String = "r1") -> Silo_V1_RunEvent {
    var e = Silo_V1_RunEvent()
    e.kind = kind
    e.body = body
    e.tool = tool
    e.runID = run
    return e
}

final class FoldTests: XCTestCase {
    func testChunksThenAssistantSettle() {
        let blocks = foldEvents([ev("user", "hi"), ev("chunk", "Hel"), ev("chunk", "lo"), ev("assistant", "Hello")])
        XCTAssertEqual(blocks.map(\.kind), [.user, .assistant])
        XCTAssertEqual(blocks[1].text, "Hello")
        XCTAssertFalse(blocks[1].streaming)
    }

    func testThinkingClosesOnChunk() {
        let blocks = foldEvents([ev("thinking_chunk", "hm"), ev("chunk", "x")])
        XCTAssertEqual(blocks.map(\.kind), [.thinking, .assistant])
        XCTAssertFalse(blocks[0].streaming)
    }

    func testToolLifecycle() {
        let blocks = foldEvents([ev("tool_args_chunk", "{\"a\"", tool: "read"), ev("tool", "{\"a\":1}", tool: "read"), ev("tool_chunk", "out"), ev("tool_result", "done", tool: "read")])
        XCTAssertEqual(blocks.count, 1)
        XCTAssertEqual(blocks[0].args, "{\"a\":1}")
        XCTAssertEqual(blocks[0].result, "done")
        XCTAssertFalse(blocks[0].running)
    }

    func testNestedCallLandsInRunningTool() {
        let blocks = foldEvents([ev("tool", "{}", tool: "exec_python"), ev("call", "GitHub · list", tool: "github.list"), ev("call_result", "ok", tool: "github.list")])
        XCTAssertEqual(blocks.count, 1)
        XCTAssertEqual(blocks[0].calls.count, 1)
        XCTAssertEqual(blocks[0].calls[0].result, "ok")
        XCTAssertFalse(blocks[0].calls[0].running)
    }

    func testApprovalDenyMarksToolAndLeavesReceipt() {
        let blocks = foldEvents([
            ev("tool", "{}", tool: "terminal"),
            ev("approval", "ap1", tool: "terminal.run"),
            ev("decision", #"{"decision":"deny","approval_id":"ap1","title":"Run command","target":"ls"}"#),
        ])
        XCTAssertEqual(blocks.map(\.kind), [.tool, .receipt])
        XCTAssertEqual(blocks[0].outcome, .denied)
        XCTAssertFalse(blocks[0].waiting)
        XCTAssertEqual(blocks[1].decision, "deny")
        XCTAssertEqual(blocks[1].text, "ls")
    }

    func testStoppedRunCutsToolsAndLeavesReceipt() {
        let blocks = foldEvents([ev("user", "go"), ev("tool", "{}", tool: "terminal"), ev("done", "stopped")])
        XCTAssertEqual(blocks[1].outcome, .stopped)
        XCTAssertFalse(blocks[1].running)
        XCTAssertEqual(blocks.last?.kind, .receipt)
        XCTAssertEqual(blocks.last?.decision, "stopped")
    }

    func testCompactionSettles() {
        let blocks = foldEvents([ev("compacting", tool: "auto"), ev("compaction", "summary", tool: "auto")])
        XCTAssertEqual(blocks.count, 1)
        XCTAssertEqual(blocks[0].text, "summary")
        XCTAssertFalse(blocks[0].running)
    }

    func testArtifactUpdatesInPlace() {
        let blocks = foldEvents([
            ev("artifact", #"{"type":"skill","name":"s","path":"p","approval_id":"a1","status":"pending"}"#),
            ev("artifact", #"{"type":"skill","name":"s","path":"p","approval_id":"a1","status":"saved","size":12}"#),
        ])
        XCTAssertEqual(blocks.count, 1)
        XCTAssertEqual(blocks[0].artifact?.status, "saved")
        XCTAssertEqual(blocks[0].artifact?.size, 12)
    }

    func testReportQuoteAndSkippedKinds() {
        let blocks = foldEvents([ev("chat_title", "T"), ev("usage"), ev("subagent_report", "r", tool: "a:done, b:error"), ev("feed_quote", "post", tool: "Automation · x")])
        XCTAssertEqual(blocks.map(\.kind), [.report, .quote])
        XCTAssertEqual(blocks[0].agents, [.init(name: "a", status: "done"), .init(name: "b", status: "error")])
    }

    func testStaleKeyErrorHidden() {
        XCTAssertTrue(foldEvents([ev("error", "Set OpenRouter API key in Admin")]).isEmpty)
        XCTAssertEqual(foldEvents([ev("error", "boom")]).count, 1)
    }

    func testChatBusy() {
        XCTAssertTrue(chatBusy([ev("user")]))
        XCTAssertFalse(chatBusy([ev("user"), ev("done")]))
    }
}

final class FoldCardsTests: XCTestCase {
    private func offer(_ id: String, _ status: String = "open") -> Silo_V1_SetupOffer {
        var o = Silo_V1_SetupOffer()
        o.id = id
        o.status = status
        return o
    }

    func testOffersOfOneRunAreOneGroupAndTakeTheirAnswer() {
        var a = ev("setup_offer", "a"); a.setupOffer = offer("a")
        var b = ev("setup_offer", "b"); b.setupOffer = offer("b")
        var done = ev("setup_state", "a", run: ""); done.setupOffer = offer("a", "done")
        let blocks = foldEvents([
            ev("tool", "", tool: "offer_setup"), ev("tool_result", "{}", tool: "offer_setup"), a,
            ev("tool", "", tool: "offer_setup"), ev("tool_result", "{}", tool: "offer_setup"), b,
            ev("setup_state", "b", run: ""), done,
        ])
        let groups = blocks.filter { $0.kind == .offers }
        XCTAssertEqual(groups.count, 1)
        XCTAssertEqual(groups[0].offers.map(\.id), ["a", "b"])
        XCTAssertEqual(groups[0].offers[0].status, "done")
    }

    func testCardFollowsItsState() {
        var card = Silo_V1_DashboardCard()
        card.id = "c1"
        card.status = "open"
        var shown = ev("card", "c1"); shown.card = card
        card.status = "submitted"
        var state = ev("card_state", "c1", run: ""); state.card = card
        var asked = ev("feed_quote", "q"); asked.card = card
        let blocks = foldEvents([shown, state, asked])
        XCTAssertEqual(blocks.map(\.kind), [.card, .card])
        XCTAssertEqual(blocks[0].card?.status, "submitted")
        XCTAssertTrue(blocks[1].asked)
    }

    func testBoundariesAndAuthor() {
        var user = ev("user", "hi"); user.author = "bob@example.com"
        let blocks = foldEvents([user, ev("context_cleared"), ev("wiped", "4")])
        XCTAssertEqual(blocks[0].author, "bob@example.com")
        XCTAssertEqual(blocks[1].source, "cleared")
        XCTAssertEqual(blocks[2].source, "wiped")
        XCTAssertEqual(blocks[2].count, 4)
    }

    func testPrependedPageKeepsKeys() {
        let late = foldEvents([ev("user", "b")], first: 0)
        let all = foldEvents([ev("user", "a"), ev("user", "b")], first: -1)
        XCTAssertEqual(all.last?.id, late.first?.id)
    }

    func testCompactionAndReportKeepAChatBusy() {
        XCTAssertTrue(chatBusy([ev("compacting")]))
        XCTAssertTrue(chatBusy([ev("subagent_report")]))
        XCTAssertFalse(chatBusy([ev("compacting"), ev("done")]))
    }
}

final class SlashTests: XCTestCase {
    private func command(_ name: String) -> Silo_V1_Command {
        var c = Silo_V1_Command()
        c.name = name
        return c
    }

    func testQuery() {
        XCTAssertEqual(slashQuery("/cl"), "cl")
        XCTAssertEqual(slashQuery("/"), "")
        XCTAssertNil(slashQuery("/clear now"))
        XCTAssertNil(slashQuery("hi"))
        XCTAssertEqual(matchCommands([command("clear"), command("compact"), command("wipe")], "c").map(\.name), ["clear", "compact"])
    }

    func testLooksLikeCommand() {
        XCTAssertTrue(looksLikeCommand("/clear", attachments: 0))
        XCTAssertFalse(looksLikeCommand("//clear", attachments: 0))
        XCTAssertFalse(looksLikeCommand("/clear", attachments: 1))
    }

    func testSaid() {
        var r = Silo_V1_SendResponse()
        r.command = "wipe"
        r.value = "3"
        XCTAssertEqual(commandSaid(r), "Wiped 3 messages.")
        r.command = "remember"
        r.value = "0/0/0"
        XCTAssertEqual(commandSaid(r), "Nothing new to remember.")
        r.command = "unknown"
        r.notice = "Done."
        XCTAssertEqual(commandSaid(r), "Done.")
    }
}
