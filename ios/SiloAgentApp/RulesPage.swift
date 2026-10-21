import SiloClient
import SwiftUI

/// Two sides per action, grouped as the CP sections (Bot, Secrets, each connector): Calls (may it
/// run: Allow / Auto / Ask / Deny) and Results (what happens to its result before the Bot reads
/// it: Pass / Flag / Check / Withhold). A rule the operator locked is a floor: only stricter modes
/// are offered.
struct RulesPage: View {
    @Environment(AppModel.self) private var model
    @State private var side = RuleSide.decision
    @State private var list: Silo_V1_ListRulesResponse?
    @State private var query = ""
    @State private var policy = ""
    @State private var savedPolicy = ""
    @State private var outputPolicy = ""
    @State private var savedOutputPolicy = ""

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
                     ? "Pass hands a result over as it is, Flag frames it as untrusted, Check has the filter model judge it, Withhold keeps it from the Bot."
                     : "Allow runs the action, Auto asks the approval model, Ask asks you, Deny refuses it.")
            }
            if side == .output, list?.filterEnabled == false {
                Section {
                    Text("The operator has the output filter turned off. These rules are kept, but every result passes until it is on again.")
                        .foregroundStyle(.secondary)
                }
            }
            policySection
            ForEach(filtered, id: \.id) { section in
                Section {
                    ForEach(section.rules, id: \.key) { rule in
                        RuleRow(rule: rule, side: side, modes: modes) { value in Task { await set(rule, value) } }
                    }
                } header: {
                    HStack {
                        Text(section.title)
                        Spacer()
                        Menu("All") {
                            ForEach(modes, id: \.0) { mode in
                                Button(mode.1) { Task { await setSection(section, mode.0) } }
                                    .disabled(!section.rules.allSatisfy { ruleModeAllowed(mode.0, floor: floor($0), side: side) })
                            }
                        }
                        .font(.silo(.footnote))
                        .textCase(nil)
                    }
                } footer: {
                    let summary = side == .output ? section.outputSummary : section.summary
                    if !summary.isEmpty { Text(summary) }
                }
            }
        }
        .searchable(text: $query, prompt: "Filter actions")
        .overlay { if list != nil && filtered.isEmpty && query.isEmpty { ContentUnavailableView("No rules", systemImage: "checklist") } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            policy = model.selectedBot?.autoApprove ?? ""
            savedPolicy = policy
            outputPolicy = model.selectedBot?.outputPolicy ?? ""
            savedOutputPolicy = outputPolicy
            await load()
        }
    }

    @ViewBuilder
    private var policySection: some View {
        if side == .output {
            Section {
                TextField("Empty: the built-in policy", text: $outputPolicy, axis: .vertical).lineLimit(3...10)
                if outputPolicy.trimmingCharacters(in: .whitespaces).isEmpty, let base = list?.defaultOutputPolicy, !base.isEmpty {
                    Button("Start from the Built-in Policy") { outputPolicy = base }
                }
                if outputPolicy != savedOutputPolicy {
                    Button("Save Policy") { Task { await saveOutputPolicy() } }
                }
            } header: {
                Text("Output policy")
            } footer: {
                Text("Read by the filter model for a result set to Check. It says what makes a result suspicious (flagged) or malicious (withheld).")
            }
        } else {
            Section {
                TextField("e.g. Allow read-only actions; ask before anything that sends or deletes", text: $policy, axis: .vertical)
                    .lineLimit(3...8)
                if policy != savedPolicy {
                    Button("Save Policy") { Task { await savePolicy() } }
                }
            } header: {
                Text("Auto-approval policy")
            } footer: {
                Text("Actions set to Auto are judged by the approval model against this text.")
            }
        }
    }

    private func floor(_ rule: Silo_V1_Rule) -> String {
        guard rule.locked else { return "" }
        return side == .output ? rule.operatorOutput : rule.operatorDecision
    }

    /// The sections this side shows: only actions whose result the Bot reads have an output rule.
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
        guard let botID = model.selectedBotID else { return }
        do { list = try await model.client.listRules(botID: botID) } catch { await model.report(error) }
    }

    private func send(_ rule: Silo_V1_Rule, _ value: String) async throws {
        guard let botID = model.selectedBotID else { return }
        if side == .output {
            try await model.client.setRuleOutput(botID: botID, connector: rule.connector, action: rule.action, output: value)
        } else {
            try await model.client.setRule(botID: botID, connector: rule.connector, action: rule.action, decision: value)
        }
    }

    private func set(_ rule: Silo_V1_Rule, _ value: String) async {
        let previous = list
        apply(connector: rule.connector, action: rule.action, value)
        do { try await send(rule, value) } catch {
            list = previous
            await model.report(error)
        }
    }

    private func setSection(_ section: Silo_V1_RuleSection, _ value: String) async {
        let previous = list
        for rule in section.rules { apply(connector: rule.connector, action: rule.action, value) }
        do {
            for rule in section.rules { try await send(rule, value) }
        } catch {
            list = previous
            await model.report(error)
        }
    }

    private func apply(connector: String, action: String, _ value: String) {
        guard var sections = list?.sections else { return }
        for s in sections.indices {
            for r in sections[s].rules.indices where sections[s].rules[r].connector == connector && sections[s].rules[r].action == action {
                if side == .output {
                    if !sections[s].rules[r].output.isEmpty { sections[s].rules[r].output = value }
                } else {
                    sections[s].rules[r].decision = value
                }
            }
        }
        list?.sections = sections
    }

    private func savePolicy() async {
        guard let bot = model.selectedBot else { return }
        do {
            model.applyBot(try await model.client.updateBot(bot, autoApprove: policy))
            savedPolicy = policy
        } catch { await model.report(error) }
    }

    private func saveOutputPolicy() async {
        guard let bot = model.selectedBot else { return }
        do {
            model.applyBot(try await model.client.setOutputPolicy(bot, policy: outputPolicy))
            savedOutputPolicy = outputPolicy
        } catch { await model.report(error) }
    }
}

