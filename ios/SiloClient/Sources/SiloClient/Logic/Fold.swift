import Foundation

/// Why a tool stopped without a clean result: the human denied it, or the run was
/// stopped/interrupted around it.
public enum ToolOutcome: String, Equatable, Sendable {
    case denied
    case stopped
}

/// A connector call made from Python inside an `exec_python` row.
public struct NestedCall: Identifiable, Equatable, Sendable {
    public var id: String
    public var title: String
    public var name: String
    public var result: String?
    public var running = false
    public var waiting = false
    public var outcome: ToolOutcome?
}

public struct ArtifactInfo: Equatable, Sendable {
    public var artifactType = "skill"
    public var name = ""
    public var title = ""
    public var path = ""
    public var scope = ""
    public var approvalID = ""
    public var status = ""
    public var size: Int64?
    /// The server's id for the card (empty on events from before versions) and the version shown.
    public var artifactID = ""
    public var version = 0
    /// `file` or `tree` (a folder: skills and apps).
    public var kind = ""
}

public struct AgentStatus: Equatable, Sendable {
    public var name: String
    public var status: String
}

/// A renderable unit of a conversation, folded from the raw `RunEvent` stream.
/// Port of `foldEvents` in `web/src/fold.ts`; keep the two in step.
public struct Block: Identifiable, Equatable, Sendable {
    public enum Kind: Sendable {
        case user, assistant, thinking, tool, receipt, error, report, quote, compaction, artifact
        /// A question the Bot asked, shown as its card and answered in place (`card`).
        case card
        /// What the Bot offered to set up in one turn, drawn together (`offers`).
        case offers
        /// A slash command's mark: the context started over (`cleared`) or `count` messages
        /// were deleted for good (`wiped`).
        case boundary
    }

    public var id: String
    public var kind: Kind
    public var text = ""
    public var name = ""
    public var args = ""
    public var result: String?
    public var running = false
    public var streaming = false
    public var runID = ""
    public var eventID = ""
    public var createdAt = ""
    // user
    public var attachments: [Silo_V1_Attachment] = []
    /// A non-human sender ("lead" for a subagent's brief, "onboarding" for the interview's opening).
    public var from = ""
    /// Who wrote it, on a Bot several people use (email).
    public var author = ""
    // tool
    public var calls: [NestedCall] = []
    public var waiting = false
    public var approvalID = ""
    public var outcome: ToolOutcome?
    // receipt: `decision` is allow_once / always / deny / stopped; `text` is the target
    public var decision = ""
    public var title = ""
    // report
    public var agents: [AgentStatus] = []
    // quote (source label) / compaction (auto or manual)
    public var source = ""
    public var artifact: ArtifactInfo?
    /// quote: what the chat opened from (`feed`, `widget`, `ask`, `asked`; empty on old quotes).
    public var quoted: Silo_V1_Quoted?
    /// card: the card as it stands; `asked` when it came from elsewhere into this chat.
    public var card: Silo_V1_DashboardCard?
    public var asked = false
    /// offers: the offers of one run, as they stand.
    public var offers: [Silo_V1_SetupOffer] = []
    /// boundary: `cleared` or `wiped`, and for a wipe how many messages went.
    public var count = 0
}

/// Parses a `subagent_report` label, "name:status,name2:status".
public func reportAgents(_ label: String) -> [AgentStatus] {
    label.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        .filter { !$0.isEmpty }
        .map { item in
            guard let colon = item.lastIndex(of: ":") else { return AgentStatus(name: item, status: "") }
            return AgentStatus(name: String(item[..<colon]), status: String(item[item.index(after: colon)...]))
        }
}

private func jsonObject(_ body: String) -> [String: Any] {
    guard let data = body.data(using: .utf8),
          let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
    return object
}

private func isPartialArgs(_ args: String) -> Bool {
    if args.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return true }
    guard let data = args.data(using: .utf8) else { return true }
    return (try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])) == nil
}

private let staleKey = try! NSRegularExpression(
    pattern: "openrouter api key|set openrouter|silo_openrouter|api key in admin",
    options: [.caseInsensitive]
)

