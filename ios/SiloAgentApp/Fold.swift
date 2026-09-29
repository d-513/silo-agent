import Foundation
import SiloClient

/// Minimal, dependency-free Markdown: fenced code blocks are split out so they can
/// render in a monospaced well; everything else goes through `AttributedString`.
enum Prose: Identifiable {
    case text(String)
    case code(String, String?)

    var id: String {
        switch self {
        case .text(let value): return "t" + value
        case .code(let value, let language): return "c" + (language ?? "") + value
        }
    }

    static func parse(_ markdown: String) -> [Prose] {
        var parts: [Prose] = []
        var prose: [String] = []
        var code: [String] = []
        var language: String?
        var inCode = false

        func flushProse() {
            let joined = prose.joined(separator: "\n")
            if !joined.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                parts.append(.text(joined))
            }
            prose.removeAll()
        }

        for line in markdown.components(separatedBy: "\n") {
            if line.hasPrefix("```") {
                if inCode {
                    parts.append(.code(code.joined(separator: "\n"), language))
                    code.removeAll()
                    language = nil
                    inCode = false
                } else {
                    flushProse()
                    let tag = String(line.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                    language = tag.isEmpty ? nil : tag
                    inCode = true
                }
                continue
            }
            if inCode { code.append(line) } else { prose.append(line) }
        }
        if inCode, !code.isEmpty {
            parts.append(.code(code.joined(separator: "\n"), language))
        } else {
            flushProse()
        }
        return parts
    }
}
