import Foundation

// The Stack: a Bot's face, a small head on a pile of rounded layers, on a 100 x 100 grid. A Bot is
// one shape plus one hue; its state moves the pieces. Port of `web/src/stack.ts`; keep the two in
// step. This is the geometry only: `CrestView` draws it. The shapes and hues are `StackArt.swift`,
// generated from `brand/tokens.json` by `make brand`.

public enum StackState: String, Sendable, Codable, CaseIterable {
    case idle, listening, working, needsYou = "needs_you", done, asleep
}

public enum StackEyes: String, Sendable {
    case dot, bright, happy, sleepy, dash, wink, wide
}

public struct StackBox: Equatable, Sendable {
    public var x: Double
    public var y: Double
    public var w: Double
    public var h: Double

    public init(x: Double, y: Double, w: Double, h: Double) {
        self.x = x
        self.y = y
        self.w = w
        self.h = h
    }

    public var midX: Double { x + w / 2 }
    public var midY: Double { y + h / 2 }
}

/// Five stops as 0xRRGGBB. `ground` is the tile and the ink of the eyes.
public struct StackHue: Sendable {
    public let name: String
    public let ground: UInt32
    public let head: UInt32
    public let a: UInt32
    public let b: UInt32
    public let c: UInt32

    public init(name: String, ground: UInt32, head: UInt32, a: UInt32, b: UInt32, c: UInt32) {
        self.name = name
        self.ground = ground
        self.head = head
        self.a = a
        self.b = b
        self.c = c
    }
}

public struct StackShape: Sendable {
    public let name: String
    public let head: StackBox
    /// Top to bottom, under the head.
    public let layers: [StackBox]
    /// The largest corner radius; nil is a pill, half the height.
    public let corner: Double?
    /// The eyes at rest.
    public let eyes: StackEyes
    /// Half the distance between the eyes.
    public let eyeDx: Double
    public let smile: Bool
    /// The head's rotation at rest, in degrees.
    public let tilt: Double

    public init(name: String, head: StackBox, layers: [StackBox], corner: Double?, eyes: StackEyes, eyeDx: Double, smile: Bool, tilt: Double) {
        self.name = name
        self.head = head
        self.layers = layers
        self.corner = corner
        self.eyes = eyes
        self.eyeDx = eyeDx
        self.smile = smile
        self.tilt = tilt
    }
}

/// How much is drawn at a size: under 76 pt the smile goes, under 20 the state.
public enum StackDetail: Sendable {
    case full, medium, tiny
}

public func stackDetail(_ size: Double) -> StackDetail {
    size >= 76 ? .full : size >= 20 ? .medium : .tiny
}

/// Under 48 pt the mark fills the tile and the badge is not drawn in it.
public func stackTight(_ size: Double) -> Bool { size < 48 }

public enum StackBadge: Sendable {
    case alert, check
}

/// The two marks beside the gap under a raised head.
public struct StackTicks: Equatable, Sendable {
    public var y: Double
    public var xl: Double
    public var xr: Double
}

public struct StackLayout: Equatable, Sendable {
    public var head: StackBox
    public var rot: Double
    public var layers: [StackBox]
    /// The dashed layer sliding in while it works.
    public var incoming: StackBox?
    public var ticks: StackTicks?
    public var badge: StackBadge?
    public var zz = false
}

