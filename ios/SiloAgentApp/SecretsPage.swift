import SiloClient
import SwiftUI

/// Named secrets the Bot can read with `secrets.<name>`. Values are write-only here.
struct SecretsPage: View {
    @Environment(AppModel.self) private var model
    @State private var secrets: [Silo_V1_SecretMeta] = []
    @State private var loaded = false
    @State private var adding = false
    @State private var seen = FirstSeen()
    @State private var changed = 0

    var body: some View {
        PageScroll(spacing: 12) {
            if !loaded {
                SkeletonRows(count: 3, height: 52)
            } else if secrets.isEmpty {
                EmptyWell(symbol: "key", title: "No secrets", message: "Add API keys the Bot can use without ever seeing them in chat.") {
                    Button("Add secret") { adding = true }.buttonStyle(.quiet).controlSize(.small)
                }
            } else {
                PanelSection(footer: "Handed to the Bot only after you allow it. Deleting a secret removes its rules too.") {
                    ForEach(secrets, id: \.id) { secret in
                        row(secret).rise(when: seen.isNew(secret.id))
                    }
                }
            }
        }
        .pageActions {
            Button { adding = true } label: { Image(systemName: "plus") }
                .buttonStyle(.glyph)
                .accessibilityLabel("Add secret")
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(isPresented: $adding) {
            AddSecretSheet { await load() }
                .siloSheet(detents: [.medium])
        }
        .sensoryFeedback(.success, trigger: changed)
    }

    private func row(_ secret: Silo_V1_SecretMeta) -> some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(secret.name).font(.siloMono(13.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                HStack(spacing: 8) {
                    Text("••••••••").font(.siloMono(12.5)).foregroundStyle(Theme.ink3).accessibilityHidden(true)
                    Text(secret.lastUsedAt.isEmpty ? "Never used" : "Used \(relativeTime(secret.lastUsedAt))")
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.ink3)
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            ArmedButton(title: "Delete", small: true) { Task { await delete(secret) } }
        }
        .padding(.leading, 14)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .frame(minHeight: 52)
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do {
            let list = try await model.client.listSecrets(botID: botID)
            seen.note(list.map(\.id))
            withAnimation(loaded ? Motion.state : nil) { secrets = list }
        } catch { await model.report(error) }
        loaded = true
    }

    private func delete(_ secret: Silo_V1_SecretMeta) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteSecret(botID: botID, id: secret.id)
            withAnimation(Motion.fold) { secrets.removeAll { $0.id == secret.id } }
            changed += 1
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
        VStack(spacing: 0) {
            SheetHead(title: "Add secret", onCancel: { dismiss() }) {
                Button("Add") { Task { await add() } }
                    .buttonStyle(.ink)
                    .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || value.isEmpty)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    SiloField(label: "Name", prompt: "GITHUB_TOKEN", text: $name, mono: true)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    VStack(alignment: .leading, spacing: 6) {
                        SiloField(label: "Value", prompt: "••••••••", text: $value, mono: true, secure: true)
                        Text("Stored on the Control Plane and never shown again.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                    }
                    if let failure { ErrorLine(text: failure) }
                }
                .padding(16)
            }
        }
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
