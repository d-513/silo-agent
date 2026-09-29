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
    @State private var deleting: Silo_V1_Channel?

    var body: some View {
        List {
            ForEach(channels) { channel in
                Button { editing = channel } label: {
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(channel.name).font(.headline).foregroundStyle(.primary)
                            Spacer()
                            Toggle("Enabled", isOn: Binding(get: { channel.enabled }, set: { value in Task { await setEnabled(channel, value) } }))
                                .labelsHidden()
                        }
                        Text(channel.adapterName.isEmpty ? channel.adapter : channel.adapterName).font(.caption).foregroundStyle(.secondary)
                        if let need = needsTarget(channel) {
                            Text(need).font(.caption.weight(.semibold)).foregroundStyle(Theme.vermilion)
                        } else if !channel.targetTitle.isEmpty {
                            Text("→ \(channel.targetTitle)").font(.caption).foregroundStyle(.secondary)
                        }
                        if !channel.statusDetail.isEmpty {
                            Text(channel.statusDetail).font(.caption).foregroundStyle(channel.status == "error" ? Theme.vermilion : .secondary).lineLimit(2)
                        }
                    }
                }
                .swipeActions { Button(role: .destructive) { deleting = channel } label: { Label("Delete", systemImage: "trash") } }
            }
        }
        .overlay {
            if loaded && channels.isEmpty {
                ContentUnavailableView("No channels", systemImage: "antenna.radiowaves.left.and.right", description: Text("Let the Bot talk in Telegram and other chats."))
            }
        }
        .toolbar { ToolbarItem(placement: .primaryAction) { Button { adding = true } label: { Label("Add", systemImage: "plus") } } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(item: $editing) { channel in
            ChannelForm(adapter: adapters.first { $0.slug == channel.adapter }, existing: channel) { await load() }
        }
        .sheet(isPresented: $adding) { AddChannelSheet(adapters: adapters) { await load() } }
        .confirmationDialog("Delete this channel?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { channel in
            Button("Delete \(channel.name)", role: .destructive) { Task { await delete(channel) } }
        }
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
            (channels, adapters) = try await (list, kinds)
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
            channels.removeAll { $0.id == channel.id }
        } catch { await model.report(error) }
    }
}

private struct AddChannelSheet: View {
    @Environment(\.dismiss) private var dismiss
    let adapters: [Silo_V1_ChannelAdapter]
    let onSaved: () async -> Void

    var body: some View {
        NavigationStack {
            List(adapters) { adapter in
                NavigationLink {
                    ChannelForm(adapter: adapter, existing: nil) { await onSaved(); dismiss() }
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(adapter.name).font(.headline)
                        Text(adapter.description_p).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
            .navigationTitle("Add channel")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
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

    @State private var name = ""
    @State private var enabled = true
    @State private var inbound = true
    @State private var prompt = ""
    @State private var config: [String: String] = [:]
    @State private var secrets: [String: String] = [:]
    @State private var failure: String?
    @State private var saving = false

    var body: some View {
        Form {
            Section("Name") { TextField("Name", text: $name) }
            if let adapter {
                let plain = adapter.fields.filter { !$0.secret && !$0.advanced }
                let secret = adapter.fields.filter(\.secret)
                if !plain.isEmpty {
                    Section("Settings") { ForEach(plain, id: \.key) { field in fieldRow(field) } }
                }
                if !secret.isEmpty {
                    Section {
                        ForEach(secret, id: \.key) { field in
                            SecureField(field.label + (existing?.secretsSet.contains(field.key) == true ? " (set)" : ""), text: binding(secrets, field.key, secret: true))
                        }
                    } header: { Text("Secrets") } footer: { Text("Leave blank to keep the stored value.") }
                }
                if adapter.requiresTarget {
                    Section { Text("Picking which chat this channel reads and writes happens on the web client.").font(.footnote) }
                }
            }
            Section {
                Toggle("Enabled", isOn: $enabled)
                Toggle("Reply to incoming messages", isOn: $inbound)
            } footer: { Text("Off makes it send-only: the Bot can post through it but does not read it.") }
            Section("Extra instructions") { TextEditor(text: $prompt).frame(minHeight: 70) }
            if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
        }
        .navigationTitle(existing == nil ? "New channel" : "Edit channel")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(saving ? "Saving…" : "Save") { Task { await save() } }
                    .disabled(saving || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        }
        .onAppear(perform: load)
    }

    @ViewBuilder
    private func fieldRow(_ field: Silo_V1_ChannelField) -> some View {
        if !field.options.isEmpty {
            Picker(field.label, selection: binding(config, field.key)) {
                ForEach(field.options, id: \.value) { Text($0.label).tag($0.value) }
            }
        } else {
            TextField(field.label, text: binding(config, field.key))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
    }

    private func binding(_ dict: [String: String], _ key: String, secret: Bool = false) -> Binding<String> {
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

    private func save() async {
        guard let botID = model.selectedBotID else { return }
        saving = true
        defer { saving = false }
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
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
