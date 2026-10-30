import QuickLook
import SiloClient
import SwiftUI

extension Silo_V1_Mail: @retroactive Identifiable {}

/// The Bot's receive-only inbox: its address to copy or share, and what arrived, each message
/// opening in place. The Bot reads the same messages with `read_mail`; nothing is sent from here.
/// Whether mail starts a chat is in Settings.
struct MailPage: View {
    @Environment(AppModel.self) private var model
    @State private var box: Silo_V1_Mailbox?
    @State private var messages: [Silo_V1_Mail] = []
    @State private var loaded = false
    @State private var open = ""
    @State private var seen = FirstSeen()
    @State private var removed = 0

    var body: some View {
        PageScroll(spacing: 12) {
            if let box {
                if !box.address.isEmpty {
                    PanelSection(footer: box.wake ? "Mail from your listed senders starts a chat." : "Anyone can write to this address. The Bot reads it when asked.") {
                        AddressRow(address: box.address)
                    }
                }
                if let notice = mailNotice(box.state) { MachineNote(text: notice) }
                if !box.problem.isEmpty { ErrorLine(text: "The mail listener is not running: \(box.problem)") }
            }
            if !loaded {
                SkeletonRows(count: 3, height: 64)
            } else if messages.isEmpty {
                if box?.state == "ok" {
                    EmptyWell(symbol: "envelope", title: "No mail yet", message: "Anything sent to the address above lands here, usually within a minute.")
                }
            } else {
                PanelSection(footer: footnote) {
                    ForEach(messages) { mail in
                        MailRow(mail: mail, open: open == mail.id) {
                            withAnimation(Motion.fold) { open = open == mail.id ? "" : mail.id }
                        } delete: {
                            Task { await delete(mail) }
                        }
                        .rise(when: seen.isNew(mail.id))
                    }
                }
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(10))
            }
        }
        .sensoryFeedback(.success, trigger: removed)
    }

    private var footnote: String {
        let dot = "A dot marks one the Bot has not read yet."
        guard let keep = box?.keep, keep > 0 else { return dot }
        return "The newest \(keep) messages are kept. \(dot)"
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listMail(botID: botID)
            box = list.mailbox
            seen.note(list.messages.map(\.id))
            withAnimation(loaded ? Motion.state : nil) { messages = list.messages }
        } catch { await model.report(error) }
        loaded = true
    }

    private func delete(_ mail: Silo_V1_Mail) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteMail(botID: botID, id: mail.id)
            withAnimation(Motion.fold) {
                messages.removeAll { $0.id == mail.id }
                if open == mail.id { open = "" }
            }
            removed += 1
        } catch { await model.report(error) }
    }
}

/// The address in mono with Copy and Share.
struct AddressRow: View {
    let address: String

    var body: some View {
        HStack(spacing: 0) {
            Text(address)
                .font(.siloMono(13.5, .medium))
                .foregroundStyle(Theme.ink)
                .textSelection(.enabled)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .frame(maxWidth: .infinity, alignment: .leading)
            CopyButton(text: address)
                .accessibilityLabel("Copy address")
            ShareLink(item: address) {
                Image(systemName: "square.and.arrow.up")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 40, height: 40)
            }
            .buttonStyle(.press)
        }
        .padding(.leading, 14)
        .padding(.trailing, 4)
        .frame(minHeight: 48)
    }
}

