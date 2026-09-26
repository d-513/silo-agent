import SwiftUI

let CREST_COLORS: [Color] = [
    Color(hex: 0xF3F2EF), // mist
    Color(hex: 0x8B5A3C), // brown
    Color(hex: 0x9F1239), // wine
    Color(hex: 0xEA580C), // orange
    Color(hex: 0xE0AE1C), // yellow
    Color(hex: 0x16A34A), // green
    Color(hex: 0x0F7A55), // emerald
    Color(hex: 0x2B4FC7), // cobalt
    Color(hex: 0x6D28D9), // purple
    Color(hex: 0xDB2777), // pink
    Color(hex: 0x55534E), // graphite
    Color(hex: 0x1A1917), // ink
] = [
    Color(hex: 0xF3F0E8), // plaster
    Color(hex: 0x7A4E2A), // brown
    Color(hex: 0xA33B4A), // carmine
    Color(hex: 0xE07A2F), // orange
    Color(hex: 0xE4C04A), // yellow
    Color(hex: 0x4A8F4A), // green
    Color(hex: 0x3D6F6A), // pine
    Color(hex: 0x2A3F5F), // bindery
    Color(hex: 0x6B4C8A), // purple
    Color(hex: 0xD47A8C), // pink
    Color(hex: 0x5F5E58), // stone
    Color(hex: 0x1E2126), // iron
]

let SHAPE_COUNT = 8
let CREST_SHAPE_NAMES = ["circle", "blob", "squircle", "pill", "triangle", "hex", "cloud", "drop"]

func packCrest(shape: Int, color: Int) -> Int32 {
    let s = ((shape % SHAPE_COUNT) + SHAPE_COUNT) % SHAPE_COUNT
    let c = ((color % CREST_COLORS.count) + CREST_COLORS.count) % CREST_COLORS.count
    return Int32(c * SHAPE_COUNT + s)
}

func unpackCrest(_ index: Int32) -> (shape: Int, color: Int) {
    let n = SHAPE_COUNT * CREST_COLORS.count
    let i = ((Int(index) % n) + n) % n
    return (shape: i % SHAPE_COUNT, color: i / SHAPE_COUNT)
}

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

// Ink eyes on the two light fills (mist, yellow); canvas eyes on the rest.
private func crestIsLight(_ index: Int32) -> Bool {
    let color = unpackCrest(index).color
    return color == 0 || color == 4
}

/// The crest body, drawn in a 48x48 design space and scaled to `rect`.
nonisolated struct CrestShape: Shape {
    let shape: Int

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 48
        let ox = rect.midX - 24 * s
        let oy = rect.midY - 24 * s
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: ox + x * s, y: oy + y * s) }
        func r(_ x: CGFloat, _ y: CGFloat, _ w: CGFloat, _ h: CGFloat) -> CGRect {
            CGRect(x: ox + x * s, y: oy + y * s, width: w * s, height: h * s)
        }

        var path = Path()
        switch shape {
        case 1:
            path.move(to: p(25, 6.5))
            path.addQuadCurve(to: p(41.5, 22), control: p(40, 8))
            path.addQuadCurve(to: p(23, 41.5), control: p(43.5, 40))
            path.addQuadCurve(to: p(6.5, 25), control: p(5, 42))
            path.addQuadCurve(to: p(25, 6.5), control: p(7, 8))
            path.closeSubpath()
        case 2:
            path.addRoundedRect(in: r(8, 8, 32, 32), cornerSize: CGSize(width: 11 * s, height: 11 * s))
        case 3:
            path.addRoundedRect(in: r(5, 15, 38, 18), cornerSize: CGSize(width: 9 * s, height: 9 * s))
        case 4:
            path.move(to: p(24, 6.5))
            path.addLine(to: p(41, 39.5))
            path.addLine(to: p(7, 39.5))
            path.closeSubpath()
        case 5:
            path.move(to: p(24, 6.5))
            path.addLine(to: p(39.2, 15.4))
            path.addLine(to: p(39.2, 32.6))
            path.addLine(to: p(24, 41.5))
            path.addLine(to: p(8.8, 32.6))
            path.addLine(to: p(8.8, 15.4))
            path.closeSubpath()
        case 6:
            path.addEllipse(in: r(9, 16, 17, 15))
            path.addEllipse(in: r(16, 11, 18, 17))
            path.addEllipse(in: r(24, 17, 16, 14))
            path.addRoundedRect(in: r(10, 24, 28, 9), cornerSize: CGSize(width: 4.5 * s, height: 4.5 * s))
        case 7:
            path.move(to: p(24, 6.8))
            path.addCurve(to: p(39.4, 31), control1: p(31.6, 15), control2: p(39.4, 22.8))
            path.addCurve(to: p(24, 42.6), control1: p(39.4, 38.2), control2: p(33, 42.6))
            path.addCurve(to: p(8.6, 31), control1: p(15, 42.6), control2: p(8.6, 38.2))
            path.addCurve(to: p(24, 6.8), control1: p(8.6, 22.8), control2: p(16.4, 15))
            path.closeSubpath()
        default:
            path.addEllipse(in: r(7.5, 7.5, 33, 33))
        }
        return path
    }
}

