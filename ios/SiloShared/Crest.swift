import SiloClient
import SwiftUI

let SHAPE_COUNT = STACK_SHAPES.count
let HUE_COUNT = STACK_HUES.count

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            opacity: 1
        )
    }
}

/// A path given in the Stack's grid, drawn at `k` points a unit. `centred` puts the grid's
/// (0, 0) at the middle of the view, for a face that rides on its head; otherwise the grid's
/// `origin` is the view's corner.
private struct GridPath: Shape {
    var path: Path
    var k: CGFloat
    var origin: CGFloat = 0
    var centred = false

    func path(in rect: CGRect) -> Path {
        let move = centred
            ? CGAffineTransform(translationX: rect.midX, y: rect.midY)
            : CGAffineTransform(translationX: rect.minX - origin * k, y: rect.minY - origin * k)
        return path.applying(CGAffineTransform(scaleX: k, y: k).concatenating(move))
    }
}

private func gridPath(_ build: (inout Path) -> Void) -> Path {
    var p = Path()
    build(&p)
    return p
}

/// A Bot's crest: a Stack, one of eight shapes in one of eight hues, acting out a state. The
/// geometry is `stackScene` (SiloClient), the same numbers the web draws; here every piece is its
/// own view, so a change of state slides each one to its new place. Under 20 pt only the resting
/// face is drawn, and under 48 the badge is the caller's (a `Lamp` on the corner). `cutout` is the
/// one-color mark for the lock screen: no ground, and the eyes are holes.
struct CrestView: View {
    let index: Int32
    var size: CGFloat = 40
    var state: StackState = .idle
    var cutout = false

