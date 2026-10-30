import SiloClient
import SwiftUI

extension Silo_V1_Channel: @retroactive Identifiable {}
extension Silo_V1_ChannelAdapter: @retroactive Identifiable { public var id: String { slug } }

/// Channels connect the Bot to outside chats. List, toggle, edit basic fields, add; the interactive
/// setup wizard (QR logins, chat pickers) stays on the web.
struct ChannelsPage: View {
    @Environment(AppModel.self) private var model
    @State private var channels: [Silo_V1_Channel] = []
    @State private var adapters: [Silo_V1_ChannelAdapter] = []
    @State private var loaded = false
    @State private var editing: Silo_V1_Channel?
    @State private var adding = false
    @State private var seen = FirstSeen()
    @State private var removed = 0

    var body: some View {
        PageScroll(spacing: 12) {
            if !loaded {
                SkeletonRows(count: 2, height: 120)
            } else if channels.isEmpty {
                EmptyWell(symbol: "antenna.radiowaves.left.and.right", title: "No channels", message: "Let the Bot talk in Telegram and other chats.") {
                    Button("Add channel") { adding = true }.buttonStyle(.quiet).controlSize(.small)
                }
            } else {
                ForEach(channels) { channel in
                    ChannelCard(channel: channel, need: needsTarget(channel)) {
                        editing = channel
                    } enable: { value in
                        Task { await setEnabled(channel, value) }
                    } delete: {
                        Task { await delete(channel) }
                    }
                    .rise(when: seen.isNew(channel.id))
                }
            }
        }
        .pageActions {
            Button { adding = true } label: { Image(systemName: "plus") }
                .buttonStyle(.glyph)
                .accessibilityLabel("Add channel")
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(item: $editing) { channel in
            ChannelForm(adapter: adapters.first { $0.slug == channel.adapter }, existing: channel) { await load() }
                .siloSheet()
        }
        .sheet(isPresented: $adding) {
            AddChannelSheet(adapters: adapters) { await load() }
                .siloSheet()
        }
        .sensoryFeedback(.success, trigger: removed)
    }

    private func needsTarget(_ channel: Silo_V1_Channel) -> String? {
        let requires = adapters.first { $0.slug == channel.adapter }?.requiresTarget ?? false
        return requires && channel.externalID.isEmpty ? "Set up on the web client to pick its chat." : nil
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            async let list = model.client.listBotChannels(botID: botID)
            async let kinds = model.client.listChannelAdapters()
            let (fresh, known) = try await (list, kinds)
            seen.note(fresh.map(\.id))
            adapters = known
            withAnimation(loaded ? Motion.state : nil) { channels = fresh }
        } catch { await model.report(error) }
        loaded = true
    }

    private func setEnabled(_ channel: Silo_V1_Channel, _ value: Bool) async {
        var request = Silo_V1_UpdateChannelRequest()
        request.botID = channel.botID
        request.id = channel.id
        request.name = channel.name
        request.enabled = value
        request.inbound = channel.inbound
        request.prompt = channel.prompt
        request.config = channel.config
        request.externalID = channel.externalID
        request.targetTitle = channel.targetTitle
        do { _ = try await model.client.updateChannel(request) } catch { await model.report(error) }
        await load()
    }

    private func delete(_ channel: Silo_V1_Channel) async {
        do {
            try await model.client.deleteChannel(botID: channel.botID, id: channel.id)
            withAnimation(Motion.fold) { channels.removeAll { $0.id == channel.id } }
            removed += 1
        } catch { await model.report(error) }
    }
}

/// One channel: who it is, what it is bound to, its state, and the ways into it. One that needs you
/// wears the ribbon.
private struct ChannelCard: View {
    let channel: Silo_V1_Channel
    let need: String?
    let edit: () -> Void
    let enable: (Bool) -> Void
    let delete: () -> Void

    private var look: StatusLook { channelLook(channel.status) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 12) {
                Button(action: edit) {
                    info.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
                }
                .buttonStyle(.press)
                MachineSwitch(label: "Enabled", isOn: Binding(get: { channel.enabled }, set: enable))
            }
            .padding(14)
            Theme.line.frame(height: 1)
            HStack(spacing: 0) {
                Button(action: edit) {
                    HStack(spacing: 6) {
                        Image(systemName: "pencil").font(.system(size: 12))
                        Text("Configure")
                    }
                }
                .buttonStyle(.ghost)
                .controlSize(.small)
                Spacer(minLength: 0)
                ArmedButton(title: "Delete", symbol: "trash", small: true, action: delete)
            }
            .padding(.horizontal, 4)
            .padding(.vertical, 4)
        }
        .machineFolio(ribbon: need != nil || look.lamp == .needsYou)
    }

    private var info: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(channel.name).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                Chip(text: channel.adapterName.isEmpty ? channel.adapter : channel.adapterName, tint: Theme.ink3)
                if !channel.enabled { Chip(text: "Disabled", tint: Theme.ink3) }
                if !channel.inbound { Chip(text: "Send-only", tint: Theme.ink3) }
            }
            if let need {
                Text(need).font(.silo(12.5, .medium)).foregroundStyle(Theme.vermilion)
            } else if !channel.targetTitle.isEmpty {
                Text("Chat: \(channel.targetTitle)").font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
            }
            StatusWord(look: look)
            if !channel.statusDetail.isEmpty {
                Text(channel.statusDetail)
                    .font(.silo(12.5))
                    .foregroundStyle(channel.status == "error" ? Theme.vermilion : Theme.ink3)
                    .lineLimit(2)
            }
        }
    }
}