/// One message: sender, subject, preview, and marks for what it carries. It opens in place.
private struct MailRow: View {
    let mail: Silo_V1_Mail
    let open: Bool
    let toggle: () -> Void
    let delete: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button(action: toggle) {
                HStack(alignment: .top, spacing: 10) {
                    MachineChevron(open: open).padding(.top, 4)
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            if !mail.read {
                                Circle().fill(Theme.cobalt).frame(width: 6, height: 6)
                                    .alignmentGuide(.firstTextBaseline) { $0[.bottom] }
                                    .accessibilityLabel("Not read by the Bot yet")
                            }
                            Text(mailSenderName(from: mail.from, address: mail.fromAddress))
                                .font(.silo(13.5, .medium))
                                .foregroundStyle(Theme.ink)
                                .lineLimit(1)
                            if !mail.verified {
                                Image(systemName: "exclamationmark.shield")
                                    .font(.system(size: 12))
                                    .foregroundStyle(Theme.ink3)
                                    .accessibilityLabel("Sender not verified")
                            }
                            if !mail.attachments.isEmpty {
                                Image(systemName: "paperclip")
                                    .font(.system(size: 11))
                                    .foregroundStyle(Theme.ink3)
                                    .accessibilityLabel("\(mail.attachments.count) attached")
                            }
                            Spacer(minLength: 0)
                            Text(relativeTime(mail.receivedAt)).font(.siloMono(12)).foregroundStyle(Theme.ink3).fixedSize()
                        }
                        Text(mail.subject.isEmpty ? "(no subject)" : mail.subject)
                            .font(.silo(13.5))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                        if !open, !mail.preview.isEmpty {
                            Text(mail.preview).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                        }
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
                .contentShape(Rectangle())
            }
            .buttonStyle(RowPressStyle())
            .accessibilityValue(open ? "Open" : "Closed")
            if open {
                MailBody(summary: mail, delete: delete).transition(.opacity)
            }
        }
    }
}

/// An open message, as plain text (never rendered as Markdown or HTML: a stranger wrote it).
private struct MailBody: View {
    @Environment(AppModel.self) private var model
    let summary: Silo_V1_Mail
    let delete: () -> Void
    @State private var mail: Silo_V1_Mail?
    @State private var original: URL?

    var body: some View {
        let m = mail ?? summary
        VStack(alignment: .leading, spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                fact("From", m.from.isEmpty ? m.fromAddress : m.from)
                if !m.to.isEmpty { fact("To", m.to) }
                fact("Received", parseDate(m.receivedAt)?.formatted(date: .abbreviated, time: .shortened) ?? "")
            }
            sender(m)
            if let mail {
                Text(mail.text.isEmpty ? "(no text)" : mail.text)
                    .font(.silo(13.5))
                    .lineSpacing(3)
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            } else {
                SkeletonRows(count: 1, height: 64)
            }
            if !m.attachments.isEmpty { attachments(m) }
            if !m.chatID.isEmpty {
                Button {
                    if let chat = model.chats.first(where: { $0.id == m.chatID }) { model.openChat(chat) } else { model.showChat(m.chatID) }
                } label: {
                    action("Open the chat it started", symbol: "bubble.left.and.bubble.right")
                }
                .buttonStyle(.quiet)
                .controlSize(.small)
            }
            HStack(spacing: 4) {
                Button { Task { await openOriginal() } } label: {
                    action("Open original", symbol: "doc.text")
                }
                .buttonStyle(.ghost)
                .controlSize(.small)
                Spacer(minLength: 0)
                ArmedButton(title: "Delete", symbol: "trash", small: true, action: delete)
            }
        }
        .padding(.leading, 35)
        .padding(.trailing, 14)
        .padding(.bottom, 12)
        .quickLookPreview($original)
        .task {
            guard let botID = model.selectedBotID else { return }
            do { mail = try await model.client.getMail(botID: botID, id: summary.id) } catch { await model.report(error) }
        }
    }

    private func fact(_ label: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label).font(.silo(12.5)).foregroundStyle(Theme.ink3).frame(width: 58, alignment: .leading)
            Text(value).font(.silo(12.5)).foregroundStyle(Theme.ink2).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// The sender check as one line: an `emerald` shield, or all of it in `vermilion`.
    private func sender(_ m: Silo_V1_Mail) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .top, spacing: 6) {
                Image(systemName: m.verified ? "checkmark.shield" : "exclamationmark.shield")
                    .font(.system(size: 13))
                    .foregroundStyle(m.verified ? Theme.emerald : Theme.vermilion)
                Text(m.verified ? "Sender verified: their domain vouched for this message." : "Sender not verified: the From address may be forged.")
                    .font(.silo(12.5))
                    .foregroundStyle(m.verified ? Theme.ink2 : Theme.vermilion)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !m.verified, !m.authDetail.isEmpty {
                Text(m.authDetail).font(.silo(12.5)).foregroundStyle(Theme.ink3).padding(.leading, 19)
            }
        }
    }

    private func attachments(_ m: Silo_V1_Mail) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(m.attachments, id: \.index) { a in
                HStack(spacing: 6) {
                    Image(systemName: "paperclip").font(.system(size: 11)).foregroundStyle(Theme.ink3)
                    Text(a.name.isEmpty ? "Attachment \(a.index + 1)" : a.name)
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink2)
                        .lineLimit(1)
                    Text(formatBytes(Int64(a.size))).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).fixedSize()
                }
                .padding(.horizontal, 8)
                .frame(minHeight: 28)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            }
            Text("The Bot saves attachments to its workspace when it reads the message. Open the original to see them here.")
                .font(.silo(12.5))
                .foregroundStyle(Theme.ink3)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func action(_ title: String, symbol: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).font(.system(size: 12))
            Text(title)
        }
    }

    private func openOriginal() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let data = try await model.client.download(path: model.client.rawMailPath(botID: botID, id: summary.id))
            original = try model.writeTemp(data, name: "message-\(summary.id.prefix(8)).eml")
        } catch { await model.report(error) }
    }
}

