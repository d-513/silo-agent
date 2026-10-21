import Foundation

/// The block structure of a Markdown text (CommonMark + GFM tables and task items), so the app can
/// lay out paragraphs, headings, lists, quotes, code and tables itself. Inline syntax (emphasis,
/// code spans, links) is left in each block's text for `AttributedString` to read: SwiftUI's
/// `Text` renders inline Markdown only, and flattens everything else into one run.
public enum MarkdownBlock: Equatable, Sendable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case list(ordered: Bool, items: [MarkdownItem])
    case quote([MarkdownBlock])
    case code(text: String, language: String?)
    case table(header: [String], align: [MarkdownAlign], rows: [[String]])
    case rule
}

/// One list line. Nested lists are flattened: `level` is how deep the item sits.
public struct MarkdownItem: Equatable, Sendable {
    public var level: Int
    /// "•" for a bullet, "3." for an ordered item.
    public var marker: String
    public var text: String
    /// A GFM task item's box; nil for a plain item.
    public var checked: Bool?

    public init(level: Int, marker: String, text: String, checked: Bool? = nil) {
        self.level = level
        self.marker = marker
        self.text = text
        self.checked = checked
    }
}

public enum MarkdownAlign: Equatable, Sendable { case leading, center, trailing }

public enum Markdown {
    public static func parse(_ source: String) -> [MarkdownBlock] {
        var parser = Parser(lines: source.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n"))
        return parser.blocks()
    }

    private struct Parser {
        let lines: [String]
        var at = 0
        var out: [MarkdownBlock] = []
        var para: [String] = []

        init(lines: [String]) { self.lines = lines }

        mutating func blocks() -> [MarkdownBlock] {
            while at < lines.count {
                let line = lines[at]
                if isBlank(line) {
                    flush()
                    at += 1
                } else if let fence = fence(line) {
                    flush()
                    code(fence)
                } else if let level = setext(line), !para.isEmpty {
                    out.append(.heading(level: level, text: joined(para)))
                    para = []
                    at += 1
                } else if isRule(line) {
                    flush()
                    out.append(.rule)
                    at += 1
                } else if let (level, text) = heading(line) {
                    flush()
                    out.append(.heading(level: level, text: text))
                    at += 1
                } else if quoteBody(line) != nil {
                    flush()
                    quote()
                } else if let item = listItem(line), para.isEmpty || !item.ordered || item.marker == "1." {
                    // Only a bullet or a list starting at 1 may interrupt a paragraph, so
                    // "2024. was a good year" stays prose.
                    flush()
                    list()
                } else if at + 1 < lines.count, let align = delimiter(lines[at + 1]),
                          cells(line).count == align.count {
                    flush()
                    table(align)
                } else {
                    para.append(line)
                    at += 1
                }
            }
            flush()
            return out
        }

        mutating func flush() {
            if !para.isEmpty { out.append(.paragraph(joined(para))) }
            para = []
        }

        // MARK: code

        mutating func code(_ fence: (marker: String, language: String?)) {
            at += 1
            var body: [String] = []
            while at < lines.count {
                let trimmed = lines[at].trimmingCharacters(in: .whitespaces)
                if trimmed.hasPrefix(fence.marker), trimmed.allSatisfy({ $0 == fence.marker.first }) {
                    at += 1
                    break
                }
                body.append(lines[at])
                at += 1
            }
            out.append(.code(text: body.joined(separator: "\n"), language: fence.language))
        }

        // MARK: quote

        mutating func quote() {
            var inner: [String] = []
            while at < lines.count {
                if let body = quoteBody(lines[at]) {
                    inner.append(body)
                } else if !isBlank(lines[at]), !inner.isEmpty, !isBlank(inner.last!),
                          fence(lines[at]) == nil, listItem(lines[at]) == nil, heading(lines[at]) == nil {
                    inner.append(lines[at]) // a lazy continuation of the quoted paragraph
                } else {
                    break
                }
                at += 1
            }
            var nested = Parser(lines: inner)
            out.append(.quote(nested.blocks()))
        }

        // MARK: lists

        mutating func list() {
            guard let first = listItem(lines[at]) else { return }
            let ordered = first.ordered
            var items: [MarkdownItem] = []
            var indents: [Int] = []
            while at < lines.count {
                let line = lines[at]
                if let item = listItem(line) {
                    // A different kind of marker at the top level starts a new list.
                    if item.indent <= (indents.first ?? 0), item.ordered != ordered, !items.isEmpty { break }
                    while let last = indents.last, item.indent < last { indents.removeLast() }
                    if indents.last.map({ item.indent > $0 }) ?? true { indents.append(item.indent) }
                    let level = max(0, indents.count - 1)
                    var text = item.text
                    var checked: Bool?
                    if let task = task(text) { (checked, text) = task }
                    items.append(MarkdownItem(level: level, marker: item.marker, text: text, checked: checked))
                    at += 1
                } else if isBlank(line) {
                    // A blank line keeps the list going only if the list goes on after it.
                    var next = at + 1
                    while next < lines.count, isBlank(lines[next]) { next += 1 }
                    guard next < lines.count, listItem(lines[next]) != nil || indent(lines[next]) >= 2 else { break }
                    at = next
                } else if indent(line) >= 2 || (!items.isEmpty && !startsBlock(line)) {
                    // A continuation of the last item, indented or lazy.
                    let trimmed = line.trimmingCharacters(in: .whitespaces)
                    if !items.isEmpty { items[items.count - 1].text += " " + trimmed }
                    at += 1
                } else {
                    break
                }
            }
            out.append(.list(ordered: ordered, items: items))
        }

        // MARK: tables

        mutating func table(_ align: [MarkdownAlign]) {
            let header = cells(lines[at])
            at += 2
            var rows: [[String]] = []
            while at < lines.count, !isBlank(lines[at]), lines[at].contains("|") {
                var row = cells(lines[at])
                if row.count < align.count { row += Array(repeating: "", count: align.count - row.count) }
                rows.append(Array(row.prefix(align.count)))
                at += 1
            }
            out.append(.table(header: header, align: align, rows: rows))
        }

        // MARK: line tests

        func startsBlock(_ line: String) -> Bool {
            fence(line) != nil || heading(line) != nil || quoteBody(line) != nil || isRule(line)
        }

        func joined(_ lines: [String]) -> String {
            // A soft break is a space; two trailing spaces or a backslash is a hard break.
            var text = ""
            for (index, raw) in lines.enumerated() {
                var line = raw.trimmingCharacters(in: .whitespaces)
                let last = index == lines.count - 1
                var hard = false
                if raw.hasSuffix("  ") { hard = true }
                if line.hasSuffix("\\") { line.removeLast(); hard = true }
                text += line
                if !last { text += hard ? "\n" : " " }
            }
            return text
        }
    }

    // MARK: line shapes

    static func isBlank(_ line: String) -> Bool { line.trimmingCharacters(in: .whitespaces).isEmpty }

    static func indent(_ line: String) -> Int {
        var count = 0
        for char in line {
            if char == " " { count += 1 } else if char == "\t" { count += 4 } else { break }
        }
        return count
    }

    static func fence(_ line: String) -> (marker: String, language: String?)? {
        guard indent(line) < 4 else { return nil }
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        for char in ["`", "~"] {
            let run = trimmed.prefix(while: { String($0) == char })
            if run.count >= 3 {
                let tag = trimmed.dropFirst(run.count).trimmingCharacters(in: .whitespaces)
                if char == "`", tag.contains("`") { return nil }
                return (String(run), tag.isEmpty ? nil : String(tag.split(separator: " ").first ?? ""))
            }
        }
        return nil
    }

    static func heading(_ line: String) -> (Int, String)? {
        guard indent(line) < 4 else { return nil }
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        let hashes = trimmed.prefix(while: { $0 == "#" }).count
        guard (1...6).contains(hashes) else { return nil }
        let rest = trimmed.dropFirst(hashes)
        guard rest.isEmpty || rest.first == " " else { return nil }
        var text = rest.trimmingCharacters(in: .whitespaces)
        // A closing run of #s is decoration.
        if let range = text.range(of: #"\s+#+$"#, options: .regularExpression) { text.removeSubrange(range) }
        if text.allSatisfy({ $0 == "#" }) { text = "" }
        return (hashes, text)
    }

    static func setext(_ line: String) -> Int? {
        guard indent(line) < 4 else { return nil }
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return nil }
        if trimmed.allSatisfy({ $0 == "=" }) { return 1 }
        if trimmed.allSatisfy({ $0 == "-" }) { return 2 }
        return nil
    }

    static func isRule(_ line: String) -> Bool {
        guard indent(line) < 4 else { return false }
        let chars = line.filter { $0 != " " && $0 != "\t" }
        guard chars.count >= 3, let first = chars.first, "-*_".contains(first) else { return false }
        return chars.allSatisfy { $0 == first }
    }

    static func quoteBody(_ line: String) -> String? {
        guard indent(line) < 4 else { return nil }
        let trimmed = line.drop(while: { $0 == " " })
        guard trimmed.first == ">" else { return nil }
        var body = trimmed.dropFirst()
        if body.first == " " { body = body.dropFirst() }
        return String(body)
    }

    static func listItem(_ line: String) -> (indent: Int, ordered: Bool, marker: String, text: String)? {
        let pad = indent(line)
        let trimmed = line.drop(while: { $0 == " " || $0 == "\t" })
        if let first = trimmed.first, "-*+".contains(first) {
            let rest = trimmed.dropFirst()
            guard rest.isEmpty || rest.first == " " || rest.first == "\t" else { return nil }
            if isRule(line) { return nil }
            return (pad, false, "•", rest.trimmingCharacters(in: .whitespaces))
        }
        let digits = trimmed.prefix(while: { $0.isASCII && $0.isNumber })
        guard (1...9).contains(digits.count) else { return nil }
        let rest = trimmed.dropFirst(digits.count)
        guard let delimiter = rest.first, delimiter == "." || delimiter == ")" else { return nil }
        let after = rest.dropFirst()
        guard after.isEmpty || after.first == " " || after.first == "\t" else { return nil }
        return (pad, true, "\(Int(digits) ?? 1).", after.trimmingCharacters(in: .whitespaces))
    }

    static func task(_ text: String) -> (Bool, String)? {
        let lower = text.lowercased()
        for (prefix, checked) in [("[ ] ", false), ("[x] ", true)] where lower.hasPrefix(prefix) {
            return (checked, String(text.dropFirst(prefix.count)))
        }
        return nil
    }

    static func cells(_ line: String) -> [String] {
        var trimmed = line.trimmingCharacters(in: .whitespaces)
        if trimmed.hasPrefix("|") { trimmed.removeFirst() }
        if trimmed.hasSuffix("|") && !trimmed.hasSuffix("\\|") { trimmed.removeLast() }
        var cells: [String] = []
        var cell = ""
        var escaped = false
        var inCode = false
        for char in trimmed {
            if escaped {
                cell.append(char == "|" ? "|" : "\\\(char)")
                escaped = false
            } else if char == "\\" {
                escaped = true
            } else if char == "`" {
                inCode.toggle()
                cell.append(char)
            } else if char == "|" && !inCode {
                cells.append(cell.trimmingCharacters(in: .whitespaces))
                cell = ""
            } else {
                cell.append(char)
            }
        }
        cells.append(cell.trimmingCharacters(in: .whitespaces))
        return cells
    }

    static func delimiter(_ line: String) -> [MarkdownAlign]? {
        guard line.contains("-") else { return nil }
        let parts = cells(line)
        var align: [MarkdownAlign] = []
        for part in parts {
            let left = part.hasPrefix(":")
            let right = part.hasSuffix(":")
            let dashes = part.trimmingCharacters(in: CharacterSet(charactersIn: ":"))
            guard !dashes.isEmpty, dashes.allSatisfy({ $0 == "-" }) else { return nil }
            align.append(left && right ? .center : right ? .trailing : .leading)
        }
        // A one-column table needs a pipe somewhere, or `---` under a line would be one.
        if align.count == 1 && !line.contains("|") { return nil }
        return align
    }
}
