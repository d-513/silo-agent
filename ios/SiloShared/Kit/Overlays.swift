import Observation
import SwiftUI

/// One floating thing at a time: a menu over the page. Native menus wear the system's glass, so
/// Silo draws its own, anchored to what opened it.
@MainActor
@Observable
final class OverlayCenter {
    struct Shown: Identifiable {
        let id = UUID()
        var anchor: CGRect
        var content: AnyView
    }

    fileprivate(set) var shown: Shown?

    func show(anchor: CGRect, @ViewBuilder content: () -> some View) {
        shown = Shown(anchor: anchor, content: AnyView(content()))
    }

    func dismiss() { shown = nil }
}

private struct OverlayHost: ViewModifier {
    @State private var center = OverlayCenter()

    func body(content: Content) -> some View {
        content
            .environment(center)
            .overlay { Layer(center: center).ignoresSafeArea() }
    }

    private struct Layer: View {
        let center: OverlayCenter
        @State private var height: CGFloat = 0

        var body: some View {
            GeometryReader { box in
                let origin = box.frame(in: .global).origin
                ZStack(alignment: .topLeading) {
                    if let shown = center.shown {
                        Color.clear
                            .contentShape(Rectangle())
                            .onTapGesture { dismiss() }
                        let width: CGFloat = min(280, box.size.width - 24)
                        let below = shown.anchor.maxY + 6 + height < box.size.height - 24
                        let x = min(max(12, shown.anchor.midX - origin.x - width / 2), box.size.width - width - 12)
                        let y = below ? shown.anchor.maxY - origin.y + 6 : shown.anchor.minY - origin.y - 6 - height
                        shown.content
                            .frame(width: width, alignment: .leading)
                            .background(GeometryReader { Color.clear.preference(key: Height.self, value: $0.size.height) })
                            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
                            .slipShadow()
                            .offset(x: x, y: y)
                            .transition(.opacity.combined(with: .scale(scale: 0.97, anchor: below ? .top : .bottom)))
                            .id(shown.id)
                    }
                }
                .onPreferenceChange(Height.self) { height = $0 }
                .animation(Motion.settle(0.2), value: center.shown?.id)
            }
        }

        private func dismiss() { center.dismiss() }
    }

    private struct Height: PreferenceKey {
        static let defaultValue: CGFloat = 0
        static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
    }
}

extension View {
    /// Where menus float: the root, and each sheet (a sheet covers the root).
    func siloOverlayHost() -> some View { modifier(OverlayHost()) }
}

/// One line of a menu; pressing it closes the menu.
struct MenuRow: View {
    @Environment(OverlayCenter.self) private var center: OverlayCenter?
    let title: String
    var detail: String?
    var symbol: String?
    var checked = false
    var destructive = false
    var enabled = true
    let action: () -> Void

    var body: some View {
        Button {
            center?.dismiss()
            action()
        } label: {
            HStack(spacing: 10) {
                if let symbol {
                    Image(systemName: symbol).font(.system(size: 14)).frame(width: 18)
                        .foregroundStyle(destructive ? Theme.vermilion : Theme.ink2)
                }
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(.silo(14.5)).foregroundStyle(destructive ? Theme.vermilion : enabled ? Theme.ink : Theme.ink3).lineLimit(2)
                    if let detail, !detail.isEmpty { Text(detail).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1) }
                }
                Spacer(minLength: 8)
                if checked {
                    Image(systemName: "checkmark").font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.cobalt)
                }
            }
            .padding(.horizontal, 12)
            .frame(minHeight: 42)
            .contentShape(Rectangle())
        }
        .buttonStyle(RowPressStyle())
        .disabled(!enabled)
    }
}

struct MenuDivider: View {
    var body: some View { Theme.line.frame(height: 1).padding(.vertical, 4) }
}

struct MenuHeading: View {
    let text: String
    var body: some View { SectionLabel(text: text).padding(.horizontal, 12).padding(.top, 8).padding(.bottom, 2) }
}

/// A button that opens a Silo menu. Items are `MenuRow`s.
struct SiloMenu<Label: View, Items: View>: View {
    @Environment(OverlayCenter.self) private var center: OverlayCenter?
    @ViewBuilder let items: () -> Items
    @ViewBuilder let label: () -> Label
    @State private var frame: CGRect = .zero

    var body: some View {
        Button {
            center?.show(anchor: frame) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) { items() }.padding(.vertical, 4)
                }
                .frame(maxHeight: 420)
                .fixedSize(horizontal: false, vertical: true)
                .environment(center)
            }
        } label: {
            label()
                .contentShape(Rectangle())
                .background(GeometryReader { box in
                    Color.clear.onChange(of: box.frame(in: .global), initial: true) { _, new in frame = new }
                })
        }
        .buttonStyle(.press)
        .sensoryFeedback(.selection, trigger: center?.shown?.id)
    }
}

/// A choice from a list: the current one in the field, the list in a Silo menu.
struct SiloPicker<Value: Hashable>: View {
    var label: String?
    let options: [(value: Value, label: String)]
    @Binding var selection: Value

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let label { Text(label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3) }
            SiloMenu {
                ForEach(options, id: \.value) { option in
                    MenuRow(title: option.label, checked: option.value == selection) { selection = option.value }
                }
            } label: {
                HStack(spacing: 8) {
                    Text(options.first { $0.value == selection }?.label ?? "Choose…")
                        .font(.silo(15))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.ink3)
                }
                .padding(.horizontal, 12)
                .frame(height: 44)
                .siloInputFrame()
            }
        }
    }
}

