import SiloClient
import SwiftUI

/// Two sides per action, grouped as the CP sections (Bot, Secrets, each connector): Calls (may it
/// run: Allow / Auto / Ask / Deny) and Results (what happens to its result before the Bot reads
/// it: Pass / Check / Flag / Withhold). A rule the operator locked is a floor: only stricter modes
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
    /// Which sections are open; the Bot's starts open, and a filter opens them all.
    @State private var opened: [String: Bool] = ["bot": true]

    private static let callModes: [(String, String)] = [("allow", "Allow"), ("auto", "Auto"), ("ask", "Ask"), ("deny", "Deny")]
    private static let outputModes: [(String, String)] = [("pass", "Pass"), ("check", "Check"), ("flag", "Flag"), ("withhold", "Withhold")]

    private var modes: [(String, String)] { side == .output ? Self.outputModes : Self.callModes }
    private var total: Int { sided.reduce(0) { $0 + $1.rules.count } }

    var body: some View {
        PageScroll(spacing: 14) {
            Segmented(options: [(RuleSide.decision, "Calls"), (RuleSide.output, "Results")], selection: $side.animation(Motion.state))
            RuleLegend(side: side)
            if side == .output, list?.filterEnabled == false {
                MachineNote(text: "The operator has the output filter turned off. These rules are kept, but every result passes until it is on again.")
            }
            policySection
            if total > 5 {
                SiloField(prompt: "Filter actions", text: $query)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            if list == nil {
                SkeletonRows(count: 4, height: 52)
            } else if filtered.isEmpty {
                EmptyWell(symbol: "checklist", title: query.isEmpty ? "No rules" : "No matching rules found for “\(query)”")
            } else {
                ForEach(filtered, id: \.id) { section in
                    RuleSectionCard(section: section, side: side, modes: modes, open: !query.isEmpty || opened[section.id] == true) {
                        withAnimation(Motion.fold) { opened[section.id] = !(opened[section.id] ?? false) }
                    } setAll: { value in
                        Task { await setSection(section, value) }
                    } setRule: { rule, value in
                        Task { await set(rule, value) }
                    }
                }
            }
        }
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
            PanelSection(title: "Output policy", footer: "Read by the filter model for a result set to Check. It says what makes a result suspicious (flagged) or malicious (withheld). Empty means the built-in policy, which looks for attempts to direct the Bot.") {
                VStack(alignment: .leading, spacing: 10) {
                    MachineEditor(prompt: "Empty: the built-in policy. Write your own to change what is flagged or withheld.", text: $outputPolicy, mono: true)
                    HStack(spacing: 8) {
                        SaveButton(title: "Save policy", enabled: outputPolicy != savedOutputPolicy) { await saveOutputPolicy() }
                            .controlSize(.small)
                        if outputPolicy.trimmingCharacters(in: .whitespaces).isEmpty, let base = list?.defaultOutputPolicy, !base.isEmpty {
                            Button("Start from the built-in policy") { outputPolicy = base }
                                .buttonStyle(.quiet)
                                .controlSize(.small)
                        }
                    }
                }
                .padding(12)
            }
        } else {
            PanelSection(title: "Auto-approve policy", footer: "Read by the approval model when a rule is set to Auto, next to the action the Bot asks for and your last message in that conversation. Empty means every Auto rule asks.") {
                VStack(alignment: .leading, spacing: 10) {
                    MachineEditor(prompt: "e.g. Auto-approve read-only file reads, web searches, and screenshots. Ask before anything that writes, deletes, sends a message, or touches a secret.",
                                  text: $policy, mono: true)
                    SaveButton(title: "Save policy", enabled: policy != savedPolicy) { await savePolicy() }
                        .controlSize(.small)
                }
                .padding(12)
            }
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

    private func savePolicy() async -> Bool {
        guard let bot = model.selectedBot else { return false }
        do {
            model.applyBot(try await model.client.updateBot(bot, autoApprove: policy))
            savedPolicy = policy
            return true
        } catch {
            await model.report(error)
            return false
        }
    }

    private func saveOutputPolicy() async -> Bool {
        guard let bot = model.selectedBot else { return false }
        do {
            model.applyBot(try await model.client.setOutputPolicy(bot, policy: outputPolicy))
            savedOutputPolicy = outputPolicy
            return true
        } catch {
            await model.report(error)
            return false
        }
    }
}

/// What each of the four choices does, above the rules.
private struct RuleLegend: View {
    let side: RuleSide

    private var entries: [(dot: Color, title: String, text: String)] {
        side == .output
            ? [(Theme.lamp, "Pass", "The Bot reads the result as it is"),
               (Theme.ink2, "Check", "The filter model passes, flags or withholds it by your policy"),
               (Theme.line, "Flag", "Always handed over marked as untrusted, with no model asked"),
               (Theme.vermilion, "Withhold", "The Bot never reads the result")]
            : [(Theme.lamp, "Allow", "Runs immediately without asking"),
               (Theme.ink2, "Auto", "The approval model decides from your policy"),
               (Theme.line, "Ask", "Pauses the run and opens an approval slip"),
               (Theme.vermilion, "Deny", "Refuses execution automatically")]
    }

    var body: some View {
        LazyVGrid(columns: [GridItem(.flexible(), alignment: .topLeading), GridItem(.flexible(), alignment: .topLeading)], alignment: .leading, spacing: 10) {
            ForEach(entries, id: \.title) { entry in
                HStack(alignment: .top, spacing: 8) {
                    Circle().fill(entry.dot).frame(width: 8, height: 8).padding(.top, 5)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(entry.title).font(.silo(13, .medium)).foregroundStyle(Theme.ink)
                        Text(entry.text).font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
        }
        .padding(12)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}

/// One folio of rules (the Bot, its secrets, one connector): a head that folds, a switch for the
/// whole section, and a row per action.
private struct RuleSectionCard: View {
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
                                allowed: { mode in section.rules.allSatisfy { ruleModeAllowed(mode, floor: ruleFloor($0, side: side), side: side) } },
                                set: setAll)
                }
                .padding(.horizontal, 14)
                .padding(.bottom, 12)
            }
            if open {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(section.rules, id: \.key) { rule in
                        Theme.line.frame(height: 1)
                        RuleRow(rule: rule, side: side, modes: modes) { setRule(rule, $0) }
                    }
                }
                .transition(.opacity)
            }
        }
        .machineFolio()
    }
}