    @Environment(\.accessibilityReduceMotion) private var still
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let crest = unpackCrest(index)
        let sc = stackScene(shape: crest.shape, hue: crest.color, state: state, size: Double(size), mono: cutout ? 0xFFFFFF : nil)
        let k = size / CGFloat(sc.span)
        let o = CGFloat(sc.origin)
        let ink = Color(hex: sc.ink)
        let accent = Color(hex: sc.accent)
        ZStack {
            if !cutout {
                let tile = RoundedRectangle(cornerRadius: size * 0.24, style: .circular)
                tile.fill(Color(hex: sc.tile))
                // The gray ground would sink into a dark page.
                if STACK_HUES[crest.color].name == "gray", scheme == .dark {
                    tile.strokeBorder(Color.white.opacity(0.3), lineWidth: 1)
                }
            }
            if let inc = sc.incoming {
                let outline = RoundedRectangle(cornerRadius: CGFloat(inc.r) * k, style: .circular)
                    .stroke(Color(hex: inc.color), style: StrokeStyle(lineWidth: CGFloat(sc.incomingWidth) * k, dash: [5 * k, 4 * k]))
                place(outline, inc.box, sc, k).transition(.opacity)
            }
            ForEach(Array(sc.layers.enumerated()), id: \.offset) { _, l in
                place(RoundedRectangle(cornerRadius: CGFloat(l.r) * k, style: .circular).fill(Color(hex: l.color).opacity(l.opacity)), l.box, sc, k)
            }
            if let t = sc.ticks {
                GridPath(path: gridPath { p in
                    p.move(to: CGPoint(x: t.xl, y: t.y))
                    p.addLine(to: CGPoint(x: t.xl - 5, y: t.y - 4))
                    p.move(to: CGPoint(x: t.xr, y: t.y))
                    p.addLine(to: CGPoint(x: t.xr + 5, y: t.y - 4))
                }, k: k, origin: o)
                .stroke(accent, style: StrokeStyle(lineWidth: 2.2 * k, lineCap: .round))
                .transition(.opacity)
            }
            head(sc, k: k, ink: ink)
            if sc.zz {
                // At small sizes the z's move in, so the tighter square does not cut them.
                let dx: CGFloat = sc.tight ? -7 : 0
                let dy: CGFloat = sc.tight ? 3 : 0
                GridPath(path: gridPath { p in
                    p.move(to: CGPoint(x: 70 + dx, y: 14 + dy))
                    p.addLine(to: CGPoint(x: 78 + dx, y: 14 + dy))
                    p.addLine(to: CGPoint(x: 70 + dx, y: 23 + dy))
                    p.addLine(to: CGPoint(x: 78 + dx, y: 23 + dy))
                    p.move(to: CGPoint(x: 83 + dx, y: 27 + dy))
                    p.addLine(to: CGPoint(x: 87.5 + dx, y: 27 + dy))
                    p.addLine(to: CGPoint(x: 83 + dx, y: 32.5 + dy))
                    p.addLine(to: CGPoint(x: 87.5 + dx, y: 32.5 + dy))
                }, k: k, origin: o)
                .stroke(accent, style: StrokeStyle(lineWidth: 2.2 * k, lineCap: .round, lineJoin: .round))
                .transition(.opacity)
            }
            if let badge = sc.badge {
                self.badge(badge, k: k, origin: o, ink: ink)
                    .transition(.scale(scale: 0, anchor: UnitPoint(x: 0.87, y: 0.15)).combined(with: .opacity))
            }
        }
        .frame(width: size, height: size)
        .compositingGroup()
        .animation(motion, value: state)
        .accessibilityHidden(true)
    }

    /// A piece at its box: sized, then set by its centre in the drawn square.
    private func place(_ piece: some View, _ box: StackBox, _ sc: StackScene, _ k: CGFloat) -> some View {
        piece
            .frame(width: CGFloat(box.w) * k, height: CGFloat(box.h) * k)
            .position(x: CGFloat(box.midX - sc.origin) * k, y: CGFloat(box.midY - sc.origin) * k)
    }

    /// The head and its face move and tilt as one.
    private func head(_ sc: StackScene, k: CGFloat, ink: Color) -> some View {
        let h = sc.head
        return ZStack {
            RoundedRectangle(cornerRadius: CGFloat(h.r) * k, style: .circular).fill(Color(hex: h.color))
            ZStack {
                ForEach(Array(sc.face.enumerated()), id: \.offset) { _, mark in
                    self.mark(mark, k: k, ink: ink)
                }
            }
            .blendMode(cutout ? .destinationOut : .normal)
            .id(sc.eyes)
            .transition(.opacity.animation(.linear(duration: 0.1)))
        }
        .frame(width: CGFloat(h.box.w) * k, height: CGFloat(h.box.h) * k)
        .rotationEffect(.degrees(sc.rot))
        .position(x: CGFloat(h.box.midX - sc.origin) * k, y: CGFloat(h.box.midY - sc.origin) * k)
    }

    @ViewBuilder
    private func mark(_ mark: StackMark, k: CGFloat, ink: Color) -> some View {
        switch mark {
        case let .dot(x, y, r, white):
            Circle()
                .fill(white && !cutout ? Color.white : ink)
                .frame(width: CGFloat(2 * r) * k, height: CGFloat(2 * r) * k)
                .offset(x: CGFloat(x) * k, y: CGFloat(y) * k)
        case let .line(x, y, bendX, bendY, run, width):
            GridPath(path: gridPath { p in
                p.move(to: CGPoint(x: x, y: y))
                p.addQuadCurve(to: CGPoint(x: x + run, y: y), control: CGPoint(x: x + bendX, y: y + bendY))
            }, k: k, centred: true)
            .stroke(ink, style: StrokeStyle(lineWidth: CGFloat(width) * k, lineCap: .round))
        }
    }

    private func badge(_ badge: StackBadge, k: CGFloat, origin: CGFloat, ink: Color) -> some View {
        ZStack {
            GridPath(path: Path(ellipseIn: CGRect(x: 79, y: 7, width: 16, height: 16)), k: k, origin: origin).fill(Color.white)
            switch badge {
            case .alert:
                GridPath(path: gridPath { p in
                    p.move(to: CGPoint(x: 87, y: 10.5))
                    p.addLine(to: CGPoint(x: 87, y: 16))
                }, k: k, origin: origin)
                .stroke(ink, style: StrokeStyle(lineWidth: 2.4 * k, lineCap: .round))
                GridPath(path: Path(ellipseIn: CGRect(x: 85.6, y: 18.2, width: 2.8, height: 2.8)), k: k, origin: origin).fill(ink)
            case .check:
                GridPath(path: gridPath { p in
                    p.move(to: CGPoint(x: 83, y: 15.2))
                    p.addLine(to: CGPoint(x: 85.8, y: 18))
                    p.addLine(to: CGPoint(x: 90.8, y: 12.4))
                }, k: k, origin: origin)
                .stroke(ink, style: StrokeStyle(lineWidth: 2.2 * k, lineCap: .round, lineJoin: .round))
            }
        }
    }

    /// The timing belongs to the state being entered. With reduced motion it is one short fade.
    private var motion: Animation {
        if still { return .easeOut(duration: 0.15) }
        switch state {
        case .listening: return .spring(response: 0.18, dampingFraction: 0.6)
        case .needsYou: return .spring(response: 0.3, dampingFraction: 0.55)
        case .done: return .spring(response: 0.2, dampingFraction: 0.6)
        case .asleep: return .easeInOut(duration: 0.6)
        case .idle, .working: return .easeOut(duration: 0.22)
        }
    }
}