/// Where every piece of a shape is in a state.
public func stackLayout(_ shape: StackShape, _ state: StackState = .idle, centered: Bool = false, compact: Bool = false) -> StackLayout {
    var out = StackLayout(head: shape.head, rot: shape.tilt, layers: shape.layers)

    func centerAll(_ o: inout StackLayout) {
        o.head.x = 50 - o.head.w / 2
        o.rot = 0
        for i in o.layers.indices { o.layers[i].x = 50 - o.layers[i].w / 2 }
    }

    switch state {
    case .idle:
        break
    case .listening:
        // The head lifts and tilts and the body settles. Small sizes move less, so the head
        // stays inside the tile.
        out.head.y -= compact ? 3 : 5
        out.rot = compact ? -8 : -10
        for i in out.layers.indices { out.layers[i].y += 2 }
    case .working where compact:
        // No room for another layer: the bottom one becomes the dashed one.
        out.rot = 0
        let last = out.layers.removeLast()
        out.incoming = StackBox(x: min(last.x + 8, 86 - last.w), y: last.y, w: last.w, h: last.h)
    case .working:
        // Everything gets 2 shorter and restacks higher, leaving room for a dashed layer that
        // slides in from the right under the stack.
        var y = max(10, out.head.y - 8)
        out.rot = 0
        out.head.h -= 2
        out.head.y = y
        y += out.head.h + 2
        for i in out.layers.indices {
            out.layers[i].h = max(10, out.layers[i].h - 2)
            out.layers[i].y = y
            y += out.layers[i].h + 2
        }
        if let last = out.layers.last {
            out.incoming = StackBox(x: min(last.x + 14, 90 - last.w), y: y + 1, w: last.w, h: min(13, last.h - 1))
        }
    case .needsYou:
        // The head pops up and leaves a gap, which two ticks mark.
        out.head.y -= compact ? 4 : 8
        out.rot = 0
        for i in out.layers.indices { out.layers[i].y += 2 }
        if let first = out.layers.first {
            out.ticks = StackTicks(
                y: (out.head.y + out.head.h + first.y) / 2 + 1,
                xl: min(out.head.x, first.x) + 2,
                xr: max(out.head.x + out.head.w, first.x + first.w) + 2
            )
        }
        out.badge = .alert
    case .done:
        centerAll(&out)
        out.badge = .check
    case .asleep:
        // The layers flatten and spread, and the head sinks onto them.
        var y = 85.0
        let count = out.layers.count
        for i in stride(from: count - 1, through: 0, by: -1) {
            out.layers[i].h = min(out.layers[i].h, 12)
            out.layers[i].w = 68 - 6 * Double(count - 1 - i)
            out.layers[i].x = 50 - out.layers[i].w / 2
            y -= out.layers[i].h
            out.layers[i].y = y
            y -= 2
        }
        out.head.h = (out.head.h * 0.75).rounded()
        out.head.w += 4
        out.head.x = 50 - out.head.w / 2
        out.head.y = y - out.head.h
        out.rot = 0
        out.zz = true
    }
    if centered { centerAll(&out) }
    return out
}

/// The layers' colors by how many there are. They never change with the state.
public func stackLayerColors(_ hue: StackHue, count: Int) -> [UInt32] {
    switch count {
    case 1: return [hue.b]
    case 2: return [hue.b, hue.c]
    default: return [hue.a, hue.b, hue.c]
    }
}

/// One mark of a face, around the head's centre: a dot, or a stroke from (x, y) that runs `run`
/// to the right, bent toward (x + bendX, y + bendY).
public enum StackMark: Equatable, Sendable {
    case dot(x: Double, y: Double, r: Double, white: Bool)
    case line(x: Double, y: Double, bendX: Double, bendY: Double, run: Double, width: Double)
}

/// A face around (cx0, cy): the eyes of a style, and the smile under them.
public func stackFace(_ style: StackEyes, cx0: Double, cy: Double, dx: Double, r: Double, sw: Double, smile: Bool) -> [StackMark] {
    let xl = cx0 - dx
    let xr = cx0 + dx
    func dot(_ x: Double, _ rad: Double, white: Bool = false) -> StackMark { .dot(x: x, y: cy, r: rad, white: white) }
    func arcUp(_ x: Double) -> StackMark { .line(x: x - 3.5, y: cy + 1.5, bendX: 3.5, bendY: -4.5, run: 7, width: 2.6 * sw) }
    func arcDown(_ x: Double) -> StackMark { .line(x: x - 3.5, y: cy, bendX: 3.5, bendY: 3.5, run: 7, width: 2.6 * sw) }
    func dash(_ x: Double) -> StackMark { .line(x: x - 4, y: cy, bendX: 4, bendY: 0, run: 8, width: 3 * sw) }
    var out: [StackMark]
    switch style {
    case .dot:
        out = [dot(xl, r), dot(xr, r)]
    case .bright:
        out = [
            dot(xl, r + 0.4), dot(xr, r + 0.4),
            .dot(x: xl + 1.3, y: cy - 1.3, r: 1.2, white: true),
            .dot(x: xr + 1.3, y: cy - 1.3, r: 1.2, white: true),
        ]
    case .happy:
        out = [arcUp(xl), arcUp(xr)]
    case .sleepy:
        out = [arcDown(xl), arcDown(xr)]
    case .dash:
        out = [dash(xl), dash(xr)]
    case .wink:
        out = [dot(xl, r), arcUp(xr)]
    case .wide:
        out = [dot(xl, r + 1.8, white: true), dot(xr, r + 1.8, white: true), dot(xl, (r + 1.8) / 2), dot(xr, (r + 1.8) / 2)]
    }
    if smile { out.append(.line(x: cx0 - 3, y: cy + 6, bendX: 3, bendY: 2.5, run: 6, width: 2 * sw)) }
    return out
}

public struct StackPiece: Equatable, Sendable {
    public var box: StackBox
    public var r: Double
    /// 0xRRGGBB.
    public var color: UInt32
    public var opacity: Double
}

