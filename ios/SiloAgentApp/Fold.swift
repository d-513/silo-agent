import Foundation
import SiloClient

/// A renderable unit of a conversation, folded from the raw `RunEvent` stream.
struct Block: Identifiable {
    enum Kind {
        case user
        case assistant
        case thinking
        case tool
        case call
        case error
    }

    var id: String
    var kind: Kind
    var text: String = ""
    var name: String = ""
    var args: String = ""
    var result: String?
    var running: Bool = false
    var streaming: Bool = false
    var runID: String = ""
}

/// Port of the web client's `foldEvents`: collapses granular stream events into blocks.
func foldEvents(_ events: [Silo_V1_RunEvent]) -> [Block] {
    var out: [Block] = []
    var counter = 0
    func nextKey() -> String {
        counter += 1
        return "b\(counter)"
    }

    func runOk(_ block: Block, _ runID: String) -> Bool {
        runID.isEmpty || block.runID.isEmpty || block.runID == runID
    }

    func lastIndex(where predicate: (Block) -> Bool) -> Int? {
        guard !out.isEmpty else { return nil }
        for index in stride(from: out.count - 1, through: 0, by: -1) where predicate(out[index]) {
            return index
        }
        return nil
    }

    func lastThinking() -> Int? { lastIndex { $0.kind == .thinking } }

    func closeThinking() {
        if let index = lastThinking() { out[index].streaming = false }
    }

    func lastRunningTool(name: String, runID: String) -> Int? {
        lastIndex { block in
            guard block.kind == .tool, block.running, runOk(block, runID) else { return false }
            return name.isEmpty || block.name == name
        }
    }

    for event in events {
        switch event.kind {
        case "done", "chat_title", "usage":
            continue

        case "user":
            out.append(Block(id: nextKey(), kind: .user, text: event.body, runID: event.runID))

        case "thinking_chunk":
            if let index = lastThinking() {
                out[index].text += event.body
                out[index].streaming = true
            } else {
                out.append(Block(id: nextKey(), kind: .thinking, text: event.body, streaming: true, runID: event.runID))
            }

        case "thinking":
            if let index = lastThinking() {
                if !event.body.isEmpty { out[index].text = event.body }
                out[index].streaming = false
            } else {
                out.append(Block(id: nextKey(), kind: .thinking, text: event.body, runID: event.runID))
            }

        case "chunk":
            closeThinking()
            if let index = out.indices.last, out[index].kind == .assistant {
                out[index].text += event.body
                out[index].streaming = true
            } else {
                out.append(Block(id: nextKey(), kind: .assistant, text: event.body, streaming: true, runID: event.runID))
            }

        case "assistant", "section", "section_live":
            closeThinking()
            if let index = out.indices.last, out[index].kind == .assistant, out[index].streaming {
                if !event.body.isEmpty { out[index].text = event.body }
                out[index].streaming = false
            } else if !event.body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                out.append(Block(id: nextKey(), kind: .assistant, text: event.body, runID: event.runID))
            }

        case "tool_args_chunk":
            closeThinking()
            if let index = lastRunningTool(name: event.tool, runID: event.runID) {
                out[index].args += event.body
                if !event.tool.isEmpty { out[index].name = event.tool }
            } else {
                out.append(Block(
                    id: nextKey(),
                    kind: .tool,
                    name: event.tool.isEmpty ? "tool" : event.tool,
                    args: event.body,
                    running: true,
                    runID: event.runID
                ))
            }

        case "tool":
            closeThinking()
            if let index = lastRunningTool(name: event.tool, runID: event.runID) {
                if !event.body.isEmpty { out[index].args = event.body }
                if !event.tool.isEmpty { out[index].name = event.tool }
            } else {
                out.append(Block(
                    id: nextKey(),
                    kind: .tool,
                    name: event.tool.isEmpty ? "tool" : event.tool,
                    args: event.body,
                    running: true,
                    runID: event.runID
                ))
            }

        case "tool_chunk":
            closeThinking()
            if let index = lastIndex(where: { $0.kind == .tool && $0.running && runOk($0, event.runID) }) {
                out[index].result = (out[index].result ?? "") + event.body
            }

        case "tool_result":
            closeThinking()
            if let index = lastIndex(where: {
                $0.kind == .tool && $0.running && runOk($0, event.runID)
                    && (event.tool.isEmpty || $0.name == event.tool)
            }) {
                out[index].result = event.body
                out[index].running = false
            }

        case "call":
            closeThinking()
            out.append(Block(
                id: nextKey(),
                kind: .call,
                text: event.body,
                name: event.tool,
                running: true,
                runID: event.runID
            ))

        case "call_result":
            closeThinking()
            if let index = lastIndex(where: {
                $0.kind == .call && $0.running && runOk($0, event.runID)
                    && (event.tool.isEmpty || $0.name == event.tool)
            }) {
                out[index].result = event.body
                out[index].running = false
            }

        case "error":
            closeThinking()
            out.append(Block(id: nextKey(), kind: .error, text: event.body, runID: event.runID))

        default:
            continue
        }
    }
    return out
}

/// Whether a conversation still has a run in flight.
func chatBusy(_ events: [Silo_V1_RunEvent]) -> Bool {
    var open = Set<String>()
    for event in events {
        guard !event.runID.isEmpty else { continue }
        if event.kind == "user" { open.insert(event.runID) }
        if event.kind == "done" || event.kind == "error" { open.remove(event.runID) }
    }
    return !open.isEmpty
}

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