struct CrestPicker: View {
    @Binding var value: Int32

    private var selection: (shape: Int, color: Int) { unpackCrest(value) }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Shape").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 8) {
                    ForEach(0..<SHAPE_COUNT, id: \.self) { shape in
                        let on = shape == selection.shape
                        let cell = RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
                        Button {
                            withAnimation(Motion.quiet(0.2)) { value = packCrest(shape: shape, color: selection.color) }
                        } label: {
                            CrestView(index: packCrest(shape: shape, color: selection.color), size: 36)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 6)
                                .background(on ? Theme.cobaltPale : .clear, in: cell)
                                .overlay(cell.strokeBorder(on ? Theme.cobalt : .clear, lineWidth: 1))
                        }
                        .buttonStyle(.press)
                        .accessibilityLabel(STACK_SHAPES[shape].name.capitalized)
                        .accessibilityAddTraits(on ? .isSelected : [])
                    }
                }
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("Color").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                HStack(spacing: 0) {
                    ForEach(0..<HUE_COUNT, id: \.self) { color in
                        let on = color == selection.color
                        let hue = STACK_HUES[color]
                        Button {
                            withAnimation(Motion.quiet(0.2)) { value = packCrest(shape: selection.shape, color: color) }
                        } label: {
                            // The ground is the swatch; the dot says which hue the dark ground belongs to.
                            Circle()
                                .fill(Color(hex: hue.ground))
                                .overlay(Circle().fill(Color(hex: hue.b)).frame(width: 10, height: 10))
                                .overlay(Circle().stroke(Theme.line, lineWidth: 1))
                                .overlay {
                                    if on {
                                        Circle().stroke(Theme.surface, lineWidth: 2).padding(-1)
                                        Circle().stroke(Theme.cobalt, lineWidth: 2).padding(-3)
                                    }
                                }
                                .frame(width: 30, height: 30)
                                .frame(maxWidth: .infinity)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.press)
                        .accessibilityLabel(hue.name.capitalized)
                        .accessibilityAddTraits(on ? .isSelected : [])
                    }
                }
            }
        }
        .sensoryFeedback(.selection, trigger: value)
    }
}

#if !SILO_EXTENSION
/// A select whose value is the Bot's crest: it sits beside a field and opens the picker in a sheet.
struct CrestSelect: View {
    @Binding var value: Int32
    @State private var open = false

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        Button { open = true } label: {
            HStack(spacing: 4) {
                CrestView(index: value, size: 34)
                Image(systemName: "chevron.down").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.leading, 4)
            .padding(.trailing, 8)
            .frame(height: 44)
            .background(Theme.surface, in: shape)
            .overlay(shape.strokeBorder(Theme.lineStrong, lineWidth: 1))
        }
        .buttonStyle(.press)
        .accessibilityLabel("Change crest")
        .sheet(isPresented: $open) {
            VStack(spacing: 0) {
                SheetHead(title: "Crest", cancel: "Done", onCancel: { open = false })
                CrestPicker(value: $value).padding(16)
                Spacer(minLength: 0)
            }
            .siloSheet(detents: [.medium])
        }
    }
}
#endif