/// The least strict mode a rule may take on a Bot: the operator's locked value and the
/// Workspace's guardrail, whichever is stricter ("" for no floor). The Workspace's own Rules page
/// passes `workspace: false`, since there the Workspace's floor is the value being set.
func ruleFloor(_ rule: Silo_V1_Rule, side: RuleSide, workspace: Bool = true) -> String {
    let op = rule.locked ? (side == .output ? rule.operatorOutput : rule.operatorDecision) : ""
    let space = workspace ? (side == .output ? rule.spaceOutput : rule.spaceDecision) : ""
    if op.isEmpty { return space }
    if space.isEmpty { return op }
    return ruleStrictness(space, side: side) > ruleStrictness(op, side: side) ? space : op
}

/// The four-way switch of one rule or a whole section: the kit's `Segmented`, with the modes below
/// a floor off and only the lit word carrying a tone. No lit pill while a section's rules differ.
struct RuleSegment: View {
    let modes: [(String, String)]
    let value: String
    let side: RuleSide
    var small = false
    var allowed: (String) -> Bool = { _ in true }
    let set: (String) -> Void
    @Namespace private var pill

    var body: some View {
        HStack(spacing: 2) {
            ForEach(modes, id: \.0) { mode in
                let on = mode.0 == value
                let offered = allowed(mode.0)
                Button {
                    if !on { set(mode.0) }
                } label: {
                    Text(mode.1)
                        .font(.silo(small ? 12.5 : 13, .medium))
                        .foregroundStyle(on ? tone(mode.0) : Theme.ink3)
                        .opacity(offered ? 1 : 0.4)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .frame(maxWidth: .infinity)
                        .frame(height: small ? 28 : 32)
                        .background {
                            if on {
                                RoundedRectangle(cornerRadius: Theme.Radius.sm - 1, style: .continuous)
                                    .fill(Theme.surface)
                                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm - 1, style: .continuous).stroke(Theme.line))
                                    .matchedGeometryEffect(id: "pill", in: pill)
                            }
                        }
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(!offered)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(3)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        .animation(Motion.quiet(0.28), value: value)
        .sensoryFeedback(.selection, trigger: value)
    }

    private func tone(_ mode: String) -> Color {
        switch mode {
        case "allow", "pass": return Theme.emerald
        case "deny", "withhold": return Theme.vermilion
        case "auto", "check": return Theme.ink2
        default: return Theme.ink
        }
    }
}

/// One action and the switch for the side of its rule the page is showing; a floor turns off what
/// it does not allow.
struct RuleRow: View {
    let rule: Silo_V1_Rule
    let side: RuleSide
    let modes: [(String, String)]
    var workspace = true
    let set: (String) -> Void

    private var value: String { side == .output ? rule.output : rule.decision }
    private var floor: String { ruleFloor(rule, side: side, workspace: workspace) }
    /// The Workspace set this floor (it is stricter than the operator's).
    private var fromWorkspace: Bool {
        workspace && !floor.isEmpty && floor == (side == .output ? rule.spaceOutput : rule.spaceDecision)
            && floor != (rule.locked ? (side == .output ? rule.operatorOutput : rule.operatorDecision) : "")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(rule.title.isEmpty ? rule.action : rule.title)
                        .font(rule.connector == "secrets" ? .siloMono(13, .medium) : .silo(14, .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(2)
                    if rule.connector == "secrets" { Chip(text: "Secret", tint: Theme.ink3) }
                }
                if !rule.title.isEmpty, rule.title != rule.action, rule.action != "*", rule.connector != "secrets" {
                    Text(rule.action).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
                if !floor.isEmpty {
                    HStack(spacing: 4) {
                        Image(systemName: "lock.fill").font(.system(size: 9))
                        Text(fromWorkspace ? "Set by the workspace: \(label(floor))" : "Operator: \(label(floor))")
                            .font(.silo(11.5, .medium))
                    }
                    .foregroundStyle(Theme.ink2)
                    .padding(.horizontal, 7)
                    .frame(height: 20)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
                }
            }
            RuleSegment(modes: modes, value: value, side: side,
                        allowed: { ruleModeAllowed($0, floor: floor, side: side) }, set: set)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }

    private func label(_ mode: String) -> String { modes.first { $0.0 == mode }?.1 ?? mode.capitalized }
}

extension Silo_V1_Rule {
    /// Catalog-default rules have no row id, so the pair is the identity.
    var key: String { connector + "." + action }
}
