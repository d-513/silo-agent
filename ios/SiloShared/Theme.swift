import SiloClient
import SwiftUI
import UIKit

/// Silo tokens (from `DESIGN.md` and `web/src/index.css`) as adaptive colors: paper canvas, ink
/// type, one quiet cobalt for "you are here", emerald "the machine is alive", vermilion "it needs
/// you" or destructive. Light and dark share the names.
enum Theme {
    /// Picks `light` or `dark` by the current interface style.
    private static func adaptive(light: UInt32, dark: UInt32, alpha: (CGFloat, CGFloat) = (1, 1)) -> Color {
        Color(UIColor { traits in
            traits.userInterfaceStyle == .dark ? UIColor(hex: dark, alpha: alpha.1) : UIColor(hex: light, alpha: alpha.0)
        })
    }

    static let cobalt = adaptive(light: 0x2B4FC7, dark: 0x8AA4FF)
    static let cobaltPale = adaptive(light: 0xE8EDFB, dark: 0x283258)
    static let emerald = adaptive(light: 0x0F7A55, dark: 0x4FC38D)
    static let lamp = adaptive(light: 0x16A06F, dark: 0x2FB880)
    static let vermilion = adaptive(light: 0xC4372B, dark: 0xF27565)
    static let vermilionPale = adaptive(light: 0xFBEAE8, dark: 0x3D2320)

    /// Type: `ink` for what you read, `ink2` for supporting text, `ink3` for meta and glyphs at rest.
    static let ink = adaptive(light: 0x1A1917, dark: 0xEDEBE6)
    static let ink2 = adaptive(light: 0x55534E, dark: 0xB5B2AB)
    static let ink3 = adaptive(light: 0x6B6862, dark: 0x9B9891)
    /// A label on an ink fill.
    static let onInk = adaptive(light: 0xFFFFFF, dark: 0x171615)
    /// A label on a cobalt, emerald or vermilion fill.
    static let onAccent = adaptive(light: 0xFFFFFF, dark: 0x171615)
    /// Text on the hatch, which is dark in both themes.
    static let onHatch = Color(uiColor: UIColor(hex: 0xFAF9F7))
    /// Hover and press of an ink fill.
    static let inkDeep = adaptive(light: 0x000000, dark: 0xFFFFFF)

    /// The page.
    static let canvas = adaptive(light: 0xFAF9F7, dark: 0x171615)
    /// Cards, the composer field, the approval slip.
    static let surface = adaptive(light: 0xFFFFFF, dark: 0x252422)
    /// The sidebar, user bubbles, inset groups, code blocks.
    static let well = adaptive(light: 0xF3F2EF, dark: 0x1E1D1B)
    /// A pressed control.
    static let pressed = adaptive(light: 0xEAE8E4, dark: 0x31302D)
    /// Hairlines.
    static let line = adaptive(light: 0x1A1917, dark: 0xEDEBE6, alpha: (0.09, 0.10))
    static let lineStrong = adaptive(light: 0x1A1917, dark: 0xEDEBE6, alpha: (0.16, 0.18))
    /// Dark inset for code, terminal, and preview wells (the "hatch").
    static let hatch = adaptive(light: 0x141413, dark: 0x0E0E0D)
    static let matte = adaptive(light: 0x0E0E0D, dark: 0x080808)
    /// Shadows: heavier in the dark, where the ring does the seating.
    static let shade = adaptive(light: 0x14120E, dark: 0x000000)

    /// Corner radii from `DESIGN.md`.
    enum Radius {
        static let xs: CGFloat = 6
        static let sm: CGFloat = 8
        static let control: CGFloat = 10
        static let card: CGFloat = 14
        static let panel: CGFloat = 16
        static let composer: CGFloat = 18
        static let bubble: CGFloat = 18
    }

    /// A status word's color.
    static func color(_ tone: WordTone) -> Color {
        switch tone {
        case .emerald: return emerald
        case .vermilion: return vermilion
        case .ink2: return ink2
        case .ink3: return ink3
        }
    }
}

extension UIColor {
    convenience init(hex: UInt32, alpha: CGFloat = 1) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: alpha
        )
    }
}

// MARK: - Type

extension Font {
    /// Geist at a text style's size, scaling with Dynamic Type like the system styles it replaces.
    static func silo(_ style: Font.TextStyle, _ weight: Font.Weight? = nil) -> Font {
        let (size, standard) = siloSize(style)
        return .custom("Geist", size: size, relativeTo: style).weight(weight ?? standard)
    }

    static func siloMono(_ style: Font.TextStyle, _ weight: Font.Weight = .regular) -> Font {
        .custom("Geist Mono", size: siloSize(style).0 - 1, relativeTo: style).weight(weight)
    }

    private static func siloSize(_ style: Font.TextStyle) -> (CGFloat, Font.Weight) {
        switch style {
        case .largeTitle: return (32, .medium)
        case .title: return (26, .medium)
        case .title2: return (22, .medium)
        case .title3: return (19, .medium)
        case .headline: return (16, .semibold)
        case .body: return (16, .regular)
        case .callout: return (15, .regular)
        case .subheadline: return (14, .regular)
        case .footnote: return (13, .regular)
        case .caption: return (12, .regular)
        case .caption2: return (11, .regular)
        default: return (16, .regular)
        }
    }

    /// Geist, the interface face. Sizes follow Dynamic Type relative to body.
    static func silo(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .custom("Geist", size: size, relativeTo: .body).weight(weight)
    }

    /// Geist Mono: paths, times, counts, code.
    static func siloMono(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .custom("Geist Mono", size: size, relativeTo: .body).weight(weight)
    }

    /// The Bot's words: Source Serif 4, so they read differently from the interface.
    static func reply(_ size: CGFloat = 17) -> Font {
        .custom("Source Serif 4", size: size, relativeTo: .body)
    }
}

// MARK: - Surfaces

extension View {
    /// A quiet filled card (no ring, continuous corners) for content inside the thread.
    func siloCard(padding: CGFloat = 12) -> some View {
        self.padding(padding)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}