/// The two dots. Same 48x48 space so they line up with `CrestShape`.
nonisolated struct CrestEyes: Shape {
    let shape: Int

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 48
        let ox = rect.midX - 24 * s
        let oy = rect.midY - 24 * s
        let (cx, cy, spread): (CGFloat, CGFloat, CGFloat) = {
            switch shape {
            case 1: return (23.2, 20, 4.6)
            case 3: return (24, 24, 6.2)
            case 4: return (24, 26.5, 4.6)
            case 5: return (24, 22, 4.6)
            case 6: return (24, 24.5, 4.6)
            case 7: return (24, 27, 4.6)
            default: return (24, 21, 4.6)
            }
        }()

        var path = Path()
        let radius = 1.65 * s
        for dx in [-spread, spread] {
            let center = CGPoint(x: ox + (cx + dx) * s, y: oy + cy * s)
            path.addEllipse(in: CGRect(
                x: center.x - radius,
                y: center.y - radius,
                width: radius * 2,
                height: radius * 2
            ))
        }
        return path
    }
}

struct CrestView: View {
    let index: Int32
    var size: CGFloat = 40

    var body: some View {
        let shape = unpackCrest(index).shape
        let fill = CREST_COLORS[unpackCrest(index).color]
        let light = crestIsLight(index)
        let eyeColor = light ? Color(hex: 0x1A1917) : Color(hex: 0xFAF9F7)
        ZStack {
            // Only mist takes the hairline outline (line-strong on canvas).
            if unpackCrest(index).color == 0 {
                CrestShape(shape: shape).stroke(Color(hex: 0xD8D7D4), lineWidth: max(1, size / 48))
            }
            CrestShape(shape: shape).fill(fill)
            CrestEyes(shape: shape).fill(eyeColor)
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

struct CrestPicker: View {
    @Binding var value: Int32

    private var selection: (shape: Int, color: Int) { unpackCrest(value) }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Shape").font(.caption).foregroundStyle(.secondary)
                LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 4), spacing: 8) {
                    ForEach(0..<SHAPE_COUNT, id: \.self) { shape in
                        Button {
                            value = packCrest(shape: shape, color: selection.color)
                        } label: {
                            CrestView(index: packCrest(shape: shape, color: selection.color), size: 36)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 6)
                                .background(
                                    RoundedRectangle(cornerRadius: 8)
                                        .fill(shape == selection.shape ? Color.accentColor.opacity(0.15) : .clear)
                                )
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(CREST_SHAPE_NAMES[shape])
                    }
                }
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("Color").font(.caption).foregroundStyle(.secondary)
                LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 6), spacing: 10) {
                    ForEach(0..<CREST_COLORS.count, id: \.self) { color in
                        Button {
                            value = packCrest(shape: selection.shape, color: color)
                        } label: {
                            Circle()
                                .fill(CREST_COLORS[color])
                                .frame(height: 30)
                                .overlay(Circle().stroke(.quaternary, lineWidth: 1))
                                .overlay {
                                    if color == selection.color {
                                        Circle().stroke(Color.accentColor, lineWidth: 2.5)
                                    }
                                }
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Color \(color + 1)")
                    }
                }
            }
        }
    }
}
