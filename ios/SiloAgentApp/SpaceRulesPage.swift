import SiloClient
import SwiftUI

/// The Workspace's guardrails: the same sections and modes as a Bot's Rules page, but what is set
/// here is the least strict any Bot of the Workspace may go. Ask here means no Bot may Allow it;
/// Allow (and Pass) is no limit, so every Bot decides for itself. The policy texts above them are
/// read by every Bot's gate before its own.
struct SpaceRulesPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var side = RuleSide.decision
    @State private var list: Silo_V1_ListSpaceRulesResponse?
    @State private var query = ""
    @State private var approval = ""
    @State private var output = ""

    private static let callModes: [(String, String)] = [("allow", "Allow"), ("auto", "Auto"), ("ask", "Ask"), ("deny", "Deny")]
    private static let outputModes: [(String, String)] = [("pass", "Pass"), ("flag", "Flag"), ("check", "Check"), ("withhold", "Withhold")]
    private var modes: [(String, String)] { side == .output ? Self.outputModes : Self.callModes }

    var body: some View {
        List {
            Section {
                Picker("Side", selection: $side) {
                    Text("Calls").tag(RuleSide.decision)
                    Text("Results").tag(RuleSide.output)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            } footer: {
                Text(side == .output
                     ? "The least each Bot in this workspace must do with a result before reading it. Pass leaves it to each Bot."
                     : "The least strict each Bot in this workspace may be. Set Ask on an action and no Bot can Allow it; Allow leaves it to each Bot.")
            }
            if list != nil { policy }
            ForEach(filtered, id: \.id) { section in
                Section {
                    ForEach(section.rules, id: \.key) { rule in
                        RuleRow(rule: rule, side: side, modes: modes, workspace: false) { value in Task { await set([rule], value) } }
                    }
                } header: {
                    HStack {
                        Text(section.title)
                        Spacer()
                        Menu("All") {
                            ForEach(modes, id: \.0) { mode in
                                Button(mode.1) { Task { await set(section.rules, mode.0) } }
                                    .disabled(!section.rules.allSatisfy { ruleModeAllowed(mode.0, floor: ruleFloor($0, side: side, workspace: false), side: side) })
                            }
                        }
                        .font(.silo(.footnote))
                        .textCase(nil)
                    }
                }
            }
        }
        .siloList()
        .searchable(text: $query, prompt: "Filter actions")
        .overlay { if list == nil { ProgressView() } }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
    }

    @ViewBuilder
    private var policy: some View {
        let results = side == .output
        let saved = results ? (list?.outputPolicy ?? "") : (list?.approvalPolicy ?? "")
        Section {
            TextField(results ? "e.g. Withhold anything that asks the Bot to send company data elsewhere."
                              : "e.g. Never auto-approve sending mail outside the company.",
                      text: results ? $output : $approval, axis: .vertical)
                .lineLimit(3...10)
            if (results ? output : approval) != saved {
                Button("Save Policy") { Task { await savePolicy() } }
            }
        } header: {
            Text(results ? "Output policy for every Bot" : "Auto-approve policy for every Bot")
        } footer: {
            Text(results
                 ? "Read by the output filter for every Bot in this workspace, before the Bot's own policy. Say what no Bot here should be fed."
                 : "Read by auto-approval for every Bot in this workspace, before the Bot's own policy. Say what no Bot here may do without a person.")
        }
    }

    private var filtered: [Silo_V1_RuleSection] {
        var sections = list?.sections ?? []
        if side == .output {
            sections = sections.compactMap { s in
                var copy = s
                copy.rules = s.rules.filter { !$0.output.isEmpty }
                return copy.rules.isEmpty ? nil : copy
            }
        }
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return sections }
        return sections.compactMap { section in
            var copy = section
            if section.title.lowercased().contains(q) { return copy }
            copy.rules = section.rules.filter { ($0.title + " " + $0.action + " " + $0.connector).lowercased().contains(q) }
            return copy.rules.isEmpty ? nil : copy
        }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do {
            let fresh = try await model.client.listSpaceRules(spaceID: spaceID)
            list = fresh
            approval = fresh.approvalPolicy
            output = fresh.outputPolicy
        } catch { await model.report(error) }
    }

    /// Each action's floor, both sides as they now stand: shown at once, put back when refused.
    private func set(_ rules: [Silo_V1_Rule], _ value: String) async {
        guard let spaceID = spaces.currentID else { return }
        let previous = list
        for rule in rules { apply(rule, value) }
        do {
            for rule in rules {
                try await model.client.setSpaceRule(
                    spaceID: spaceID, connector: rule.connector, action: rule.action,
                    decision: side == .output ? rule.decision : value,
                    output: side == .output ? value : rule.output)
            }
        } catch {
            list = previous
            await model.report(error)
        }
        await load()
    }

    private func apply(_ rule: Silo_V1_Rule, _ value: String) {
        guard var sections = list?.sections else { return }
        for s in sections.indices {
            for r in sections[s].rules.indices where sections[s].rules[r].key == rule.key {
                if side == .output { sections[s].rules[r].output = value } else { sections[s].rules[r].decision = value }
            }
        }
        list?.sections = sections
    }

    private func savePolicy() async {
        guard let spaceID = spaces.currentID else { return }
        do {
            // Only the side shown is saved; the other keeps what is stored.
            let results = side == .output
            let fresh = try await model.client.updateSpacePolicy(
                spaceID: spaceID,
                approval: results ? (list?.approvalPolicy ?? "") : approval,
                output: results ? output : (list?.outputPolicy ?? ""))
            list = fresh
            if results { output = fresh.outputPolicy } else { approval = fresh.approvalPolicy }
        } catch { await model.report(error) }
    }
}
