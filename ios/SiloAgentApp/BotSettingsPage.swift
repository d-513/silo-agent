import SiloClient
import SwiftUI

/// Name, description, SOUL, and the Bot's default model. A chat's own override still wins.
struct BotSettingsPage: View {
    @Environment(AppModel.self) private var model
    @State private var name = ""
    @State private var description = ""
    @State private var soul = ""
    @State private var botModel = ""
    @State private var saving = false
    @State private var confirmDelete = false

    var body: some View {
        Form {
            Section("Name") { TextField("Name", text: $name) }
            Section("Description") { TextField("What is this Bot for?", text: $description, axis: .vertical) }
            Section {
                TextEditor(text: $soul).frame(minHeight: 140)
            } header: {
                Text("SOUL")
            } footer: {
                Text("Who the Bot is and how it behaves. Injected into every run.")
            }
            Section {
                Picker("Default model", selection: $botModel) {
                    Text("Operator default").tag("")
                    ForEach(model.models, id: \.id) { Text($0.label.isEmpty ? $0.id : $0.label).tag($0.id) }
                }
            } footer: {
                Text("A chat's own model choice overrides this.")
            }
            Section {
                Button("Delete Bot", role: .destructive) { confirmDelete = true }
            } footer: {
                Text("Removes the machine, the Bot, and its workspace.")
            }
        }
        .toolbar {
            if dirty {
                ToolbarItem(placement: .confirmationAction) {
                    Button(saving ? "Saving…" : "Save") { Task { await save() } }
                        .disabled(saving || name.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
        .task(id: model.selectedBotID) { load() }
        .confirmationDialog("Delete this Bot?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete Bot and its data", role: .destructive) { Task { await delete() } }
        } message: {
            Text("This cannot be undone.")
        }
    }

    private var dirty: Bool {
        guard let bot = model.selectedBot else { return false }
        return name != bot.name || description != bot.description_p || soul != bot.soul || botModel != bot.model
    }

    private func load() {
        guard let bot = model.selectedBot else { return }
        name = bot.name
        description = bot.description_p
        soul = bot.soul
        botModel = bot.model
    }

    private func save() async {
        guard let bot = model.selectedBot else { return }
        saving = true
        defer { saving = false }
        do {
            model.applyBot(try await model.client.updateBot(bot, name: name, description: description, soul: soul, model: botModel))
        } catch { await model.report(error) }
    }

    private func delete() async {
        guard let id = model.selectedBotID else { return }
        do {
            try await model.client.deleteBot(id)
            await model.botDeleted(id)
        } catch { await model.report(error) }
    }
}
