import QuickLook
import SiloClient
import SwiftUI

extension Silo_V1_Mail: @retroactive Identifiable {}

/// The Bot's receive-only inbox: its address to copy or share, and what arrived. The Bot reads the
/// same messages with `read_mail`; nothing is sent from here. Whether mail starts a chat is in
/// Settings.
struct MailPage: View {
    @Environment(AppModel.self) private var model
    @State private var box: Silo_V1_Mailbox?
    @State private var messages: [Silo_V1_Mail] = []
    @State private var loaded = false
    @State private var deleting: Silo_V1_Mail?

    var body: some View {
        List {
            if let box {
                Section {
                    if !box.address.isEmpty { AddressRow(address: box.address) }
                    if let notice = mailNotice(box.state) { Text(notice).foregroundStyle(.secondary) }
                    if !box.problem.isEmpty {
                        Label("The mail listener is not running: \(box.problem)", systemImage: "exclamationmark.triangle")
                            .foregroundStyle(Theme.vermilion)
                    }
                } footer: {
                    Text(box.wake ? "Mail from your listed senders starts a chat." : "Anyone can write to this address. The Bot reads it when asked.")
                }
            }
            Section {
                ForEach(messages) { mail in
                    NavigationLink {
                        MailDetail(id: mail.id, summary: mail) { deleted in
                            messages.removeAll { $0.id == deleted }
                        }
                    } label: {
                        MailRow(mail: mail)
                    }
                    .swipeActions {
                        Button(role: .destructive) { deleting = mail } label: { Label("Delete", systemImage: "trash") }
                    }
                }
            }
        }
        .overlay {
            if loaded && messages.isEmpty && box?.state == "ok" {
                ContentUnavailableView("No mail yet", systemImage: "envelope", description: Text("Messages sent to the Bot's address show up here."))
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(10))
            }
        }
        .confirmationDialog("Delete this message?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { mail in
            Button("Delete message", role: .destructive) { Task { await delete(mail) } }
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listMail(botID: botID)
            box = list.mailbox
            messages = list.messages
        } catch { await model.report(error) }
        loaded = true
    }

    private func delete(_ mail: Silo_V1_Mail) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteMail(botID: botID, id: mail.id)
            messages.removeAll { $0.id == mail.id }
        } catch { await model.report(error) }
    }
}

/// The address in mono with Copy and Share.
struct AddressRow: View {
    let address: String
    @State private var copied = false

    var body: some View {
        HStack {
            Text(address).font(.callout.monospaced()).textSelection(.enabled).lineLimit(1).minimumScaleFactor(0.7)
            Spacer(minLength: 8)
            Button {
                UIPasteboard.general.string = address
                copied = true
                Task { try? await Task.sleep(for: .seconds(1.5)); copied = false }
            } label: {
                Image(systemName: copied ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(copied ? "Copied" : "Copy address")
            .sensoryFeedback(.success, trigger: copied) { _, now in now }
            ShareLink(item: address) { Image(systemName: "square.and.arrow.up") }
                .buttonStyle(.borderless)
        }
    }
}

private struct MailRow: View {
    let mail: Silo_V1_Mail

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if !mail.read { Circle().fill(Theme.cobalt).frame(width: 8, height: 8).accessibilityLabel("Unread by the Bot") }
                Text(mailSenderName(from: mail.from, address: mail.fromAddress))
                    .font(.headline)
                    .lineLimit(1)
                if mail.verified {
                    Image(systemName: "checkmark.seal.fill").font(.caption).foregroundStyle(Theme.emerald).accessibilityLabel("Verified sender")
                }
                Spacer(minLength: 6)
                Text(relativeTime(mail.receivedAt)).font(.caption).foregroundStyle(.secondary)
            }
            Text(mail.subject.isEmpty ? "(no subject)" : mail.subject).font(.subheadline).lineLimit(1)
            if !mail.preview.isEmpty {
                Text(mail.preview).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
            }
            if !mail.attachments.isEmpty {
                Label("\(mail.attachments.count)", systemImage: "paperclip").font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}

/// One message as plain text (never rendered as Markdown or HTML: a stranger wrote it).
private struct MailDetail: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let id: String
    let summary: Silo_V1_Mail
    let deleted: (String) -> Void
    @State private var mail: Silo_V1_Mail?
    @State private var original: URL?
    @State private var confirmDelete = false

    var body: some View {
        let m = mail ?? summary
        List {
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text(m.subject.isEmpty ? "(no subject)" : m.subject).font(.title3.weight(.semibold))
                    LabeledContent("From") { Text(m.from.isEmpty ? m.fromAddress : m.from).textSelection(.enabled) }
                    if !m.to.isEmpty { LabeledContent("To") { Text(m.to).textSelection(.enabled) } }
                    LabeledContent("Received") { Text(parseDate(m.receivedAt)?.formatted(date: .abbreviated, time: .shortened) ?? "") }
                }
                .font(.subheadline)
                Label {
                    Text(m.verified ? "The sender's domain vouched for this message." : (m.authDetail.isEmpty ? "The sender could not be verified." : m.authDetail))
                } icon: {
                    Image(systemName: m.verified ? "checkmark.seal.fill" : "exclamationmark.shield")
                        .foregroundStyle(m.verified ? Theme.emerald : Theme.vermilion)
                }
                .font(.footnote)
            }
            if !m.chatID.isEmpty {
                Section {
                    Button {
                        if let chat = model.chats.first(where: { $0.id == m.chatID }) { model.openChat(chat) } else { model.path.append(.chat(m.chatID)) }
                    } label: {
                        Label("Open the chat it started", systemImage: "bubble.left.and.bubble.right")
                    }
                }
            }
            Section {
                if let mail {
                    Text(mail.text.isEmpty ? "(empty)" : mail.text)
                        .font(.body)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                } else {
                    ProgressView().frame(maxWidth: .infinity)
                }
            }
            if !m.attachments.isEmpty {
                Section {
                    ForEach(m.attachments, id: \.index) { a in
                        LabeledContent {
                            Text(formatBytes(Int64(a.size)))
                        } label: {
                            Label(a.name.isEmpty ? "Attachment \(a.index + 1)" : a.name, systemImage: "paperclip")
                        }
                    }
                } header: {
                    Text("Attachments")
                } footer: {
                    Text("The Bot saves attachments to its workspace when it reads the message. Open the original to see them here.")
                }
            }
        }
        .navigationTitle("Message")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button { Task { await openOriginal() } } label: { Label("Open original (.eml)", systemImage: "doc.text") }
                    Button(role: .destructive) { confirmDelete = true } label: { Label("Delete", systemImage: "trash") }
                } label: {
                    Label("More", systemImage: "ellipsis.circle")
                }
            }
        }
        .quickLookPreview($original)
        .confirmationDialog("Delete this message?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete message", role: .destructive) { Task { await delete() } }
        }
        .task {
            guard let botID = model.selectedBotID else { return }
            do { mail = try await model.client.getMail(botID: botID, id: id) } catch { await model.report(error) }
        }
    }

    private func openOriginal() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let data = try await model.client.download(path: model.client.rawMailPath(botID: botID, id: id))
            original = try model.writeTemp(data, name: "message-\(id.prefix(8)).eml")
        } catch { await model.report(error) }
    }

    private func delete() async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteMail(botID: botID, id: id)
            deleted(id)
            dismiss()
        } catch { await model.report(error) }
    }
}