/// Which adapter, then its form, in the same sheet.
private struct AddChannelSheet: View {
    @Environment(\.dismiss) private var dismiss
    let adapters: [Silo_V1_ChannelAdapter]
    let onSaved: () async -> Void
    @State private var picked: Silo_V1_ChannelAdapter?

    var body: some View {
        if let picked {
            ChannelForm(adapter: picked, existing: nil, onSaved: { await onSaved() }, cancel: "Back", onCancel: {
                withAnimation(Motion.state) { self.picked = nil }
            })
            .transition(.opacity)
        } else {
            VStack(spacing: 0) {
                SheetHead(title: "Add channel", onCancel: { dismiss() })
                PageScroll(spacing: 12) {
                    Panel {
                        ForEach(adapters) { adapter in
                            PanelRow(title: adapter.name, detail: adapter.description_p, chevron: true) {
                                withAnimation(Motion.state) { picked = adapter }
                            }
                        }
                    }
                }
            }
            .transition(.opacity)
        }
    }
}

/// Create or edit a channel from the adapter's declared fields. A blank secret keeps the stored one.
private struct ChannelForm: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let adapter: Silo_V1_ChannelAdapter?
    let existing: Silo_V1_Channel?
    let onSaved: () async -> Void
    var cancel = "Cancel"
    var onCancel: (() -> Void)?

    @State private var name = ""
    @State private var enabled = true
    @State private var inbound = true
    @State private var prompt = ""
    @State private var config: [String: String] = [:]
    @State private var secrets: [String: String] = [:]
    @State private var failure: String?

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: existing == nil ? "New channel" : "Edit channel", cancel: cancel, onCancel: onCancel ?? { dismiss() }) {
                SaveButton(enabled: !name.trimmingCharacters(in: .whitespaces).isEmpty) { await save() }
            }
            PageScroll(spacing: 16) {
                SiloField(label: "Name", prompt: "Name", text: $name)
                if let adapter {
                    let plain = adapter.fields.filter { !$0.secret && !$0.advanced }
                    let secret = adapter.fields.filter(\.secret)
                    ForEach(plain, id: \.key) { field in fieldRow(field) }
                    if !secret.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            ForEach(secret, id: \.key) { field in
                                let set = existing?.secretsSet.contains(field.key) == true
                                SiloField(label: field.label, prompt: set ? "••••••••" : "", text: binding(field.key, secret: true), mono: true, secure: true)
                            }
                            Text("Leave blank to keep the stored value.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                        }
                    }
                    if adapter.requiresTarget {
                        MachineNote(text: "Picking which chat this channel reads and writes happens on the web client.")
                    }
                }
                PanelSection(footer: "Off makes it send-only: the Bot can post through it but does not read it.") {
                    PanelRow(title: "Enabled", trailing: { MachineSwitch(label: "Enabled", isOn: $enabled) })
                    PanelRow(title: "Reply to incoming messages", trailing: { MachineSwitch(label: "Reply to incoming messages", isOn: $inbound) })
                }
                MachineEditor(label: "Extra instructions", text: $prompt, minHeight: 90)
                if let failure { ErrorLine(text: failure) }
            }
        }
        .onAppear(perform: load)
    }

    @ViewBuilder
    private func fieldRow(_ field: Silo_V1_ChannelField) -> some View {
        if !field.options.isEmpty {
            SiloPicker(label: field.label, options: field.options.map { (value: $0.value, label: $0.label) }, selection: binding(field.key))
        } else {
            SiloField(label: field.label, text: binding(field.key))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
    }

    private func binding(_ key: String, secret: Bool = false) -> Binding<String> {
        Binding(
            get: { (secret ? secrets : config)[key] ?? "" },
            set: { if secret { secrets[key] = $0 } else { config[key] = $0 } }
        )
    }

    private func load() {
        if let existing {
            name = existing.name
            enabled = existing.enabled
            inbound = existing.inbound
            prompt = existing.prompt
            config = existing.config
        } else if let adapter {
            name = adapter.name
        }
    }

    private func save() async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        let filled = secrets.filter { !$0.value.isEmpty }
        do {
            if let existing {
                var request = Silo_V1_UpdateChannelRequest()
                request.botID = botID
                request.id = existing.id
                request.name = name
                request.enabled = enabled
                request.inbound = inbound
                request.prompt = prompt
                request.config = config
                request.secrets = filled
                request.externalID = existing.externalID
                request.targetTitle = existing.targetTitle
                _ = try await model.client.updateChannel(request)
            } else if let adapter {
                var request = Silo_V1_CreateChannelRequest()
                request.botID = botID
                request.adapter = adapter.slug
                request.name = name
                request.enabled = enabled
                request.inbound = inbound
                request.prompt = prompt
                request.config = config
                request.secrets = filled
                _ = try await model.client.createChannel(request)
            }
            await onSaved()
            dismiss()
            return true
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
            return false
        }
    }
}