private struct RuleRow: View {
    let rule: Silo_V1_Rule
    let side: RuleSide
    let modes: [(String, String)]
    let set: (String) -> Void

    private var value: String { side == .output ? rule.output : rule.decision }
    private var floor: String {
        guard rule.locked else { return "" }
        return side == .output ? rule.operatorOutput : rule.operatorDecision
    }

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    if !floor.isEmpty {
                        Image(systemName: "lock.fill").font(.silo(.caption2)).foregroundStyle(.secondary)
                            .accessibilityLabel("Locked by the operator")
                    }
                    Text(rule.title.isEmpty ? rule.action : rule.title)
                        .font(rule.connector == "secrets" ? .system(.subheadline, design: .monospaced) : .subheadline)
                }
                if !rule.title.isEmpty, rule.title != rule.action, rule.action != "*", rule.connector != "secrets" {
                    Text(rule.action)
                        .font(.siloMono(.caption))
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            Menu {
                Picker("Mode", selection: Binding(get: { value }, set: set)) {
                    ForEach(modes, id: \.0) { mode in
                        Text(mode.1).tag(mode.0)
                            .selectionDisabled(!ruleModeAllowed(mode.0, floor: floor, side: side))
                    }
                }
                if !floor.isEmpty {
                    Text("The operator set at least \(label(floor)).")
                }
            } label: {
                HStack(spacing: 3) {
                    Text(label(value))
                    Image(systemName: "chevron.up.chevron.down").font(.silo(.caption2))
                }
                .font(.silo(.subheadline))
                .foregroundStyle(tone)
            }
        }
    }

    private func label(_ mode: String) -> String { modes.first { $0.0 == mode }?.1 ?? mode.capitalized }

    private var tone: Color {
        switch value {
        case "allow", "pass": return Theme.emerald
        case "deny", "withhold": return Theme.vermilion
        case "flag": return Theme.cobalt
        default: return .secondary
        }
    }
}

extension Silo_V1_Rule {
    /// Catalog-default rules have no row id, so the pair is the identity.
    var key: String { connector + "." + action }
}
