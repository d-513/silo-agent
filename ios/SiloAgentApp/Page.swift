import SwiftUI
import UIKit

/// The frame of every page but the chat: a head on `canvas` with Back, the title and the page's actions.
/// The system bar is gone: its glass is not Silo's. A page says what goes in the head with
/// `.pageActions { … }`, and scrolls with `PageScroll`.
struct SiloPage<Content: View>: View {
    let title: String
    var subtitle: String?
    /// False at the root of a stack, where there is nothing to go back to.
    var back = true
    /// At a root with no Bot's top bar over it: the sidebar's menu glyph in Back's place.
    var menu = false
    @ViewBuilder let content: Content
    @Environment(\.dismiss) private var dismiss
    @State private var hasActions = false

    /// A page the Bot's top bar already names (one the rail opened) draws its head only for its actions.
    private var named: Bool { !back && !menu }

    var body: some View {
        content
            .safeAreaPadding(.top, named && !hasActions ? 0 : 52)
            .onPreferenceChange(PageHasActionsKey.self) { hasActions = $0 }
            .overlayPreferenceValue(PageActionsKey.self, alignment: .top) { actions in
                if !named || actions != nil { head(actions) }
            }
            .background(Theme.canvas.ignoresSafeArea())
            .toolbar(.hidden, for: .navigationBar)
            .font(.silo(15))
    }

    private func head(_ actions: PageActions?) -> some View {
        HStack(spacing: 2) {
            if back {
                Button { dismiss() } label: {
                    Image(systemName: "chevron.left").font(.system(size: 15, weight: .medium))
                }
                .buttonStyle(.glyph(tint: Theme.ink))
                .accessibilityLabel("Back")
            } else {
                if menu { MenuGlyph() }
                Color.clear.frame(width: 10, height: 40)
            }
            VStack(alignment: .leading, spacing: 0) {
                Text(named ? "" : title).font(.silo(16, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let actions { HStack(spacing: 2) { actions.view } }
        }
        .padding(.horizontal, 6)
        .frame(height: 52)
        .background(Theme.canvas)
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
    }
}

struct PageActions {
    let view: AnyView
}

struct PageHasActionsKey: PreferenceKey {
    static let defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) { value = value || nextValue() }
}

struct PageActionsKey: PreferenceKey {
    static let defaultValue: PageActions? = nil
    static func reduce(value: inout PageActions?, nextValue: () -> PageActions?) { value = nextValue() ?? value }
}

extension View {
    /// Glyph buttons or a small button for the page's head.
    func pageActions(@ViewBuilder _ actions: () -> some View) -> some View {
        preference(key: PageActionsKey.self, value: PageActions(view: AnyView(actions())))
            .preference(key: PageHasActionsKey.self, value: true)
    }
}

/// A page's body: one column on paper, pads `md`, panels 16pt apart.
struct PageScroll<Content: View>: View {
    var spacing: CGFloat = 16
    @ViewBuilder let content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: spacing) { content }
                .padding(.horizontal, 16)
                .padding(.top, 14)
                .padding(.bottom, 32)
                .frame(maxWidth: 760, alignment: .leading)
                .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
    }
}

/// Swiping back stays when the system bar is hidden.
extension UINavigationController: @retroactive UIGestureRecognizerDelegate {
    override open func viewDidLoad() {
        super.viewDidLoad()
        interactivePopGestureRecognizer?.delegate = self
    }

    public func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        viewControllers.count > 1
    }
}
