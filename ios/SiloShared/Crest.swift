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
]

let SHAPE_COUNT = 8
let CREST_SHAPE_NAMES = ["Bobble hat", "Bent tip", "Wide brim", "Paper boat", "Cone", "Striped cone", "Baker's toque", "Stocking cap"]

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

/// A Bot's skrzat. The images come from `make brand`: small ones in every
/// target, large ones in the app, because the rough felt edge is not something
/// a SwiftUI shape can draw and an extension must stay light.
struct CrestView: View {
    let index: Int32
    var size: CGFloat = 40

    private var asset: String {
        let crest = unpackCrest(index)
        let name = crest.color * SHAPE_COUNT + crest.shape
        #if SILO_EXTENSION
        return "crest-\(name)"
        #else
        return size > 44 ? "crest-large-\(name)" : "crest-\(name)"
        #endif
    }

    var body: some View {
        Image(asset)
            .resizable()
            .interpolation(.high)
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
                Text("Hat").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
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
                        .accessibilityLabel(CREST_SHAPE_NAMES[shape])
                        .accessibilityAddTraits(on ? .isSelected : [])
                    }
                }
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("Color").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 6), spacing: 12) {
                    ForEach(0..<CREST_COLORS.count, id: \.self) { color in
                        let on = color == selection.color
                        Button {
                            withAnimation(Motion.quiet(0.2)) { value = packCrest(shape: selection.shape, color: color) }
                        } label: {
                            Circle()
                                .fill(CREST_COLORS[color])
                                .overlay(Circle().stroke(Theme.line, lineWidth: 1))
                                .overlay {
                                    if on {
                                        Circle().stroke(Theme.surface, lineWidth: 2).padding(-1)
                                        Circle().stroke(Theme.cobalt, lineWidth: 2).padding(-3)
                                    }
                                }
                                .frame(height: 30)
                                .frame(maxWidth: .infinity)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.press)
                        .accessibilityLabel("Color \(color + 1)")
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
