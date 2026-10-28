import XCTest
@testable import SiloClient

final class SpacesTests: XCTestCase {
    func testPickSpace() {
        XCTAssertEqual(pickSpace(ids: ["a", "b"], named: "b", last: "a"), "b")
        XCTAssertEqual(pickSpace(ids: ["a", "b"], named: nil, last: "b"), "b")
        XCTAssertEqual(pickSpace(ids: ["a", "b"], named: "", last: "gone"), "a")
        XCTAssertEqual(pickSpace(ids: [], named: nil, last: nil), "")
    }

    func testSpaceMark() {
        XCTAssertEqual(spaceMark(name: "Acme labs"), "AL")
        XCTAssertEqual(spaceMark(name: "Acme"), "AC")
        XCTAssertEqual(spaceMark(name: "Acme labs", given: "x!"), "X")
        XCTAssertEqual(spaceMark(name: "  "), "W")
        XCTAssertEqual(spaceMark(name: "— Ops"), "O")
    }

    func testRoles() {
        XCTAssertTrue(canAdminSpace("owner"))
        XCTAssertTrue(canAdminSpace("admin"))
        XCTAssertFalse(canAdminSpace("member"))
        XCTAssertTrue(canBuildSpace("member"))
        XCTAssertFalse(canBuildSpace("guest"))
        XCTAssertFalse(canBuildSpace(""))
        XCTAssertEqual(spaceRoleWord("owner"), "")
        XCTAssertEqual(spaceRoleWord("guest"), "Guest")
    }

    func testMayChangePerson() {
        XCTAssertTrue(mayChangePerson(caller: "owner", theirs: "admin"))
        XCTAssertFalse(mayChangePerson(caller: "owner", theirs: "owner"))
        XCTAssertTrue(mayChangePerson(caller: "admin", theirs: "guest"))
        XCTAssertFalse(mayChangePerson(caller: "admin", theirs: "admin"))
        XCTAssertFalse(mayChangePerson(caller: "member", theirs: "guest"))
    }

    func testInitials() {
        XCTAssertEqual(personInitials("alice.smith@example.com"), "AS")
        XCTAssertEqual(personInitials("bob@example.com"), "BO")
        XCTAssertEqual(personInitials(""), "?")
    }

    func testTabsByRole() {
        XCTAssertEqual(SpaceTab.visible(role: "guest"), [.people, .settings])
        XCTAssertEqual(SpaceTab.visible(role: "member"), [.connectors, .skills, .drives, .people, .settings])
        XCTAssertEqual(SpaceTab.visible(role: "admin"), SpaceTab.allCases)
    }

    func testMoodAndOrder() {
        XCTAssertEqual(botMood(status: "needs_you", asks: 2, live: 0), .needsYou)
        XCTAssertEqual(botMood(status: "online", asks: 1, live: 0), .asks)
        XCTAssertEqual(botMood(status: "online", asks: 0, live: 1), .working)
        XCTAssertEqual(botMood(status: "", asks: 0, live: 0), .stopped)
        XCTAssertEqual(botMood(status: "idle", asks: 0, live: 0), .online)

        let bots: [(String, BotMood)] = [("a", .online), ("b", .working), ("c", .stopped), ("d", .needsYou), ("e", .online), ("f", .asks)]
        let groups = overviewGroups(bots) { $0.1 }
        XCTAssertEqual(groups.now.map(\.0), ["d", "f", "b"])
        XCTAssertEqual(groups.resting.map(\.0), ["a", "e", "c"])
    }

    func testPulse() {
        let pulse = overviewPulse(moods: [.needsYou, .working, .online, .online], asks: 1)
        XCTAssertEqual(pulse.map(\.words), ["1 need you", "1 question", "1 at work", "2 resting"])
    }

    func testActivityWords() {
        XCTAssertEqual(activitySource(kind: "automation", title: "Digest"), "Automation “Digest”")
        XCTAssertEqual(activitySource(kind: "chat", title: ""), "New chat")
        XCTAssertEqual(activityStepApp("exec_python"), "Python")
        XCTAssertEqual(activityStepApp("github__list_issues"), "github")
        XCTAssertEqual(spaceActivityLine(kind: "granted", subject: "GitHub", detail: "", botName: "Scout"), "GitHub given to Scout")
        XCTAssertEqual(spaceActivityLine(kind: "lent", subject: "Drive", detail: "shared", botName: ""), "Drive shared with the workspace")
    }

    func testHandoff() {
        XCTAssertEqual(defaultHandoff(heirs: ["u2"], leaving: true), Handoff(action: "manager", userID: "u2"))
        XCTAssertEqual(defaultHandoff(heirs: [], leaving: true), Handoff(action: "delete"))
        XCTAssertEqual(defaultHandoff(heirs: [], leaving: false), Handoff(action: "take"))
        XCTAssertEqual(handoffActions(hasHeirs: true, leaving: false), ["manager", "take", "delete"])
        XCTAssertEqual(handoffActions(hasHeirs: false, leaving: true), ["delete"])
    }

    func testLendBlocked() {
        XCTAssertEqual(lendBlocked(lentBy: "", shared: false, me: "a@x", othersPicked: true), "")
        XCTAssertEqual(lendBlocked(lentBy: "b@x", shared: true, me: "a@x", othersPicked: false), "")
        XCTAssertEqual(lendBlocked(lentBy: "b@x", shared: false, me: "a@x", othersPicked: false), "Personal to b@x")
        XCTAssertEqual(lendBlocked(lentBy: "a@x", shared: false, me: "a@x", othersPicked: false), "")
        XCTAssertEqual(lendBlocked(lentBy: "a@x", shared: false, me: "a@x", othersPicked: true), "Personal: others will use this Bot")
    }

    func testMoveSummary() {
        XCTAssertEqual(moveSummary(connectors: [], drives: [], skills: [], people: []), "It takes nothing of its workspace's along.")
        XCTAssertEqual(moveSummary(connectors: ["GitHub"], drives: ["docs"], skills: [], people: ["b@x"]),
                       "It loses what was its workspace's: GitHub, docs. These people are not in the new one and lose it: b@x.")
    }

    func testNextCopyName() {
        XCTAssertEqual(nextCopyName("GitHub", taken: []), "GitHub")
        XCTAssertEqual(nextCopyName("GitHub", taken: ["github", "GitHub 2"]), "GitHub 3")
    }
}
