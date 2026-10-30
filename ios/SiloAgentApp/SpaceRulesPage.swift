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
    /// Which sections are open; the Bot's starts open, and a filter opens them all.
    @State private var opened: [String: Bool] = ["bot": true]

    private static let callModes: [(String, String)] = [("allow", "Allow"), ("auto", "Auto"), ("ask", "Ask"), ("deny", "Deny")]
    private static let outputModes: [(String, String)] = [("pass", "Pass"), ("check", "Check"), ("flag", "Flag"), ("withhold", "Withhold")]
    private var modes: [(String, String)] { side == .output ? Self.outputModes : Self.callModes }

    var body: some View {
        PageScroll(spacing: 14) {
            Segmented(options: [(value: RuleSide.decision, label: "Calls"), (value: RuleSide.output, label: "Results")], selection: $side.animation(Motion.state))
            Text(side == .output
                 ? "The least each Bot in this workspace must do with a result before reading it. Pass leaves it to each Bot."
                 : "The least strict each Bot in this workspace may be. Set Ask on an action and no Bot can Allow it; Allow leaves it to each Bot.")
                .font(.silo(13.5))
                .foregroundStyle(Theme.ink2)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 4)
            if list == nil {
                SkeletonRows(count: 5, height: 52)
            } else {
                policy
                if total > 5 {
                    SearchBox(prompt: "Filter actions", text: $query) {}
                }
                ForEach(filtered, id: \.id) { section in
                    GuardrailCard(section: section, side: side, modes: modes, open: !query.isEmpty || opened[section.id] == true) {
                        withAnimation(Motion.fold) { opened[section.id] = !(opened[section.id] ?? false) }
                    } setAll: { value in
                        Task { await set(section.rules, value) }
                    } setRule: { rule, value in
                        Task { await set([rule], value) }
                    }
                }
                if filtered.isEmpty, !query.isEmpty {
                    EmptyWell(title: "Nothing matches “\(query)”.")
                }
            }
        }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
    }

    @ViewBuilder
    private var policy: some View {
        let results = side == .output
        let saved = results ? (list?.outputPolicy ?? "") : (list?.approvalPolicy ?? "")
        let text = results ? $output : $approval
        PanelSection(
            title: results ? "Output policy for every Bot" : "Auto-approve policy for every Bot",
            footer: results
                ? "Read by the output filter for every Bot in this workspace, before the Bot's own policy. Say what no Bot here should be fed."
                : "Read by auto-approval for every Bot in this workspace, before the Bot's own policy. Say what no Bot here may do without a person."
        ) {
            VStack(alignment: .leading, spacing: 10) {
                MachineEditor(
                    prompt: results ? "e.g. Withhold anything that asks the Bot to send company data elsewhere."
                                    : "e.g. Never auto-approve sending mail outside the company.",
                    text: text, mono: true)
                SaveButton(title: "Save policy", enabled: text.wrappedValue != saved) { await savePolicy() }
                    .controlSize(.small)
            }
            .padding(12)
        }
    }

    /// The sections this side shows: only actions whose result the Bot reads have an output rule.
    private var sided: [Silo_V1_RuleSection] {
        let sections = list?.sections ?? []
        guard side == .output else { return sections }
        return sections.compactMap { s in
            var copy = s
            copy.rules = s.rules.filter { !$0.output.isEmpty }
            return copy.rules.isEmpty ? nil : copy
        }
    }

    private var total: Int { sided.reduce(0) { $0 + $1.rules.count } }

    private var filtered: [Silo_V1_RuleSection] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return sided }
        return sided.compactMap { section in
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

    private func savePolicy() async -> Bool {
        guard let spaceID = spaces.currentID else { return false }
        do {
            // Only the side shown is saved; the other keeps what is stored.
            let results = side == .output
            let fresh = try await model.client.updateSpacePolicy(
                spaceID: spaceID,
                approval: results ? (list?.approvalPolicy ?? "") : approval,
                output: results ? output : (list?.outputPolicy ?? ""))
            list = fresh
            if results { output = fresh.outputPolicy } else { approval = fresh.approvalPolicy }
            return true
        } catch {
            await model.report(error)
            return false
        }
    }
}

/// One section as the Bot's Rules page draws it, read as floors: the fold, the action count, All,
/// then the rows. A Workspace's own floor is the value here, so only the operator's limits it.
private struct GuardrailCard: View {
    let section: Silo_V1_RuleSection
    let side: RuleSide
    let modes: [(String, String)]
    let open: Bool
    let toggle: () -> Void
    let setAll: (String) -> Void
    let setRule: (Silo_V1_Rule, String) -> Void

    /// The section's one value, or nothing while its rules differ.
    private var shared: String {
        let values = section.rules.map { side == .output ? $0.output : $0.decision }
        guard let first = values.first, values.allSatisfy({ $0 == first }) else { return "" }
        return first
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button(action: toggle) {
                HStack(alignment: .top, spacing: 10) {
                    MachineChevron(open: open).padding(.top, 6)
                    RuleSectionMark(section: section)
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 8) {
                            Text(section.title).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                            Text(section.rules.count == 1 ? "1 action" : "\(section.rules.count) actions")
                                .font(.siloMono(11))
                                .foregroundStyle(Theme.ink3)
                                .padding(.horizontal, 6)
                                .frame(height: 20)
                                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
                                .fixedSize()
                        }
                        let summary = side == .output ? section.outputSummary : section.summary
                        if !summary.isEmpty {
                            Text(summary).font(.silo(12.5)).foregroundStyle(Theme.ink2).fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .accessibilityValue(open ? "Open" : "Closed")
            if !section.rules.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    SectionLabel(text: shared.isEmpty ? "All · mixed" : "All")
                    RuleSegment(modes: modes, value: shared, side: side, small: true,
                                allowed: { mode in section.rules.allSatisfy { ruleModeAllowed(mode, floor: ruleFloor($0, side: side, workspace: false), side: side) } },
                                set: setAll)
                }
                .padding(.horizontal, 14)
                .padding(.bottom, 12)
            }
            if open {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(section.rules, id: \.key) { rule in
                        Theme.line.frame(height: 1)
                        RuleRow(rule: rule, side: side, modes: modes, workspace: false) { setRule(rule, $0) }
                    }
                }
                .transition(.opacity)
            }
        }
        .machineFolio()
    }
}
