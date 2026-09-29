import SwiftUI
import UIKit

/// Silo tokens (from `DESIGN.md`) as adaptive colors. Native controls keep the system look;
/// these are used only where color carries meaning: cobalt is "you are here / act here",
/// emerald "the machine is alive", vermilion "it needs you" or destructive.
enum Theme {
    /// Picks `light` or `dark` by the current interface style.
    private static func adaptive(light: UInt32, dark: UInt32) -> Color {
        Color(UIColor { traits in
            UIColor(hex: traits.userInterfaceStyle == .dark ? dark : light)
        })
    }

    static let cobalt = adaptive(light: 0x2B4FC7, dark: 0x7B96F0)
    static let cobaltPale = adaptive(light: 0xE8EDFB, dark: 0x1B2544)
    static let emerald = adaptive(light: 0x0F7A55, dark: 0x3FBF8F)
    static let lamp = adaptive(light: 0x16A06F, dark: 0x3FBF8F)
    static let vermilion = adaptive(light: 0xC4372B, dark: 0xF07A6E)
    static let vermilionPale = adaptive(light: 0xFBEAE8, dark: 0x3A1D1A)

    /// Thread background and headers.
    static let canvas = adaptive(light: 0xFAF9F7, dark: 0x0E0E0D)
    /// Cards, the composer field, the approval slip.
    static let surface = adaptive(light: 0xFFFFFF, dark: 0x1A1917)
    /// User bubbles, inset groups, code blocks.
    static let well = adaptive(light: 0xF3F2EF, dark: 0x232220)
    /// Hairline ring around cards.
    static let line = adaptive(light: 0xE4E3E1, dark: 0x33312E)
    /// Dark inset for code, terminal, and preview wells (the "hatch").
    static let hatch = Color(uiColor: UIColor(hex: 0x141413))

    /// Crest-palette fills for Settings-style icon tiles.
    enum TileColor { case purple, orange, teal, graphite, wine }

    static func tile(_ color: TileColor) -> Color {
        switch color {
        case .purple: return Color(hex: 0x6D28D9)
        case .orange: return Color(hex: 0xEA580C)
        case .teal: return Color(hex: 0x3D6F6A)
        case .graphite: return Color(hex: 0x55534E)
        case .wine: return Color(hex: 0x9F1239)
        }
    }

    /// Corner radii from `DESIGN.md` (`control`, `card`, `bubble`).
    enum Radius {
        static let control: CGFloat = 12
        static let card: CGFloat = 20
        static let bubble: CGFloat = 22
    }
}

extension UIColor {
    convenience init(hex: UInt32) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: 1
        )
    }
}

extension View {
    /// A quiet filled card (no ring, continuous corners) for content inside the thread.
    func siloCard(padding: CGFloat = 12) -> some View {
        self.padding(padding)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}

extension Font {
    /// The Bot's words: a serif, so they look different from the interface (SF).
    static func reply(_ style: Font.TextStyle = .body) -> Font {
        .system(style, design: .serif)
    }
}
