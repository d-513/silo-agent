import XCTest
@testable import SiloClient

final class WidgetsTests: XCTestCase {
    func testJSONReaders() {
        let d = WidgetJSON.object(#"{"value": 12.5, "unit": "ms", "on": true, "items": [{"a": 1}, 3, "x"]}"#)
        XCTAssertEqual(WidgetJSON.number(d["value"]), 12.5)
        XCTAssertNil(WidgetJSON.number(d["on"]), "a bool is no number")
        XCTAssertEqual(WidgetJSON.text(d["on"]), "yes")
        XCTAssertEqual(WidgetJSON.items(d["items"]).count, 1)
        XCTAssertEqual(WidgetJSON.object("[1]").count, 0)
        XCTAssertEqual(WidgetJSON.number(" 42 "), 42)
        XCTAssertEqual(WidgetJSON.display("1,200 €"), "1,200 €", "text stays as written")
    }

    func testAgeAndInterval() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        XCTAssertEqual(widgetAge("1970-01-12T13:46:30Z", now: now), "now")
        XCTAssertEqual(widgetAge("1970-01-12T13:40:00Z", now: now), "6 m")
        XCTAssertEqual(widgetAge("", now: now), "")
        XCTAssertEqual(widgetInterval(900), "15 m")
        XCTAssertEqual(widgetInterval(7200), "2 h")
        XCTAssertEqual(widgetInterval(86400), "1 d")
        XCTAssertEqual(widgetInterval(0), "")
    }

    func testActions() {
        let acts = widgetActions(#"{"actions": [{"id": "a", "label": "Archive", "row": true, "confirm": true}, {"id": "h", "label": "Ask", "kind": "hand"}, {"id": "", "label": "x"}]}"#)
        XCTAssertEqual(acts.map(\.id), ["a", "h"])
        XCTAssertTrue(acts[0].row && acts[0].confirm && !acts[0].hand)
        XCTAssertTrue(acts[1].hand)
    }

    func testChecklistToggle() {
        var state = "{}"
        state = widgetToggle(state, id: "x")
        XCTAssertEqual(widgetChecked(state), ["x"])
        state = widgetToggle(state, id: "y")
        state = widgetToggle(state, id: "x")
        XCTAssertEqual(widgetChecked(state), ["y"])
    }

    func testReceiptToneURL() {
        XCTAssertNil(widgetReceipt("{}"))
        XCTAssertEqual(widgetReceipt(#"{"text": "Archived 3"}"#)?.by, "you")
        XCTAssertEqual(widgetReceipt(#"{"text": "x", "ok": false}"#)?.ok, false)
        XCTAssertEqual(widgetTone("down"), .down)
        XCTAssertEqual(widgetTone("weird"), .ok)
        XCTAssertNil(widgetURL("javascript:alert(1)"))
        XCTAssertNotNil(widgetURL("https://example.com/a"))
    }

    func testNowSplit() {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = TimeZone(identifier: "UTC")!
        let now = ISO8601DateFormatter().date(from: "2026-10-20T12:00:00Z")!
        XCTAssertEqual(widgetNowSplit(["2026-10-20T09:00:00Z", "2026-10-20T15:00:00Z"], now: now, calendar: cal), 1)
        XCTAssertEqual(widgetNowSplit(["2026-10-22T09:00:00Z"], now: now, calendar: cal), -1)
        XCTAssertEqual(widgetNowSplit(["soon"], now: now, calendar: cal), -1)
    }

    func testNumeric() {
        XCTAssertTrue(widgetNumeric(3))
        XCTAssertTrue(widgetNumeric("12.5 %"))
        XCTAssertTrue(widgetNumeric("1,200 GB"))
        XCTAssertFalse(widgetNumeric("hello"))
        XCTAssertFalse(widgetNumeric(true))
    }

    func testAskBlocks() {
        let json = #"[{"type":"markdown","text":"Hi"},{"type":"switch","name":"go","label":"Go","default":true},{"type":"select","name":"when","label":"When","options":["now",{"value":"later","label":"Later"}]},{"type":"checkboxes","name":"pick","options":["a","b"],"default":["b"]},{"type":"kv","items":[{"label":"Size","value":"3 GB"}]},{"type":"input","label":"no name"}]"#
        let (blocks, defaults) = parseAskBlocks(json)
        XCTAssertEqual(blocks.count, 6)
        XCTAssertEqual(defaults["go"], .bool(true))
        XCTAssertEqual(defaults["when"], .string("now"))
        XCTAssertEqual(defaults["pick"], .list(["b"]))
        XCTAssertEqual(blocks[2].options[1].label, "Later")
        XCTAssertEqual(blocks[4].pairs.first?.value, "3 GB")
        XCTAssertFalse(blocks[5].isControl, "a control with no name has nowhere to put its value")
        XCTAssertEqual(encodeAskValues(["go": .bool(false)]), #"{"go":false}"#)
    }
}
