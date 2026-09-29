import Observation
import SiloClient
import SwiftUI

/// Who is signed in, which Control Plane this is, and Sign Out. Admin stays on the web client.
struct AccountView: View {
    @Environment(AppModel.self) private var model
    @State private var confirmSignOut = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack(spacing: 14) {
                        Image(systemName: "person.crop.circle.fill")
                            .font(.system(size: 44))
                            .foregroundStyle(.secondary)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.user?.email ?? "Signed in").font(.headline)
                            if model.user?.admin == true {
                                Text("Administrator").font(.subheadline).foregroundStyle(.secondary)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
                Section {
                    LabeledContent("Control Plane", value: model.serverURL)
                } footer: {
                    Text("Admin settings, libraries and the desktop are on the web client.")
                }
                Section {
                    Button("Sign Out", role: .destructive) { confirmSignOut = true }
                }
            }
            .navigationTitle("Account")
            .confirmationDialog("Sign out of Silo?", isPresented: $confirmSignOut, titleVisibility: .visible) {
                Button("Sign Out", role: .destructive) { Task { await model.signOut() } }
            }
        }
    }
}
