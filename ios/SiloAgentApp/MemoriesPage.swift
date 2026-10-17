import SiloClient
import SwiftUI

/// Memories the Bot saved.
struct MemoriesPage: View {
    @Environment(AppModel.self) private var model
    @State private var memories: [Silo_V1_Memory] = []
    @State private var query = ""
    @State private var results: [Silo_V1_Memory]?
    @State private var deleting: Silo_V1_Memory?
    @State private var loaded = false

    var body: some View {
        List {
            Section {
                ForEach(results ?? memories, id: \.id) { memory in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(memory.content).textSelection(.enabled)
                        Text(meta(memory)).font(.caption).foregroundStyle(.secondary)
                    }
                    .swipeActions { Button(role: .destructive) { deleting = memory } label: { Label("Delete", systemImage: "trash") } }
                }
            } header: {
                Text(results == nil ? "Memories" : "Matches")
            }
        }
        .searchable(text: $query, prompt: "Search memories")
        .onSubmit(of: .search) { Task { await search() } }
        .onChange(of: query) { _, value in if value.isEmpty { results = nil } }
        .overlay {
            if loaded && memories.isEmpty && results == nil {
                EmptyView()
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
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

    private func delete(_ memory: Silo_V1_Memory) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteMemory(botID: botID, id: memory.id)
            memories.removeAll { $0.id == memory.id }
            results?.removeAll { $0.id == memory.id }
        } catch { await model.report(error) }
    }
}
