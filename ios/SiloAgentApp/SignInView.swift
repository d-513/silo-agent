import Observation
import SwiftUI

struct SignInView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        NavigationStack {
            Form {
                Section {
                    TextField("http://localhost:8080", text: $model.serverURL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                } header: {
                    Text("Control Plane")
                } footer: {
                    Text("The address of your Silo Control Plane, for example http://localhost:8080 or http://192.168.1.20:8080.")
                }

                Section("Account") {
                    TextField("Email", text: $model.email)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.emailAddress)
                        .textContentType(.username)
                    SecureField("Password", text: $model.password)
                        .textContentType(.password)
                        .onSubmit { Task { await model.signIn() } }
                }

                Section {
                    Button {
                        Task { await model.signIn() }
                    } label: {
                        HStack {
                            Spacer()
                            if model.signingIn {
                                ProgressView()
                            } else {
                                Text("Sign In").fontWeight(.semibold)
                            }
                            Spacer()
                        }
                    }
                    .disabled(model.signingIn || model.email.isEmpty || model.password.isEmpty)
                }

                if let error = model.signInError {
                    Section {
                        Text(error)
                            .font(.footnote)
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("Silo")
        }
    }
}