/// Everything a drawing of a Bot holds, as numbers and colors.
public struct StackScene: Sendable {
    public var state: StackState
    public var detail: StackDetail
    public var tight: Bool
    /// The square of the grid that is drawn: from `origin` on both axes, `span` wide.
    public var origin: Double
    public var span: Double
    /// The ground: the whole square, its corner 24% of it.
    public var tile: UInt32
    public var ink: UInt32
    public var layers: [StackPiece]
    /// The dashed outline; its `color` is the stroke.
    public var incoming: StackPiece?
    public var incomingWidth: Double
    public var ticks: StackTicks?
    /// The ticks and the z's.
    public var accent: UInt32
    public var head: StackPiece
    public var rot: Double
    public var eyes: StackEyes
    /// The face, around the head's centre.
    public var face: [StackMark]
    public var zz: Bool
    /// Drawn in the mark only at 48 pt and up.
    public var badge: StackBadge?
}

private func wrap(_ i: Int, _ count: Int) -> Int { ((i % count) + count) % count }

public func stackScene(shape s: Int, hue h: Int, state asked: StackState = .idle, size: Double = 100, mono: UInt32? = nil, monoBg: UInt32 = 0x14161A) -> StackScene {
    let shape = STACK_SHAPES[wrap(s, STACK_SHAPES.count)]
    let pal = STACK_HUES[wrap(h, STACK_HUES.count)]
    let detail = stackDetail(size)
    let tight = stackTight(size)
    // A state cannot be read under 20 pt: the UI beside the crest says it.
    let state = detail == .tiny ? .idle : asked
    let g = stackLayout(shape, state, centered: detail == .tiny, compact: tight)
    let count = shape.layers.count
    let fills = mono.map { m in g.layers.map { _ in m } } ?? stackLayerColors(pal, count: count)
    let opacity = mono == nil ? g.layers.map { _ in 1.0 } : (STACK_MONO_OPACITY[count] ?? g.layers.map { _ in 1.0 })
    func radius(_ height: Double) -> Double { shape.corner.map { min($0, height / 2) } ?? height / 2 }

    let stateEyes: [StackState: StackEyes] = [.listening: .bright, .working: .dash, .needsYou: .wide, .done: .happy, .asleep: .sleepy]
    let eyes = detail == .tiny ? .dot : (stateEyes[state] ?? shape.eyes)
    let smile = detail == .full && state == .idle && shape.smile
    let r = detail == .full ? 3.2 : detail == .medium ? 4.2 : 5.6
    let dx = shape.eyeDx + (detail == .medium ? 0.5 : detail == .tiny ? 3 : 0)
    let sw = detail == .full ? 1 : 1.25

    return StackScene(
        state: state,
        detail: detail,
        tight: tight,
        origin: tight ? 10 : 0,
        span: tight ? 80 : 100,
        tile: mono == nil ? pal.ground : monoBg,
        ink: mono == nil ? pal.ground : monoBg,
        layers: g.layers.enumerated().map { i, l in
            StackPiece(box: l, r: radius(l.h), color: fills[min(i, fills.count - 1)], opacity: opacity[min(i, opacity.count - 1)])
        },
        incoming: g.incoming.map { StackPiece(box: $0, r: radius($0.h), color: mono ?? pal.c, opacity: 1) },
        incomingWidth: tight ? 3 : 2.4,
        ticks: g.ticks,
        accent: mono ?? pal.b,
        head: StackPiece(box: g.head, r: radius(g.head.h), color: mono ?? pal.head, opacity: 1),
        rot: g.rot,
        eyes: eyes,
        face: stackFace(eyes, cx0: 0, cy: smile ? -1 : 0, dx: dx, r: r, sw: sw, smile: smile),
        zz: g.zz,
        badge: tight ? nil : g.badge
    )
}

// A Bot stores its look as one number, hue * 8 + shape, in the order of StackArt.swift. Never
// reorder those, only restyle.

public func packCrest(shape: Int, color: Int) -> Int32 {
    Int32(wrap(color, STACK_HUES.count) * STACK_SHAPES.count + wrap(shape, STACK_SHAPES.count))
}

public func unpackCrest(_ index: Int32) -> (shape: Int, color: Int) {
    let i = wrap(Int(index), STACK_SHAPES.count * STACK_HUES.count)
    return (shape: i % STACK_SHAPES.count, color: i / STACK_SHAPES.count)
}

/// A new Bot's look: the first shape, in a hue no Bot beside it wears yet, else in `spare`.
public func freshCrest(taken: [Int32], spare: Int) -> Int32 {
    let used = Set(taken.map { unpackCrest($0).color })
    let free = STACK_HUES.indices.first { !used.contains($0) }
    return packCrest(shape: 0, color: free ?? spare)
}
