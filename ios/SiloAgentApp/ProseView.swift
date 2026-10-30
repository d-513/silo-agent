import SiloClient
import SwiftUI

/// The Bot's words as Markdown blocks: paragraphs in the reply serif, headings in it at 500, lists,
/// quotes, `well` code and tables that scroll sideways instead of widening the thread. `size` and
/// `leading` are the reply's 16.5/28 unless a place sets it smaller (an ask's question is 16/26).
struct ProseView: View {
    let text: String
    var size: CGFloat = 16.5
    var leading: CGFloat = 28
    var tone: Color = Theme.ink

    var body: some View {
        BlocksView(blocks: Markdown.parse(text), type: ProseType(size: size, leading: leading))
            .foregroundStyle(tone)
            .tint(Theme.cobalt)
    }
}

/// What a block is set in. The tracking is the reply's -0.003em.
private struct ProseType {
    var size: CGFloat
    var leading: CGFloat

    var font: Font { .reply(size) }
    var tracking: CGFloat { size * -0.003 }

    /// A heading: 22/28, 18/26, then the text's own size, all at 500.
    func heading(_ level: Int) -> ProseType {
        switch level {
        case 1: ProseType(size: size * 4 / 3, leading: leading)
        case 2: ProseType(size: size * 12 / 11, leading: leading * 13 / 14)
        default: self
        }
    }
}

/// Lines a fixed distance apart, so a code span cannot make one taller; it follows Dynamic Type.
private struct Leading: ViewModifier {
    let points: CGFloat
    @ScaledMetric(relativeTo: .body) private var scale: CGFloat = 1

    func body(content: Content) -> some View {
        content.lineHeight(.exact(points: points * scale))
    }
}

private extension View {
    func leading(_ points: CGFloat) -> some View { modifier(Leading(points: points)) }
}

private struct BlocksView: View {
    let blocks: [MarkdownBlock]
    let type: ProseType

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { index, block in
                BlockContent(block: block, type: type)
                    .padding(.top, gap(before: index))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// A heading keeps its distance from what came before and sits close to what it heads.
    private func gap(before index: Int) -> CGFloat {
        guard index > 0 else { return 0 }
        if case .heading = blocks[index - 1] { return 6 }
        if case .heading = blocks[index] { return 14 }
        return 10
    }
}

private struct BlockContent: View {
    let block: MarkdownBlock
    let type: ProseType

    var body: some View {
        switch block {
        case .paragraph(let value):
            Text(inline(value, type))
                .font(type.font)
                .tracking(type.tracking)
                .leading(type.leading)
                .textSelection(.enabled)
        case .heading(let level, let value):
            let heading = type.heading(level)
            Text(inline(value, heading, base: .medium))
                .font(heading.font.weight(.medium))
                .tracking(heading.tracking)
                .leading(heading.leading)
                .textSelection(.enabled)
                .accessibilityAddTraits(.isHeader)
        case .list(_, let items):
            VStack(alignment: .leading, spacing: 3) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    ItemRow(item: item, type: type)
                }
            }
        case .quote(let inner):
            BlocksView(blocks: inner, type: type)
                .foregroundStyle(Theme.ink2)
                .padding(.leading, 14)
                .overlay(alignment: .leading) { Theme.lineStrong.frame(width: 2) }
        case .code(let value, let language):
            CodeWell(code: value, language: language)
        case .table(let header, let align, let rows):
            TableView(header: header, align: align, rows: rows)
        case .rule:
            Theme.line.frame(height: 1).padding(.vertical, 4)
        }
    }
}

private struct ItemRow: View {
    let item: MarkdownItem
    let type: ProseType

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Group {
                if let checked = item.checked {
                    Image(systemName: checked ? "checkmark.square" : "square")
                        .font(.system(size: type.size * 0.85))
                } else {
                    Text(item.marker).font(type.font).monospacedDigit()
                }
            }
            .foregroundStyle(Theme.ink2)
            .frame(minWidth: 14, alignment: .trailing)
            Text(inline(item.text, type))
                .font(type.font)
                .tracking(type.tracking)
                .leading(type.leading)
                .strikethrough(item.checked == true, color: Theme.ink3)
                .textSelection(.enabled)
        }
        .padding(.leading, CGFloat(item.level) * 18)
    }
}