/// Bot Settings → Mail: the address (copy, rotate) and whether mail from listed senders starts a chat.
struct MailSettingsSection: View {
    @Environment(AppModel.self) private var model
    @State private var box: Silo_V1_Mailbox?
    @State private var wake = false
    @State private var wakeFrom = ""

    var body: some View {
        PanelSection(title: "Mail", footer: "Only verified mail from these senders (addresses or @domain) starts a chat, at most 10 an hour. The rest waits in the inbox.") {
            if let box {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 12) {
                        MachineTile(symbol: "envelope")
                        Text(box.address)
                            .font(.siloMono(13.5, .medium))
                            .foregroundStyle(Theme.ink)
                            .textSelection(.enabled)
                            .lineLimit(1)
                            .minimumScaleFactor(0.7)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        CopyButton(text: box.address)
                            .accessibilityLabel("Copy address")
                    }
                    Text("Anyone who knows it can write to it. Adding +anything before the @ reaches the same mailbox.")
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink2)
                        .fixedSize(horizontal: false, vertical: true)
                    ArmedButton(title: "New address", armedTitle: "Tap again: the old address stops working",
                                symbol: "arrow.clockwise", small: true) {
                        Task { await rotate() }
                    }
                }
                .padding(14)
                PanelRow(title: "Start a chat on new mail", trailing: {
                    MachineSwitch(label: "Start a chat on new mail", isOn: $wake.animation(Motion.fold))
                })
                if wake || !wakeFrom.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        SiloField(label: "Senders", prompt: "ada@example.com, @example.org", text: $wakeFrom, mono: true, axis: .vertical, lines: 1...4,
                                  error: dirty ? mailWakeListError(on: wake, raw: wakeFrom) : nil)
                            .textInputAutocapitalization(.never)
                            .keyboardType(.emailAddress)
                            .autocorrectionDisabled()
                    }
                    .padding(14)
                    .transition(.opacity)
                }
                if dirty {
                    HStack(spacing: 8) {
                        Spacer(minLength: 0)
                        Button("Discard") { withAnimation(Motion.fold) { apply(box) } }
                            .buttonStyle(.ghost)
                            .controlSize(.small)
                        SaveButton(enabled: mailWakeListError(on: wake, raw: wakeFrom) == nil) { await save() }
                            .controlSize(.small)
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Theme.well)
                    .transition(.opacity)
                }
            } else {
                SkeletonRows(count: 2, height: 48).padding(14)
            }
        }
        .task(id: model.selectedBotID) { await load() }
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

    private func save() async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        do {
            apply(try await model.client.updateMailbox(botID: botID, wake: wake, wakeFrom: wakeFrom))
            return true
        } catch {
            await model.report(error)
            return false
        }
    }

    private func rotate() async {
        guard let botID = model.selectedBotID else { return }
        do {
            apply(try await model.client.rotateMailbox(botID: botID))
            await model.refreshBots()
        } catch { await model.report(error) }
    }
}
