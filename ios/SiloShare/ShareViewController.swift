import AppIntents
import SiloClient
import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// "Send to a Bot" in the share sheet: a link, some text or files go to its main chat, with a line
/// from you. Silo's own form, not the system's compose card.
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        SiloGroup.boot()
        view.backgroundColor = .clear
        let root = ShareCard(items: (extensionContext?.inputItems as? [NSExtensionItem]) ?? []) { [weak self] in
            self?.extensionContext?.completeRequest(returningItems: nil)
        }
        let host = UIHostingController(rootView: root)
        host.view.backgroundColor = .clear
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }
}

private struct ShareCard: View {
    let items: [NSExtensionItem]
    let done: () -> Void
    private let state = SiloStore.load()
    @State private var bot = ""
    @State private var note = ""
    @State private var shared = ""
    @State private var files: [(name: String, data: Data)] = []
    @State private var sending = false
    @State private var failure: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Button("Cancel", action: done).buttonStyle(.ghost).controlSize(.small)
                Spacer()
                Text("Send to a Bot").font(.silo(15, .semibold)).foregroundStyle(Theme.ink)
                Spacer()
                Button(sending ? "Sending…" : "Send") { Task { await send() } }
                    .buttonStyle(.ink)
                    .controlSize(.small)
                    .disabled(bot.isEmpty || sending)
            }
            if state.bots.isEmpty {
                EmptyWell(title: "Open Silo and sign in first.")
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(state.bots) { row in
                            Button { bot = row.id } label: { botChip(row) }.buttonStyle(.press)
                        }
                    }
                }
                SiloField(prompt: "Add a line for your Bot…", text: $note, axis: .vertical, lines: 2...4)
                if !shared.isEmpty {
                    Text(shared).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(3)
                        .padding(10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                }
                ForEach(Array(files.enumerated()), id: \.offset) { _, file in
                    HStack(spacing: 6) {
                        Image(systemName: "paperclip").font(.system(size: 12))
                        Text(file.name).font(.silo(13)).lineLimit(1)
                    }
                    .foregroundStyle(Theme.ink2)
                }
                if let failure { ErrorLine(text: failure) }
            }
            Spacer(minLength: 0)
        }
        .padding(16)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Theme.surface.ignoresSafeArea())
        .tint(Theme.cobalt)
        .task {
            bot = state.lastBot?.id ?? ""
            await load()
        }
    }

    private func botChip(_ row: SharedBot) -> some View {
        let on = row.id == bot
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        return HStack(spacing: 8) {
            CrestView(index: row.crest, size: 22)
            Text(row.name).font(.silo(13.5, .medium)).foregroundStyle(on ? Theme.ink : Theme.ink2).lineLimit(1)
        }
        .padding(.horizontal, 10)
        .frame(height: 40)
        .background(on ? Theme.cobaltPale : Theme.surface, in: shape)
        .overlay(shape.strokeBorder(on ? Theme.cobalt : Theme.lineStrong))
    }

    private func load() async {
        for provider in items.flatMap({ $0.attachments ?? [] }) {
            if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier),
               let url = try? await provider.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL, !url.isFileURL {
                shared += (shared.isEmpty ? "" : "\n") + url.absoluteString
            } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier),
                      let text = try? await provider.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String {
                shared += (shared.isEmpty ? "" : "\n") + text
            } else if let type = provider.registeredTypeIdentifiers.first {
                let name = provider.suggestedName ?? "shared"
                let item = try? await provider.loadItem(forTypeIdentifier: type)
                if let url = item as? URL, let data = try? Data(contentsOf: url) {
                    files.append((url.lastPathComponent, data))
                } else if let data = item as? Data {
                    files.append((name, data))
                } else if let image = item as? UIImage, let data = image.jpegData(compressionQuality: 0.9) {
                    files.append((name + ".jpg", data))
                }
            }
        }
    }

    private func send() async {
        sending = true
        defer { sending = false }
        failure = nil
        let client = SiloClient(host: state.server)
        do {
            var attachments: [Silo_V1_Attachment] = []
            for file in files {
                let safe = file.name.replacingOccurrences(of: "/", with: "_")
                let path = "tmp/\(safe)"
                try await client.putFile(botID: bot, path: path, data: file.data)
                var item = Silo_V1_Attachment()
                item.name = safe
                item.path = path
                item.size = Int64(file.data.count)
                attachments.append(item)
            }
            let text = [note, shared].filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.joined(separator: "\n\n")
            _ = try await client.send(botID: bot, chatID: "", text: text, attachments: attachments)
            done()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