/// A GFM table, set in Geist like the web's: a `well` head, hairline rows. It fills the column while
/// it fits, and past that it scrolls inside the thread; it never widens the column.
private struct TableView: View {
    let header: [String]
    let align: [MarkdownAlign]
    let rows: [[String]]

    private static let type = ProseType(size: 14, leading: 22)

    var body: some View {
        ViewThatFits(in: .horizontal) {
            grid
            ScrollView(.horizontal, showsIndicators: false) { grid }
                .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        }
    }

    private var grid: some View {
        Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
            GridRow {
                ForEach(header.indices, id: \.self) { column in
                    cell(header[column], column, head: true)
                }
            }
            ForEach(rows.indices, id: \.self) { index in
                GridRow {
                    ForEach(header.indices, id: \.self) { column in
                        cell(column < rows[index].count ? rows[index][column] : "", column, head: false)
                    }
                }
            }
        }
        // Each cell fills its row, so a row must not take more height than its tallest cell needs.
        .fixedSize(horizontal: false, vertical: true)
    }

    private func cell(_ value: String, _ column: Int, head: Bool) -> some View {
        let side: MarkdownAlign = column < align.count ? align[column] : .leading
        let alignment: Alignment = side == .trailing ? .trailing : side == .center ? .center : .leading
        return Text(inline(value, Self.type, serif: false))
            .font(.silo(14, head ? .medium : .regular))
            .foregroundStyle(Theme.ink)
            .multilineTextAlignment(side == .trailing ? .trailing : side == .center ? .center : .leading)
            .lineLimit(head ? 1 : nil)
            .leading(Self.type.leading)
            .textSelection(.enabled)
            .modifier(CellWidth(limit: head ? .infinity : 260))
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: alignment)
            .background(head ? Theme.well : .clear)
            .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
            .gridColumnAlignment(side == .trailing ? .trailing : side == .center ? .center : .leading)
    }
}

/// A cell is as wide as its text wants, up to `limit`, where it wraps. A plain `maxWidth` frame cannot
/// say this: a scroll view proposes no width at all, so the text would never be asked to wrap.
private struct CellWidth: ViewModifier {
    let limit: CGFloat

    func body(content: Content) -> some View {
        CappedWidth(limit: limit) { content }
    }

    private struct CappedWidth: Layout {
        let limit: CGFloat

        func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
            guard let cell = subviews.first else { return .zero }
            let width = min(cell.sizeThatFits(.unspecified).width, limit)
            return cell.sizeThatFits(ProposedViewSize(width: width, height: nil))
        }

        func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
            subviews.first?.place(at: bounds.origin, proposal: ProposedViewSize(width: bounds.width, height: bounds.height))
        }
    }
}

/// Inline Markdown (emphasis, code spans, links) for one block's text. Links take the tint and an
/// underline, a code span is `well` mono at .8em, and strong is 600, not the system's bold.
private func inline(_ value: String, _ type: ProseType, base: Font.Weight = .regular, serif: Bool = true) -> AttributedString {
    let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
    var out = (try? AttributedString(markdown: value, options: options)) ?? AttributedString(value)
    for run in out.runs {
        if run.link != nil { out[run.range].swiftUI.underlineStyle = .single }
        guard var intent = run.inlinePresentationIntent else { continue }
        if intent.contains(.code) {
            out[run.range].swiftUI.font = .siloMono(type.size * 0.8)
            out[run.range].swiftUI.backgroundColor = Theme.well
            intent.remove(.code)
        } else if intent.contains(.stronglyEmphasized) || intent.contains(.emphasized) {
            let strong = intent.contains(.stronglyEmphasized)
            var font = serif ? type.font.weight(strong ? .semibold : base) : Font.silo(type.size, strong ? .semibold : base)
            if intent.contains(.emphasized) { font = font.italic() }
            out[run.range].swiftUI.font = font
            intent.remove(.stronglyEmphasized)
            intent.remove(.emphasized)
        }
        out[run.range].inlinePresentationIntent = intent.isEmpty ? nil : intent
    }
    return out
}