/// Bot Settings → Mail: the address (copy, rotate) and whether mail from listed senders starts a chat.
struct MailSettingsSection: View {
    @Environment(AppModel.self) private var model
    @State private var box: Silo_V1_Mailbox?
    @State private var wake = false
    @State private var wakeFrom = ""
    @State private var saving = false
    @State private var confirmRotate = false

    var body: some View {
        Section {
            if let box {
                AddressRow(address: box.address)
                Toggle("Start a chat on new mail", isOn: $wake)
                if wake {
                    TextField("ada@example.com, @example.org", text: $wakeFrom, axis: .vertical)
                        .lineLimit(1...4)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.emailAddress)
                        .autocorrectionDisabled()
                }
                if let error = mailWakeListError(on: wake, raw: wakeFrom), dirty {
                    Text(error).font(.footnote).foregroundStyle(Theme.vermilion)
                }
                if dirty {
                    Button(saving ? "Saving…" : "Save mail settings") { Task { await save() } }
                        .disabled(saving || mailWakeListError(on: wake, raw: wakeFrom) != nil)
                }
                Button("New address…", role: .destructive) { confirmRotate = true }
            } else {
                ProgressView()
            }
        } header: {
            Text("Mail")
        } footer: {
            Text("Only verified mail from these senders (addresses or @domain) starts a chat, at most 10 an hour. The rest waits in the inbox.")
        }
        .task(id: model.selectedBotID) { await load() }
        .confirmationDialog("Get a new address?", isPresented: $confirmRotate, titleVisibility: .visible) {
            Button("Replace the address", role: .destructive) { Task { await rotate() } }
        } message: {
            Text("Mail to the old address is refused from then on.")
        }
    }

    private var dirty: Bool { box.map { $0.wake != wake || $0.wakeFrom != wakeFrom } ?? false }

    private func apply(_ b: Silo_V1_Mailbox) {
        box = b
        wake = b.wake
        wakeFrom = b.wakeFrom
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { apply(try await model.client.listMail(botID: botID).mailbox) } catch { await model.report(error) }
    }

    private func save() async {
        guard let botID = model.selectedBotID else { return }
        saving = true
        defer { saving = false }
        do { apply(try await model.client.updateMailbox(botID: botID, wake: wake, wakeFrom: wakeFrom)) } catch { await model.report(error) }
    }

    private func rotate() async {
        guard let botID = model.selectedBotID else { return }
        do {
            apply(try await model.client.rotateMailbox(botID: botID))
            await model.refreshBots()
        } catch { await model.report(error) }
    }
}
