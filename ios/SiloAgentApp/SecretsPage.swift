import SiloClient
import SwiftUI

/// Named secrets the Bot can read with `secrets.<name>`. Values are write-only here.
struct SecretsPage: View {
    @Environment(AppModel.self) private var model
    @State private var secrets: [Silo_V1_SecretMeta] = []
    @State private var loaded = false
    @State private var adding = false
    @State private var deleting: Silo_V1_SecretMeta?

    var body: some View {
        List {
            ForEach(secrets, id: \.id) { secret in
                VStack(alignment: .leading, spacing: 2) {
                    Text(secret.name).font(.system(.body, design: .monospaced))
                    Text(secret.lastUsedAt.isEmpty ? "Never used" : "Used \(relativeTime(secret.lastUsedAt))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .swipeActions { Button(role: .destructive) { deleting = secret } label: { Label("Delete", systemImage: "trash") } }
            }
        }
        .overlay {
            if loaded && secrets.isEmpty {
                ContentUnavailableView("No secrets", systemImage: "key", description: Text("Add API keys the Bot can use without ever seeing them in chat."))
            }
        }
        .toolbar {
            ToolbarItem(placement: .bottomBar) {
                Button { adding = true } label: { Label("Add Secret", systemImage: "plus") }
            }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(isPresented: $adding) { AddSecretSheet { await load() } }
        .confirmationDialog("Delete this secret?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible, presenting: deleting) { secret in
            Button("Delete \(secret.name)", role: .destructive) { Task { await delete(secret) } }
        } message: { _ in Text("Rules for this secret are removed too.") }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { secrets = try await model.client.listSecrets(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func delete(_ secret: Silo_V1_SecretMeta) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteSecret(botID: botID, id: secret.id)
            secrets.removeAll { $0.id == secret.id }
        } catch { await model.report(error) }
    }
}

private struct AddSecretSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let onAdded: () async -> Void
    @State private var name = ""
    @State private var value = ""
    @State private var failure: String?

    var body: some View {
        NavigationStack {
            Form {
                Section("Name") {
                    TextField("GITHUB_TOKEN", text: $name)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.system(.body, design: .monospaced))
                }
                Section {
                    SecureField("Value", text: $value)
                } footer: {
                    Text("Stored on the Control Plane and never shown again.")
                }
                if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
            }
            .navigationTitle("Add secret")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { Task { await add() } }
                        .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || value.isEmpty)
                }
            }
        }
        .presentationDetents([.medium])
    }

    private func add() async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.addSecret(botID: botID, name: name.trimmingCharacters(in: .whitespaces), value: value)
            await onAdded()
            dismiss()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
