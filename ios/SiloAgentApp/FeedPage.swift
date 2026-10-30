import SiloClient
import SwiftUI

/// The owner's read-only inbox: what the Bot posted with `feed`. Delete, or reply in the main chat
/// (or a chat of its own).
struct FeedPage: View {
    @Environment(AppModel.self) private var model
    @State private var posts: [Silo_V1_FeedPost] = []
    @State private var loaded = false
    /// What was unread when it came in: opening the Feed reads it, and the dot stays for the visit.
    @State private var fresh: Set<String> = []
    @State private var arrived: Set<String> = []
    @State private var quoting = ""
    @State private var quoted = 0

    var body: some View {
        PageScroll(spacing: 12) {
            if !loaded {
                SkeletonRows(count: 3, height: 120)
            } else if posts.isEmpty {
                EmptyWell(symbol: "tray", title: "Nothing posted yet", message: "When the Bot posts to its Feed, it shows up here.")
            } else {
                ForEach(posts, id: \.id) { post in
                    card(post).rise(when: arrived.contains(post.id))
                }
            }
        }
        .sensoryFeedback(.success, trigger: quoted)
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            await load()
            await markRead()
        }
    }

    private func card(_ post: Silo_V1_FeedPost) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 2) {
                VStack(alignment: .leading, spacing: 3) {
                    if !post.title.isEmpty {
                        Text(post.title).font(.silo(15, .semibold)).tracking(-0.15).foregroundStyle(Theme.ink)
                    }
                    HStack(spacing: 6) {
                        if fresh.contains(post.id) {
                            Circle().fill(Theme.cobalt).frame(width: 6, height: 6).accessibilityLabel("New")
                        }
                        Text(When.stamp(post.createdAt)).font(.siloMono(11.5))
                        if !post.sourceName.isEmpty {
                            Text("·").accessibilityHidden(true)
                            Text("\(sourceLabel(post.sourceKind)) · \(post.sourceName)").lineLimit(1)
                        }
                    }
                    .font(.silo(12))
                    .foregroundStyle(Theme.ink3)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Button { Task { await quote(post) } } label: { Image(systemName: "quote.opening") }
                    .buttonStyle(.glyph(tint: Theme.ink2, side: 34))
                    .disabled(!quoting.isEmpty)
                    .accessibilityLabel("Reply in the main chat")
                Button { Task { await quote(post, newChat: true) } } label: { Image(systemName: "square.and.pencil") }
                    .buttonStyle(.glyph(tint: Theme.ink2, side: 34))
                    .disabled(!quoting.isEmpty)
                    .accessibilityLabel("Reply in a new chat")
                ArmedGlyph(symbol: "trash", label: "Delete post") { Task { await delete(post) } }
            }
            ProseView(text: post.body)
        }
        .padding(.leading, 16)
        .padding(.trailing, 10)
        .padding(.top, 12)
        .padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(Theme.line))
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
        do {
            let list = try await model.client.listFeed(botID: botID)
            arrived = loaded ? Set(list.map(\.id)).subtracting(posts.map(\.id)) : []
            fresh.formUnion(list.filter { !$0.read }.map(\.id))
            withAnimation(Motion.state) { posts = list }
        } catch { await model.report(error) }
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
            withAnimation(Motion.state) { posts.removeAll { $0.id == post.id } }
        } catch { await model.report(error) }
    }

    /// Replies to a post in the main chat (or a chat of its own while a reply runs there).
    private func quote(_ post: Silo_V1_FeedPost, newChat: Bool = false) async {
        guard let botID = model.selectedBotID else { return }
        quoting = post.id
        defer { quoting = "" }
        do {
            model.openChat(try await model.client.quoteFeedPost(botID: botID, id: post.id, newChat: newChat))
            quoted += 1
        } catch { await model.report(error) }
    }
}
