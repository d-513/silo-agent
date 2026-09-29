import SiloClient
import SwiftUI

/// Allow / Auto / Ask / Deny per action, grouped as the CP sections (Bot, Secrets, each connector).
struct RulesPage: View {
    @Environment(AppModel.self) private var model
    @State private var sections: [Silo_V1_RuleSection] = []
    @State private var loaded = false
    @State private var query = ""
    @State private var policy = ""
    @State private var savedPolicy = ""

    private static let modes: [(String, String)] = [("allow", "Allow"), ("auto", "Auto"), ("ask", "Ask"), ("deny", "Deny")]

    var body: some View {
        List {
            Section {
                TextEditor(text: $policy).frame(minHeight: 80).font(.callout)
                if policy != savedPolicy {
                    Button("Save policy") { Task { await savePolicy() } }
                }
            } header: {
                Text("Auto-approval policy")
            } footer: {
                Text("Actions set to Auto are judged by the approval model against this text.")
            }
            ForEach(filtered, id: \.id) { section in
                Section {
                    ForEach(section.rules, id: \.id) { rule in
                        VStack(alignment: .leading, spacing: 6) {
                            Text(rule.title.isEmpty ? rule.action : rule.title).font(.subheadline)
                            decisionPicker(rule.decision) { value in Task { await set(rule, value) } }
                        }
                        .padding(.vertical, 2)
                    }
                } header: {
                    HStack {
                        Text(section.title)
                        Spacer()
                        Menu("All") {
                            ForEach(Self.modes, id: \.0) { mode in
                                Button(mode.1) { Task { await setSection(section, mode.0) } }
                            }
                        }
                        .font(.footnote)
                        .textCase(nil)
                    }
                } footer: {
                    if !section.summary.isEmpty { Text(section.summary) }
                }
            }
        }
        .searchable(text: $query, prompt: "Filter actions")
        .overlay { if loaded && sections.isEmpty { ContentUnavailableView("No rules", systemImage: "checklist") } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            policy = model.selectedBot?.autoApprove ?? ""
            savedPolicy = policy
            await load()
        }
    }

    private func decisionPicker(_ value: String, onChange: @escaping (String) -> Void) -> some View {
        Picker("Decision", selection: Binding(get: { value }, set: onChange)) {
            ForEach(Self.modes, id: \.0) { Text($0.1).tag($0.0) }
        }
        .pickerStyle(.segmented)
        .labelsHidden()
    }

    private var filtered: [Silo_V1_RuleSection] {
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
        do { sections = try await model.client.listRuleSections(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func set(_ rule: Silo_V1_Rule, _ decision: String) async {
        guard let botID = model.selectedBotID else { return }
        let previous = sections
        apply(connector: rule.connector, action: rule.action, decision)
        do { try await model.client.setRule(botID: botID, connector: rule.connector, action: rule.action, decision: decision) } catch {
            sections = previous
            await model.report(error)
        }
    }

    private func setSection(_ section: Silo_V1_RuleSection, _ decision: String) async {
        guard let botID = model.selectedBotID else { return }
        let previous = sections
        for rule in section.rules { apply(connector: rule.connector, action: rule.action, decision) }
        do {
            for rule in section.rules {
                try await model.client.setRule(botID: botID, connector: rule.connector, action: rule.action, decision: decision)
            }
        } catch {
            sections = previous
            await model.report(error)
        }
    }

    private func apply(connector: String, action: String, _ decision: String) {
        for s in sections.indices {
            for r in sections[s].rules.indices where sections[s].rules[r].connector == connector && sections[s].rules[r].action == action {
                sections[s].rules[r].decision = decision
            }
        }
    }

    private func savePolicy() async {
        guard let bot = model.selectedBot else { return }
        do {
            model.applyBot(try await model.client.updateBot(bot, autoApprove: policy))
            savedPolicy = policy
        } catch { await model.report(error) }
    }
}