/// `first` is the key index of `events[0]`: a page loaded in front of a thread starts below
/// zero, so the blocks already drawn keep their ids.
public func foldEvents(_ events: [Silo_V1_RunEvent], first: Int = 0) -> [Block] {
    var out: [Block] = []
    var counter = first

    func runOk(_ block: Block, _ runID: String) -> Bool {
        runID.isEmpty || block.runID.isEmpty || block.runID == runID
    }
    func lastIndex(where predicate: (Block) -> Bool) -> Int? {
        out.indices.reversed().first { predicate(out[$0]) }
    }
    func lastRunning(_ predicate: (Block) -> Bool) -> Int? {
        lastIndex { $0.kind == .tool && $0.running && predicate($0) }
    }
    func lastThinking() -> Int? { lastIndex { $0.kind == .thinking } }
    func closeThinking() {
        if let index = lastThinking() { out[index].streaming = false }
    }
    func runningCompaction(_ runID: String) -> Int? {
        lastIndex { $0.kind == .compaction && $0.running && runOk($0, runID) }
    }
    func waitingTool(_ runID: String, approvalID: String) -> Int? {
        lastIndex { block in
            guard block.kind == .tool, runOk(block, runID) else { return false }
            return approvalID.isEmpty ? block.waiting : block.approvalID == approvalID
        }
    }

    for event in events {
        if event.kind == "chat_title" || event.kind == "usage" { continue }
        if event.kind == "error",
           staleKey.firstMatch(in: event.body, range: NSRange(event.body.startIndex..., in: event.body)) != nil { continue }
        let key = "\(event.kind)-\(counter)"
        counter += 1
        func block(_ kind: Block.Kind) -> Block {
            var b = Block(id: key, kind: kind)
            b.runID = event.runID
            b.eventID = event.id
            b.createdAt = event.createdAt
            return b
        }

        switch event.kind {
        case "user":
            var b = block(.user)
            b.text = event.body
            b.attachments = event.attachments
            b.from = event.tool
            b.author = event.author
            out.append(b)

        case "subagent_report":
            var b = block(.report)
            b.text = event.body
            b.agents = reportAgents(event.tool)
            out.append(b)

        case "thinking_chunk":
            if let last = out.indices.last, out[last].kind == .thinking {
                out[last].text += event.body
                out[last].streaming = true
            } else {
                var b = block(.thinking)
                b.text = event.body
                b.streaming = true
                out.append(b)
            }

        case "thinking":
            if let index = lastThinking() {
                if !event.body.isEmpty { out[index].text = event.body }
                out[index].streaming = false
            } else {
                var b = block(.thinking)
                b.text = event.body
                out.append(b)
            }

        case "chunk":
            closeThinking()
            if let last = out.indices.last, out[last].kind == .assistant {
                out[last].text += event.body
                out[last].streaming = true
            } else {
                var b = block(.assistant)
                b.text = event.body
                b.streaming = true
                out.append(b)
            }

        case "compacting":
            closeThinking()
            var b = block(.compaction)
            b.source = event.tool
            b.running = true
            out.append(b)

        case "compaction":
            if let index = runningCompaction(event.runID) {
                out[index].text = event.body
                out[index].running = false
            } else {
                var b = block(.compaction)
                b.text = event.body
                b.source = event.tool
                out.append(b)
            }

        case "feed_quote":
            // A question the Bot asked from elsewhere is drawn as its card, live.
            if event.hasCard {
                var b = block(.card)
                b.card = event.card
                b.asked = true
                out.append(b)
            } else {
                var b = block(.quote)
                b.text = event.body
                b.source = event.tool
                if event.hasQuoted { b.quoted = event.quoted }
                out.append(b)
            }

        case "card":
            if event.hasCard {
                var b = block(.card)
                b.card = event.card
                out.append(b)
            }

        case "card_state":
            // A card in the chat changed (answered, put away): every block showing it follows.
            guard event.hasCard else { continue }
            for index in out.indices where out[index].kind == .card && out[index].card?.id == event.card.id {
                out[index].card = event.card
            }

        case "setup_offer":
            // Offers of one run join the group before them, past the offer_setup rows that made them.
            guard event.hasSetupOffer else { continue }
            var joined = false
            for index in out.indices.reversed() {
                if out[index].kind == .offers && out[index].runID == event.runID {
                    out[index].offers.append(event.setupOffer)
                    joined = true
                    break
                }
                if !(out[index].kind == .tool && out[index].name == "offer_setup") { break }
            }
            if !joined {
                var b = block(.offers)
                b.offers = [event.setupOffer]
                out.append(b)
            }

        case "setup_state":
            guard event.hasSetupOffer else { continue }
            for index in out.indices where out[index].kind == .offers {
                out[index].offers = out[index].offers.map { $0.id == event.setupOffer.id ? event.setupOffer : $0 }
            }

        case "context_cleared", "wiped":
            closeThinking()
            var b = block(.boundary)
            b.source = event.kind == "wiped" ? "wiped" : "cleared"
            b.count = Int(event.body) ?? 0
            out.append(b)

        case "assistant", "section", "section_live":
            closeThinking()
            if let last = out.indices.last, out[last].kind == .assistant, out[last].streaming {
                if !event.body.isEmpty { out[last].text = event.body }
                out[last].streaming = false
            } else if !event.body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                var b = block(.assistant)
                b.text = event.body
                out.append(b)
            }

        case "tool_args_chunk":
            closeThinking()
            let named = event.tool.isEmpty ? nil : lastRunning { $0.name == event.tool && runOk($0, event.runID) }
            if let index = named ?? lastRunning({ runOk($0, event.runID) }) {
                out[index].args += event.body
                if !event.tool.isEmpty { out[index].name = event.tool }
                if !event.runID.isEmpty { out[index].runID = event.runID }
            } else {
                var b = block(.tool)
                b.name = event.tool.isEmpty ? "tool" : event.tool
                b.args = event.body
                b.running = true
                out.append(b)
            }

        case "tool":
            closeThinking()
            let named = event.tool.isEmpty ? nil : lastRunning { $0.name == event.tool && runOk($0, event.runID) }
            if let index = named ?? lastRunning({ isPartialArgs($0.args) && runOk($0, event.runID) }) {
                if !event.body.isEmpty { out[index].args = event.body }
                if !event.tool.isEmpty { out[index].name = event.tool }
                if !event.runID.isEmpty { out[index].runID = event.runID }
            } else {
                var b = block(.tool)
                b.name = event.tool.isEmpty ? "tool" : event.tool
                b.args = event.body
                b.running = true
                out.append(b)
            }

        case "tool_chunk":
            closeThinking()
            if let index = lastIndex(where: { $0.kind == .tool && $0.running && (event.runID.isEmpty || $0.runID == event.runID) }) {
                out[index].result = (out[index].result ?? "") + event.body
            }

        case "tool_result":
            closeThinking()
            if let index = lastIndex(where: {
                $0.kind == .tool && $0.running && ($0.name == event.tool || event.tool.isEmpty)
                    && (event.runID.isEmpty || $0.runID == event.runID)
            }) {
                out[index].result = event.body
                out[index].running = false
            }

        case "call":
            closeThinking()
            let title = !event.body.isEmpty ? event.body : (!event.tool.isEmpty ? event.tool : "call")
            let item = NestedCall(id: key, title: title, name: event.tool, running: true)
            if let index = lastRunning({ runOk($0, event.runID) }) {
                out[index].calls.append(item)
            } else {
                var b = block(.tool)
                b.name = "call"
                b.running = true
                b.calls = [item]
                out.append(b)
            }

        case "call_result":
            closeThinking()
            for index in out.indices.reversed() {
                let b = out[index]
                guard b.kind == .tool, !(!event.runID.isEmpty && !b.runID.isEmpty && b.runID != event.runID),
                      !b.calls.isEmpty else { continue }
                if let call = out[index].calls.indices.reversed().first(where: {
                    out[index].calls[$0].running && (out[index].calls[$0].name == event.tool || event.tool.isEmpty)
                }) {
                    out[index].calls[call].result = event.body
                    out[index].calls[call].running = false
                }
                break
            }

        case "approval":
            // body = approval id, tool = connector.action. Pause the running row; a connector
            // call from Python pauses its nested call too.
            if let index = lastRunning({ runOk($0, event.runID) }) {
                out[index].waiting = true
                out[index].approvalID = event.body
                if let call = out[index].calls.lastIndex(where: { $0.running }) {
                    out[index].calls[call].waiting = true
                }
            }

        case "decision":
            let d = jsonObject(event.body)
            let decision = (d["decision"] as? String) ?? ""
            let approvalID = (d["approval_id"] as? String) ?? ""
            if let index = waitingTool(event.runID, approvalID: approvalID) ?? waitingTool(event.runID, approvalID: "") {
                out[index].waiting = false
                if let call = out[index].calls.lastIndex(where: { $0.waiting }) {
                    out[index].calls[call].waiting = false
                    if decision == "deny" { out[index].calls[call].outcome = .denied }
                } else if decision == "deny" {
                    out[index].outcome = .denied
                }
            }
            var b = block(.receipt)
            b.decision = decision
            b.title = (d["title"] as? String) ?? event.tool
            b.text = (d["target"] as? String) ?? ""
            out.append(b)

        case "done":
            // Anything still running in this run was cut off. A Stop leaves a receipt.
            let cut = ["stopped", "interrupted", "error"].contains(event.body)
            while let index = runningCompaction(event.runID) { out[index].running = false }
            for index in out.indices where out[index].kind == .tool && runOk(out[index], event.runID) {
                if out[index].running {
                    out[index].running = false
                    if cut { out[index].outcome = .stopped }
                }
                out[index].waiting = false
                for call in out[index].calls.indices {
                    if out[index].calls[call].running {
                        out[index].calls[call].running = false
                        if cut { out[index].calls[call].outcome = .stopped }
                    }
                    out[index].calls[call].waiting = false
                }
            }
            closeThinking()
            if let last = out.indices.last, out[last].kind == .assistant, out[last].streaming, !event.runID.isEmpty {
                out[last].streaming = false
            }
            if event.body == "stopped" {
                var b = block(.receipt)
                b.decision = "stopped"
                b.title = "this run"
                out.append(b)
            }

        case "artifact":
            closeThinking()
            let d = jsonObject(event.body)
            let name = (d["name"] as? String) ?? ""
            var info = ArtifactInfo()
            info.artifactType = (d["type"] as? String) ?? "skill"
            info.name = name
            info.title = (d["title"] as? String) ?? name
            info.path = (d["path"] as? String) ?? ""
            info.scope = (d["scope"] as? String) ?? ""
            info.approvalID = (d["approval_id"] as? String) ?? ""
            info.status = (d["status"] as? String) ?? ""
            info.size = (d["size"] as? NSNumber)?.int64Value
            info.artifactID = (d["artifact_id"] as? String) ?? ""
            info.version = (d["version"] as? NSNumber)?.intValue ?? 0
            info.kind = (d["kind"] as? String) ?? ""
            let same = out.indices.reversed().first { index in
                guard out[index].kind == .artifact, let old = out[index].artifact else { return false }
                return (!info.artifactID.isEmpty && old.artifactID == info.artifactID)
                    || (!info.approvalID.isEmpty && old.approvalID == info.approvalID)
                    || (!name.isEmpty && old.name == name && old.path == info.path)
            }
            if let same {
                out[same].artifact = info
                out[same].runID = event.runID
            } else {
                var b = block(.artifact)
                b.artifact = info
                out.append(b)
            }

        case "error":
            closeThinking()
            if let index = runningCompaction(event.runID) { out[index].running = false }
            var b = block(.error)
            b.text = event.body
            out.append(b)

        default:
            continue
        }
    }
    return out
}

/// Whether a conversation still has a run in flight: one that opened with a user message, a
/// manual compaction or a subagent report and has not reached done or error.
public func chatBusy(_ events: [Silo_V1_RunEvent]) -> Bool {
    var open = Set<String>()
    for event in events {
        guard !event.runID.isEmpty else { continue }
        if ["user", "compacting", "subagent_report"].contains(event.kind) { open.insert(event.runID) }
        if event.kind == "done" || event.kind == "error" { open.remove(event.runID) }
    }
    return !open.isEmpty
}
