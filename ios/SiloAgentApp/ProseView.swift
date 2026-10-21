import SiloClient
import SwiftUI

/// The Bot's words as Markdown blocks: paragraphs in the reply serif, headings, lists, quotes,
/// code wells and tables that scroll sideways instead of widening the thread.
struct ProseView: View {
    let text: String

    var body: some View {
        BlocksView(blocks: Markdown.parse(text))
    }
}

private struct BlocksView: View {
    let blocks: [MarkdownBlock]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                BlockContent(block: block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct BlockContent: View {
    let block: MarkdownBlock

    var body: some View {
        switch block {
        case .paragraph(let value):
            Text(inline(value))
                .font(.reply())
                .textSelection(.enabled)
        case .heading(let level, let value):
            Text(inline(value))
                .font(Self.headingFont(level))
                .padding(.top, level <= 2 ? 6 : 2)
                .textSelection(.enabled)
        case .list(_, let items):
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    ItemRow(item: item)
                }
            }
        case .quote(let inner):
            HStack(alignment: .top, spacing: 12) {
                Rectangle().fill(Theme.line).frame(width: 3)
                BlocksView(blocks: inner).foregroundStyle(.secondary)
            }
            .fixedSize(horizontal: false, vertical: true)
        case .code(let value, let language):
            CodeWell(code: value, language: language)
        case .table(let header, let align, let rows):
            TableView(header: header, align: align, rows: rows)
        case .rule:
            Rectangle().fill(Theme.line).frame(height: 1).padding(.vertical, 4)
        }
    }

    static func headingFont(_ level: Int) -> Font {
        switch level {
        case 1: return .title3.weight(.semibold)
        case 2: return .headline
        default: return .subheadline.weight(.semibold)
        }
    }
}

private struct ItemRow: View {
    let item: MarkdownItem

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Group {
                if let checked = item.checked {
                    Image(systemName: checked ? "checkmark.square" : "square")
                        .font(.silo(.callout))
                } else {
                    Text(item.marker).font(.reply()).monospacedDigit()
                }
            }
            .foregroundStyle(.secondary)
            .frame(minWidth: 14, alignment: .trailing)
            Text(inline(item.text))
                .font(.reply())
                .strikethrough(item.checked == true, color: .secondary)
                .textSelection(.enabled)
        }
        .padding(.leading, CGFloat(item.level) * 18)
    }
}

/// A GFM table: hairline rows, header in the quiet label style, wide tables scroll inside the thread.
private struct TableView: View {
    let header: [String]
    let align: [MarkdownAlign]
    let rows: [[String]]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 0) {
                GridRow {
                    ForEach(header.indices, id: \.self) { column in
                        cell(header[column], column).font(.silo(.footnote, .medium)).foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 6)
                Rectangle().fill(Theme.line).frame(height: 1).gridCellUnsizedAxes(.horizontal)
                ForEach(rows.indices, id: \.self) { index in
                    GridRow {
                        ForEach(header.indices, id: \.self) { column in
                            cell(column < rows[index].count ? rows[index][column] : "", column).font(.silo(.subheadline))
                        }
                    }
                    .padding(.vertical, 7)
                    if index < rows.count - 1 {
                        Rectangle().fill(Theme.line.opacity(0.6)).frame(height: 1).gridCellUnsizedAxes(.horizontal)
                    }
                }
            }
            .padding(.vertical, 2)
        }
        .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
    }

    private func cell(_ value: String, _ column: Int) -> some View {
        let alignment: MarkdownAlign = column < align.count ? align[column] : .leading
        return Text(inline(value))
            .multilineTextAlignment(alignment == .trailing ? .trailing : alignment == .center ? .center : .leading)
            .frame(maxWidth: 260, alignment: alignment == .trailing ? .trailing : alignment == .center ? .center : .leading)
            .gridColumnAlignment(alignment == .trailing ? .trailing : alignment == .center ? .center : .leading)
            .textSelection(.enabled)
    }
}

/// Inline Markdown (emphasis, code spans, links) for one block's text.
private func inline(_ value: String) -> AttributedString {
    let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
    return (try? AttributedString(markdown: value, options: options)) ?? AttributedString(value)
}