extension View {
    /// A sheet in Silo's clothes: a `surface` slip, no system glass, its own place for menus.
    func siloSheet(detents: Set<PresentationDetent> = [.large]) -> some View {
        self
            .siloOverlayHost()
            .presentationDetents(detents)
            .presentationBackground(Theme.surface)
            .presentationCornerRadius(Theme.Radius.panel)
            .presentationDragIndicator(.hidden)
            .tint(Theme.cobalt)
    }
}

/// A sheet's head: Cancel, the title, and its one action. Nothing of the system's.
struct SheetHead<Confirm: View>: View {
    let title: String
    var cancel = "Cancel"
    var onCancel: (() -> Void)?
    @ViewBuilder var confirm: Confirm

    init(title: String, cancel: String = "Cancel", onCancel: (() -> Void)? = nil, @ViewBuilder confirm: () -> Confirm = { EmptyView() }) {
        self.title = title
        self.cancel = cancel
        self.onCancel = onCancel
        self.confirm = confirm()
    }

    var body: some View {
        ZStack {
            Text(title).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1).padding(.horizontal, 88)
            HStack {
                if let onCancel {
                    Button(cancel, action: onCancel).buttonStyle(.ghost).controlSize(.small)
                }
                Spacer()
                confirm.controlSize(.small)
            }
        }
        .frame(height: 52)
        .padding(.horizontal, 10)
        .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
        .background(Theme.surface)
    }
}

/// The confirm of last resort, for a press from a menu that has no button to arm: a small slip with
/// one sentence and the armed action.
struct ConfirmSlip: ViewModifier {
    @Binding var isPresented: Bool
    let title: String
    var message: String?
    let action: String
    let perform: () -> Void

    func body(content: Content) -> some View {
        content.sheet(isPresented: $isPresented) {
            VStack(alignment: .leading, spacing: 8) {
                Text(title).font(.silo(19, .medium)).foregroundStyle(Theme.ink)
                if let message { Text(message).font(.silo(14)).foregroundStyle(Theme.ink2) }
                Spacer(minLength: 8)
                ArmedButton(title: action, wide: true) {
                    isPresented = false
                    perform()
                }
                Button("Cancel") { isPresented = false }.buttonStyle(.ghost).frame(maxWidth: .infinity)
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .siloSheet(detents: [.height(message == nil ? 190 : 230)])
        }
    }
}

/// A one-field prompt, for a rename: it never leaves the room the way an alert does.
struct PromptSlip: ViewModifier {
    @Binding var isPresented: Bool
    let title: String
    var prompt = ""
    var action = "Save"
    @Binding var text: String
    let perform: () -> Void

    func body(content: Content) -> some View {
        content.sheet(isPresented: $isPresented) {
            VStack(alignment: .leading, spacing: 14) {
                Text(title).font(.silo(19, .medium)).foregroundStyle(Theme.ink)
                SiloField(prompt: prompt, text: $text)
                HStack(spacing: 8) {
                    Button("Cancel") { isPresented = false }.buttonStyle(.quiet).frame(maxWidth: .infinity)
                    Button(action) {
                        isPresented = false
                        perform()
                    }
                    .buttonStyle(.ink)
                    .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .siloSheet(detents: [.height(196)])
        }
    }
}

extension View {
    func confirmSlip(_ isPresented: Binding<Bool>, title: String, message: String? = nil, action: String, perform: @escaping () -> Void) -> some View {
        modifier(ConfirmSlip(isPresented: isPresented, title: title, message: message, action: action, perform: perform))
    }

    func promptSlip(_ isPresented: Binding<Bool>, title: String, prompt: String = "", action: String = "Save", text: Binding<String>, perform: @escaping () -> Void) -> some View {
        modifier(PromptSlip(isPresented: isPresented, title: title, prompt: prompt, action: action, text: text, perform: perform))
    }
}

private struct LongPressMenu<Items: View>: ViewModifier {
    @Environment(OverlayCenter.self) private var center: OverlayCenter?
    let enabled: Bool
    @ViewBuilder let items: () -> Items
    @State private var frame: CGRect = .zero
    @State private var pressed = 0

    func body(content: Content) -> some View {
        content
            .background(GeometryReader { box in
                Color.clear.onChange(of: box.frame(in: .global), initial: true) { _, new in frame = new }
            })
            .onLongPressGesture(minimumDuration: 0.35) {
                guard enabled, let center else { return }
                pressed += 1
                // Under a tall row the anchor is where the row's foot is on screen.
                let anchor = frame.height > 200 ? CGRect(x: frame.midX, y: frame.minY + 60, width: 0, height: 0) : frame
                center.show(anchor: anchor) {
                    VStack(alignment: .leading, spacing: 0) { items() }.padding(.vertical, 4).environment(center)
                }
            }
            .sensoryFeedback(.impact(weight: .medium), trigger: pressed)
    }
}

extension View {
    /// A press and hold opens a Silo menu here, in place of the system's context menu.
    func longPressMenu(enabled: Bool = true, @ViewBuilder _ items: @escaping () -> some View) -> some View {
        modifier(LongPressMenu(enabled: enabled, items: items))
    }
}
