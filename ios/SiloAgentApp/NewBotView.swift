import Observation
import SiloClient
import SwiftUI

struct NewBotView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    @State private var name = ""
    @State private var description = ""
    @State private var crest = packCrest(
        shape: Int.random(in: 0..<SHAPE_COUNT),
        color: Int.random(in: 0..<CREST_COLORS.count)
    )
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack {
                        Spacer()
                        CrestView(index: crest, size: 84)
                        Spacer()
                    }
                    .listRowBackground(Color.clear)
                    CrestPicker(value: $crest)
                }

                Section("Name") {
                    TextField("Scout", text: $name)
                }

                Section("Description") {
                    TextField("What this machine is for", text: $description, axis: .vertical)
                        .lineLimit(2...5)
                }

                if let error {
                    Section {
                        Text(error).font(.footnote).foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("New Bot")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if busy {
                        ProgressView()
                    } else {
                        Button("Create") { create() }
                            .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                }
            }
        }
    }

    private func create() {
        let trimmed = name.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return }
        busy = true
        error = nil
        Task {
            do {
                let bot = try await model.createBot(
                    name: trimmed,
                    crest: crest,
                    description: description.trimmingCharacters(in: .whitespacesAndNewlines)
                )
                model.selectedBotID = bot.id
                dismiss()
            } catch {
                self.error = (error as? SiloError)?.errorDescription ?? error.localizedDescription
                busy = false
            }
        }
    }
}
