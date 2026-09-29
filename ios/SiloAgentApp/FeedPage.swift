import SiloClient
import SwiftUI

/// The owner's read-only inbox: what the Bot posted with `feed`. Delete, or Quote into a new chat.
struct FeedPage: View {
    @Environment(AppModel.self) private var model
    @State private var posts: [Silo_V1_FeedPost] = []
    @State private var loaded = false
    @State private var deleting: Silo_V1_FeedPost?

    var body: some View {
        List {
            ForEach(posts, id: \.id) { post in
                VStack(alignment: .leading, spacing: 8) {
                    HStack(alignment: .firstTextBaseline) {
                        if !post.read { Circle().fill(Theme.cobalt).frame(width: 7, height: 7) }
                        Text(post.title.isEmpty ? "Post" : post.title).font(.headline)
                        Spacer(minLength: 8)
                        Text(relativeTime(post.createdAt)).font(.caption).foregroundStyle(.secondary)
                    }
                    if !post.sourceName.isEmpty {
                        Text("\(sourceLabel(post.sourceKind)) · \(post.sourceName)")
                            .font(.caption.weight(.medium))
                            .foregroundStyle(.secondary)
                    }
                    ProseView(text: post.body)
                }
                .padding(.vertical, 4)
                .swipeActions(edge: .trailing) {
                    Button(role: .destructive) { deleting = post } label: { Label("Delete", systemImage: "trash") }
                    Button { Task { await quote(post) } } label: { Label("Quote", systemImage: "quote.opening") }
                        .tint(Theme.cobalt)
                }
                .contextMenu {
                    Button { Task { await quote(post) } } label: { Label("Quote in new chat", systemImage: "quote.opening") }
                    Button(role: .destructive) { deleting = post } label: { Label("Delete", systemImage: "trash") }
                }
            }
        }
        .listStyle(.plain)
        .overlay {
            if loaded && posts.isEmpty {
                ContentUnavailableView("Nothing posted yet", systemImage: "tray", description: Text("When the Bot posts to its Feed, it shows up here."))
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            await load()
            await markRead()
        }
        .confirmationDialog("Delete this post?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { post in
            Button("Delete post", role: .destructive) { Task { await delete(post) } }
        }
    }

    private func sourceLabel(_ kind: String) -> String {
        switch kind {
        case "automation": return "Automation"
        case "channel": return "Channel"
        default: return "Chat"
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { posts = try await model.client.listFeed(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func markRead() async {
        guard let botID = model.selectedBotID, model.selectedBot?.feedUnread ?? 0 > 0 else { return }
        try? await model.client.markFeedRead(botID: botID)
        await model.refreshBots()
    }

    private func delete(_ post: Silo_V1_FeedPost) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteFeedPost(botID: botID, id: post.id)
            posts.removeAll { $0.id == post.id }
        } catch { await model.report(error) }
    }

    private func quote(_ post: Silo_V1_FeedPost) async {
        guard let botID = model.selectedBotID else { return }
        do { model.openChat(try await model.client.quoteFeedPost(botID: botID, id: post.id)) } catch { await model.report(error) }
    }
}
