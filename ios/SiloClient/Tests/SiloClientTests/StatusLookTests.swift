import XCTest
@testable import SiloClient

final class StatusLookTests: XCTestCase {
    private func row(kind: String = "chat", chat: String = "", lead: String = "", state: String = "working",
                     task: String = "", done: Int32 = 0, total: Int32 = 0, step: String = "") -> Silo_V1_BotActivity {
        var a = Silo_V1_BotActivity()
        a.kind = kind
        a.chatID = chat
        a.leadChatID = lead
        a.state = state
        a.task = task
        a.taskDone = done
        a.taskTotal = total
        a.step = step
        return a
    }

    func testRunLookFirstMatchWins() {
        XCTAssertEqual(runLook(Doing(state: "needs_you", task: "x")).word, "Needs you")
        XCTAssertEqual(runLook(Doing(state: "waiting", waitingOn: "bob@acme.dev")).word, "Waiting on bob")
        XCTAssertEqual(runLook(Doing(state: "waiting")).word, "Waiting for a manager")
        XCTAssertEqual(runLook(Doing(state: "on_call")).lamp, .online)
        XCTAssertEqual(runLook(Doing(task: "Write the report", step: "exec_python")).word, "Write the report")
        XCTAssertEqual(runLook(Doing(step: "exec_python")).word, "Using Python")
        XCTAssertEqual(runLook(.started), StatusLook(lamp: .working, word: "Thinking", tone: .emerald))
    }

    func testProgress() {
        XCTAssertEqual(progress(row(task: "t", done: 2, total: 5)), "2/5")
        XCTAssertEqual(progress(row(done: 2, total: 5)), "")
        XCTAssertEqual(progress(nil), "")
    }

    func testChatDoingPrefersWhatMatters() {
        var bot = Silo_V1_Bot()
        bot.activity = [
            row(kind: "call", chat: "c", state: "on_call"),
            row(kind: "subagent", chat: "s", lead: "c"),
            row(kind: "chat", chat: "c", task: "own"),
        ]
        XCTAssertEqual(chatDoing(bot, "c")?.task, "own")
        bot.activity.append(row(kind: "subagent", chat: "s2", lead: "c", state: "needs_you"))
        XCTAssertEqual(chatDoing(bot, "c")?.state, "needs_you")
        XCTAssertNil(chatDoing(bot, ""))
    }

    func testBotLook() {
        var bot = Silo_V1_Bot()
        bot.status = "working"
        bot.activity = [row(chat: "a"), row(chat: "b")]
        XCTAssertEqual(botLook(bot).word, "Working on 2 things")
        bot.activity = [row(chat: "a", step: "terminal")]
        XCTAssertEqual(botLook(bot).word, "Using Terminal")
        bot.status = "online"
        bot.activity = []
        XCTAssertEqual(botLook(bot), StatusLook(lamp: .none, word: "Has not worked yet", tone: .ink3))
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        bot.lastActiveAt = "2027-01-15T06:00:00Z"
        XCTAssertEqual(botLook(bot, now: now).lamp, .none)
        XCTAssertTrue(botLook(bot, now: now).word.hasPrefix("Last active"))
    }

    func testMachineWords() {
        XCTAssertEqual(powerWord(machine: "online"), "Stop Bot")
        XCTAssertEqual(powerWord(machine: "starting"), "Starting…")
        XCTAssertEqual(powerWord(machine: "stopped"), "Start Bot")
        XCTAssertEqual(machineLook("online").word, "Running")
        XCTAssertEqual(driveLook("needs_auth").word, "Reconnect needed")
        XCTAssertEqual(channelLook("pending_auth").lamp, .needsYou)
        XCTAssertEqual(connectorLook("needs_auth", builtin: true).word, "Needs setup")
        XCTAssertEqual(finishedLook("error").tone, .vermilion)
    }
}
