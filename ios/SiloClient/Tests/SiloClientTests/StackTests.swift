import XCTest
@testable import SiloClient

// The same numbers as web/src/stack_test.ts: brand/reference holds the drawings they come from.
final class StackTests: XCTestCase {
    private func shape(_ name: String) -> Int { STACK_SHAPES.firstIndex { $0.name == name }! }
    private func hue(_ name: String) -> Int { STACK_HUES.firstIndex { $0.name == name }! }

    func testTheMarkAtRest() {
        let sc = stackScene(shape: shape("wobble"), hue: hue("green"))
        XCTAssertEqual(packCrest(shape: shape("wobble"), color: hue("green")), 0)
        XCTAssertEqual(sc.tile, 0x123B33)
        XCTAssertEqual(sc.head.box, StackBox(x: 27, y: 20, w: 50, h: 24))
        XCTAssertEqual(sc.head.r, 12)
        XCTAssertEqual(sc.layers.map(\.box), [StackBox(x: 20, y: 46, w: 58, h: 18), StackBox(x: 24, y: 66, w: 50, h: 18)])
        XCTAssertEqual(sc.layers.map(\.color), [0x6FE3A8, 0x1FBF7A])
        // Two dot eyes and a smile, around the head's centre (45 and 59 on x, 31 on y).
        XCTAssertEqual(sc.face, [
            .dot(x: -7, y: -1, r: 3.2, white: false),
            .dot(x: 7, y: -1, r: 3.2, white: false),
            .line(x: -3, y: 5, bendX: 3, bendY: 2.5, run: 6, width: 2),
        ])
    }

    // brand/reference/bots/silo-working.svg
    func testWorkingRestacksOverADashedLayer() {
        let sc = stackScene(shape: shape("wobble"), hue: hue("green"), state: .working)
        XCTAssertEqual(sc.head.box, StackBox(x: 27, y: 12, w: 50, h: 22))
        XCTAssertEqual(sc.layers.map(\.box), [StackBox(x: 20, y: 36, w: 58, h: 16), StackBox(x: 24, y: 54, w: 50, h: 16)])
        XCTAssertEqual(sc.incoming?.box, StackBox(x: 38, y: 73, w: 50, h: 13))
        XCTAssertEqual(sc.incoming?.color, 0x1FBF7A)
        XCTAssertEqual(sc.eyes, .dash)
    }

    // brand/reference/bots/errands-needs_you.svg
    func testNeedsYouRaisesTheHead() {
        let sc = stackScene(shape: shape("tilt"), hue: hue("sky"), state: .needsYou)
        XCTAssertEqual(sc.head.box, StackBox(x: 24, y: 12, w: 52, h: 24))
        XCTAssertEqual(sc.rot, 0)
        XCTAssertEqual(sc.ticks, StackTicks(y: 43.5, xl: 24, xr: 80))
        XCTAssertEqual(sc.badge, .alert)
        XCTAssertEqual(sc.eyes, .wide)
    }

    func testAsleepFlattens() {
        let g = stackLayout(STACK_SHAPES[shape("tower")], .asleep)
        XCTAssertEqual(g.layers.map(\.w), [56, 62, 68])
        XCTAssertEqual(g.layers.map(\.h), [12, 12, 12])
        XCTAssertEqual(g.layers.last?.y, 73)
        XCTAssertEqual(g.head.h, 17)
        XCTAssertTrue(g.zz)
    }

    func testSmallSizesSimplify() {
        // Under 48 the mark fills the tile and carries no badge; under 20 only the resting face.
        let small = stackScene(shape: 0, hue: 0, state: .needsYou, size: 40)
        XCTAssertEqual(small.origin, 10)
        XCTAssertEqual(small.span, 80)
        XCTAssertNil(small.badge)
        XCTAssertEqual(small.head.box.y, 16)
        let tiny = stackScene(shape: shape("tilt"), hue: 0, state: .working, size: 16)
        XCTAssertEqual(tiny.state, .idle)
        XCTAssertEqual(tiny.rot, 0)
        XCTAssertEqual(tiny.face, [.dot(x: -11, y: 0, r: 5.6, white: false), .dot(x: 11, y: 0, r: 5.6, white: false)])
    }

    func testNothingLeavesTheTileAtSidebarSizes() {
        for s in STACK_SHAPES.indices {
            for state in StackState.allCases {
                let sc = stackScene(shape: s, hue: 0, state: state, size: 40)
                for box in sc.layers.map(\.box) + [sc.incoming?.box].compactMap({ $0 }) {
                    XCTAssertGreaterThanOrEqual(box.x, 10, "\(STACK_SHAPES[s].name) \(state)")
                    XCTAssertLessThanOrEqual(box.x + box.w, 90, "\(STACK_SHAPES[s].name) \(state)")
                    XCTAssertLessThanOrEqual(box.y + box.h, 90, "\(STACK_SHAPES[s].name) \(state)")
                }
            }
        }
    }

    func testCrestPacksAndUnpacks() {
        for h in STACK_HUES.indices {
            for s in STACK_SHAPES.indices {
                let crest = unpackCrest(packCrest(shape: s, color: h))
                XCTAssertEqual(crest.shape, s)
                XCTAssertEqual(crest.color, h)
            }
        }
        XCTAssertEqual(unpackCrest(-1).shape, 7)
        XCTAssertEqual(unpackCrest(-1).color, 7)
    }

    func testFreshCrestTakesAFreeHue() {
        XCTAssertEqual(freshCrest(taken: [], spare: 5), 0)
        XCTAssertEqual(freshCrest(taken: [packCrest(shape: 3, color: 0), packCrest(shape: 1, color: 1)], spare: 5), packCrest(shape: 0, color: 2))
        XCTAssertEqual(freshCrest(taken: STACK_HUES.indices.map { packCrest(shape: 0, color: $0) }, spare: 5), packCrest(shape: 0, color: 5))
    }

    func testCrestStateMostPressingFirst() {
        func bot(_ status: String, machine: String = "online", unread: Int32 = 0) -> Silo_V1_Bot {
            var b = Silo_V1_Bot()
            b.status = status
            b.machine = machine
            b.feedUnread = unread
            return b
        }
        XCTAssertEqual(crestState(bot("needs_you", unread: 3)), .needsYou)
        XCTAssertEqual(crestState(bot("working"), heard: true), .working)
        XCTAssertEqual(crestState(bot("online"), heard: true), .listening)
        XCTAssertEqual(crestState(bot("on_call")), .listening)
        XCTAssertEqual(crestState(bot("stopped", machine: "stopped", unread: 2)), .done)
        XCTAssertEqual(crestState(bot("stopped", machine: "stopped")), .asleep)
        XCTAssertEqual(crestState(bot("waiting")), .asleep)
        XCTAssertEqual(crestState(bot("online")), .idle)
        XCTAssertEqual(crestState(lamp: .working), .working)
        XCTAssertEqual(crestState(lamp: .none), .idle)
    }
}
