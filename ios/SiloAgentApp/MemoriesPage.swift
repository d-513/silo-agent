import SiloClient
import SwiftUI

/// CORE MEMORY (always in the prompt, edited here) and the long-term memories the Bot saved.
struct MemoriesPage: View {
    @Environment(AppModel.self) private var model
    @State private var core = ""
    @State private var coreLoadedFor = ""
    @State private var memories: [Silo_V1_Memory] = []
    @State private var query = ""
    @State private var results: [Silo_V1_Memory]?
    @State private var saving = false
    @State private var deleting: Silo_V1_Memory?
    @State private var loaded = false

    private static let coreCap = 8000

    var body: some View {
        List {
            Section {
                TextEditor(text: $core)
                    .frame(minHeight: 140)
                    .font(.callout)
                HStack {
                    Text("\(core.count)/\(Self.coreCap)")
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(core.count > Self.coreCap ? Theme.vermilion : .secondary)
                    Spacer()
                    Button(saving ? "Saving…" : "Save") { Task { await saveCore() } }
                        .disabled(saving || core == (model.selectedBot?.memory ?? "") || core.count > Self.coreCap)
                }
            } header: {
                Text("Core memory")
            } footer: {
                Text("Always in the Bot's prompt. Over \(Self.coreCap) characters the Bot is asked to compact it.")
            }

            Section {
                ForEach(results ?? memories, id: \.id) { memory in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(memory.content).textSelection(.enabled)
                        HStack(spacing: 6) {
                            if memory.kind == "lesson" {
                                Text("lesson")
                                    .font(.caption2.weight(.semibold))
                                    .padding(.horizontal, 6).padding(.vertical, 1)
                                    .background(Theme.cobaltPale, in: RoundedRectangle(cornerRadius: 4))
                                    .foregroundStyle(Theme.cobalt)
                            }
                            Text(meta(memory)).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .swipeActions { Button(role: .destructive) { deleting = memory } label: { Label("Delete", systemImage: "trash") } }
                }
            } header: {
                Text(results == nil ? "Long-term memories" : "Matches")
            }
        }
        .searchable(text: $query, prompt: "Search memories")
        .onSubmit(of: .search) { Task { await search() } }
        .onChange(of: query) { _, value in if value.isEmpty { results = nil } }
        .overlay {
            if loaded && memories.isEmpty && results == nil {
                // The list still shows Core memory above; this only explains the long-term half.
                EmptyView()
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            core = model.selectedBot?.memory ?? ""
            await load()
        }
        .confirmationDialog("Delete this memory?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { memory in
            Button("Delete memory", role: .destructive) { Task { await delete(memory) } }
        }
    }

    private func meta(_ memory: Silo_V1_Memory) -> String {
        var parts: [String] = []
        if results != nil { parts.append("\(matchPercent(memory.distance))% match") }
        parts.append(shortDay(memory.createdAt))
        if !memory.chatID.isEmpty { parts.append("collected") }
        if !memory.lastUsedAt.isEmpty { parts.append("recalled \(shortDay(memory.lastUsedAt))") }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { memories = try await model.client.listMemories(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func search() async {
        guard let botID = model.selectedBotID else { return }
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else { results = nil; return }
        do { results = try await model.client.searchMemories(botID: botID, query: q) } catch { await model.report(error) }
    }

    private func saveCore() async {
        guard let bot = model.selectedBot else { return }
        saving = true
        defer { saving = false }
        do { model.applyBot(try await model.client.updateBot(bot, memory: core)) } catch { await model.report(error) }
    }

    private func delete(_ memory: Silo_V1_Memory) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteMemory(botID: botID, id: memory.id)
            memories.removeAll { $0.id == memory.id }
            results?.removeAll { $0.id == memory.id }
        } catch { await model.report(error) }
    }
}
