import SiloClient
import SwiftUI

/// Memories the Bot saved, grouped by when, or ranked by meaning for a search. The Bot writes them;
/// the human reads, searches and deletes.
struct MemoriesPage: View {
    @Environment(AppModel.self) private var model
    @State private var memories: [Silo_V1_Memory] = []
    @State private var query = ""
    @State private var results: [Silo_V1_Memory]?
    @State private var searching = false
    @State private var loaded = false

    var body: some View {
        PageScroll(spacing: 18) {
            if !loaded || !memories.isEmpty || results != nil {
                SearchBox(prompt: "Search by meaning", text: $query, searching: searching) { Task { await search() } }
            }
            if !loaded {
                SkeletonRows(count: 4, height: 64)
            } else if let results {
                if results.isEmpty {
                    EmptyWell(title: "Nothing matches.") {
                        Button("Clear search") { query = "" }.buttonStyle(.ghost(tint: Theme.cobalt)).controlSize(.small)
                    }
                } else {
                    Panel { rows(results, scored: true) }
                }
            } else if memories.isEmpty {
                EmptyWell(symbol: "brain", title: "Nothing yet.",
                          message: "Saved by the Bot with remember, or collected from chats once they go quiet, and found by meaning.")
            } else {
                ForEach(chatGroups(memories, updatedAt: { parseDate($0.createdAt) })) { group in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(spacing: 8) {
                            SectionLabel(text: group.title)
                            Text("\(group.items.count)").font(.siloMono(11)).foregroundStyle(Theme.ink3)
                        }
                        .padding(.horizontal, 4)
                        Panel { rows(group.items, scored: false) }
                    }
                }
            }
        }
        .pageActions {
            if !memories.isEmpty { Chip(text: "\(memories.count)").padding(.trailing, 10) }
        }
        .onChange(of: query) { _, value in if value.isEmpty { results = nil } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            await load()
        }
    }

    private func rows(_ list: [Silo_V1_Memory], scored: Bool) -> some View {
        ForEach(list, id: \.id) { memory in
            HStack(alignment: .top, spacing: 8) {
                VStack(alignment: .leading, spacing: 5) {
                    if scored {
                        Chip(text: "\(matchPercent(memory.distance))% match", tint: Theme.cobalt, fill: Theme.cobaltPale)
                    }
                    Text(memory.content)
                        .font(.silo(14))
                        .foregroundStyle(Theme.ink)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                    meta(memory)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                ArmedGlyph(symbol: "trash", label: "Delete memory") { Task { await delete(memory) } }
            }
            .padding(.leading, 14)
            .padding(.trailing, 6)
            .padding(.vertical, 10)
        }
    }

    private func meta(_ memory: Silo_V1_Memory) -> some View {
        HStack(spacing: 6) {
            Text(shortDay(memory.createdAt))
            if !memory.chatID.isEmpty {
                Text("·").accessibilityHidden(true)
                Button { openChat(memory.chatID) } label: {
                    HStack(spacing: 3) {
                        Image(systemName: "bubble.left").font(.system(size: 10))
                        Text("collected")
                    }
                }
                .buttonStyle(.press)
                .accessibilityLabel("Open the chat it came from")
            }
            if !memory.lastUsedAt.isEmpty {
                Text("·").accessibilityHidden(true)
                Text("recalled \(shortDay(memory.lastUsedAt))").lineLimit(1)
            }
        }
        .font(.siloMono(11.5))
        .foregroundStyle(Theme.ink3)
    }

    private func openChat(_ id: String) {
        if let chat = model.chats.first(where: { $0.id == id }) { model.openChat(chat) } else { model.showChat(id) }
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
        searching = true
        defer { searching = false }
        do { results = try await model.client.searchMemories(botID: botID, query: q) } catch { await model.report(error) }
    }

    private func delete(_ memory: Silo_V1_Memory) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteMemory(botID: botID, id: memory.id)
            withAnimation(Motion.state) {
                memories.removeAll { $0.id == memory.id }
                results?.removeAll { $0.id == memory.id }
            }
        } catch { await model.report(error) }
    }
}
