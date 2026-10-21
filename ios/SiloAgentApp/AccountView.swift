import CoreImage.CIFilterBuiltins
import Observation
import SiloClient
import SwiftUI

/// Who is signed in, how they sign in (email, password, two-factor), where they are signed in, and
/// Sign Out. Admin stays on the web client. Opened as a sheet from the sidebar's foot.
struct AccountView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var modal = false
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
                            Text(model.user?.email ?? "Signed in").font(.silo(.headline))
                            if model.user?.admin == true {
                                Text("Administrator").font(.silo(.subheadline)).foregroundStyle(.secondary)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
                Section("Sign-in") {
                    NavigationLink { ChangeEmailPage() } label: {
                        LabeledContent("Email", value: model.user?.email ?? "")
                    }
                    if model.user?.hasPassword_p == true {
                        NavigationLink("Change Password") { ChangePasswordPage() }
                        NavigationLink { TwoFactorPage() } label: {
                            LabeledContent("Two-Factor", value: model.user?.totp == true ? "On" : "Off")
                        }
                    }
                    NavigationLink("Where You're Signed In") { SessionsPage() }
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
            .siloList()
            .navigationTitle("Account")
            .toolbar {
                if modal { ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } } }
            }
            .confirmationDialog("Sign out of Silo?", isPresented: $confirmSignOut, titleVisibility: .visible) {
                Button("Sign Out", role: .destructive) { Task { await model.signOut() } }
            }
        }
    }
}

private struct ChangeEmailPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var email = ""
    @State private var busy = false
    @State private var failure: String?

    var body: some View {
        Form {
            Section {
                TextField("Email", text: $email)
                    .textContentType(.emailAddress)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            } footer: {
                Text(failure ?? "You sign in with this address. Silo sends no mail, so it is changed at once.")
                    .foregroundStyle(failure == nil ? Color.secondary : Theme.vermilion)
            }
        }
        .navigationTitle("Email")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") { Task { await save() } }
                    .disabled(busy || email.trimmingCharacters(in: .whitespaces).isEmpty || email == model.user?.email)
            }
        }
        .onAppear { email = model.user?.email ?? "" }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            model.applyUser(try await model.client.changeEmail(email.trimmingCharacters(in: .whitespaces)))
            dismiss()
        } catch { failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription }
    }
}

private struct ChangePasswordPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var current = ""
    @State private var password = ""
    @State private var again = ""
    @State private var busy = false
    @State private var failure: String?

    var body: some View {
        Form {
            Section {
                SecureField("Current password", text: $current).textContentType(.password)
            }
            Section {
                SecureField("New password", text: $password).textContentType(.newPassword)
                SecureField("New password again", text: $again).textContentType(.newPassword)
            } footer: {
                if let failure {
                    Text(failure).foregroundStyle(Theme.vermilion)
                } else if !again.isEmpty && again != password {
                    Text("The two new passwords differ.").foregroundStyle(Theme.vermilion)
                } else {
                    Text("Your other sessions are signed out; this one stays.")
                }
            }
        }
        .navigationTitle("Change Password")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") { Task { await save() } }
                    .disabled(busy || current.isEmpty || password.isEmpty || password != again)
            }
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            model.applyUser(try await model.client.changePassword(current: current, password: password))
            dismiss()
        } catch { failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription }
    }
}

/// Two-factor for password sign-in: set up with an authenticator (Passwords on iOS takes the link
/// directly), turn off, or get new recovery codes. Each change asks for a current code.
private struct TwoFactorPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @State private var password = ""
    @State private var setup: Silo_V1_StartTOTPResponse?
    @State private var code = ""
    @State private var codes: [String] = []
    @State private var busy = false
    @State private var failure: String?
    @State private var confirmOff = false

    private var on: Bool { model.user?.totp == true }

    var body: some View {
        Form {
            if !codes.isEmpty {
                recoverySection
            } else if on {
                Section {
                    Label("Two-factor is on", systemImage: "checkmark.shield.fill").foregroundStyle(Theme.emerald)
                }
                Section {
                    TextField("6-digit code", text: $code)
                        .keyboardType(.numberPad)
                        .textContentType(.oneTimeCode)
                    Button("New Recovery Codes") { Task { await renewCodes() } }
                        .disabled(busy || code.count < 6)
                    Button("Turn Off Two-Factor", role: .destructive) { confirmOff = true }
                        .disabled(busy || code.count < 6)
                } footer: {
                    footer("Enter a code from your authenticator, or a recovery code, to change anything here.")
                }
            } else if let setup {
                Section {
                    if let image = qrImage(setup) {
                        Image(uiImage: image)
                            .interpolation(.none)
                            .resizable()
                            .scaledToFit()
                            .frame(maxWidth: 220)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 8)
                            .accessibilityLabel("Setup code")
                    }
                    if let url = URL(string: setup.url) {
                        Button { openURL(url) } label: { Label("Add to Passwords", systemImage: "key.viewfinder") }
                    }
                    LabeledContent("Key") {
                        Text(setup.secret).font(.siloMono(.caption)).textSelection(.enabled)
                    }
                } footer: {
                    Text("Scan the code with an authenticator, or add it to Passwords on this device, then enter the code it shows.")
                }
                Section {
                    TextField("6-digit code", text: $code)
                        .keyboardType(.numberPad)
                        .textContentType(.oneTimeCode)
                    Button("Turn On") { Task { await confirm() } }
                        .disabled(busy || code.count < 6)
                } footer: {
                    footer(nil)
                }
            } else {
                Section {
                    SecureField("Password", text: $password).textContentType(.password)
                    Button("Set Up Two-Factor") { Task { await start() } }
                        .disabled(busy || password.isEmpty)
                } footer: {
                    footer("After your password, Silo will also ask for a code from your authenticator. Confirm your password to begin.")
                }
            }
        }
        .navigationTitle("Two-Factor")
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("Turn off two-factor?", isPresented: $confirmOff, titleVisibility: .visible) {
            Button("Turn Off", role: .destructive) { Task { await disable() } }
        } message: {
            Text("Your password alone will sign you in.")
        }
    }

    private var recoverySection: some View {
        Section {
            ForEach(codes, id: \.self) { Text($0).font(.siloMono(.body)) }
            Button { UIPasteboard.general.string = codes.joined(separator: "\n") } label: { Label("Copy All", systemImage: "doc.on.doc") }
            ShareLink(item: codes.joined(separator: "\n")) { Label("Save…", systemImage: "square.and.arrow.up") }
            Button("Done") {
                codes = []
                code = ""
            }
        } header: {
            Text("Recovery codes")
        } footer: {
            Text("Each one signs you in once without your authenticator. Keep them somewhere safe: they are not shown again.")
        }
    }

    @ViewBuilder
    private func footer(_ text: String?) -> some View {
        if let failure { Text(failure).foregroundStyle(Theme.vermilion) } else if let text { Text(text) }
    }

    /// The server sends a PNG data URL; draw it again locally when it does not decode.
    private func qrImage(_ setup: Silo_V1_StartTOTPResponse) -> UIImage? {
        if let comma = setup.qr.firstIndex(of: ","), let data = Data(base64Encoded: String(setup.qr[setup.qr.index(after: comma)...])),
           let image = UIImage(data: data) {
            return image
        }
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(setup.url.utf8)
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)),
              let cg = CIContext().createCGImage(output, from: output.extent) else { return nil }
        return UIImage(cgImage: cg)
    }

    private func run(_ body: () async throws -> Void) async {
        busy = true
        failure = nil
        defer { busy = false }
        do { try await body() } catch { failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription }
    }

    private func start() async {
        await run {
            setup = try await model.client.startTOTP(password: password)
            password = ""
        }
    }

    private func confirm() async {
        await run {
            let out = try await model.client.confirmTOTP(code: code.trimmingCharacters(in: .whitespaces))
            if out.hasUser { model.applyUser(out.user) }
            codes = out.codes
            setup = nil
            code = ""
        }
    }

    private func disable() async {
        await run {
            model.applyUser(try await model.client.disableTOTP(code: code.trimmingCharacters(in: .whitespaces)))
            code = ""
        }
    }

    private func renewCodes() async {
        await run {
            codes = try await model.client.newRecoveryCodes(code: code.trimmingCharacters(in: .whitespaces)).codes
            code = ""
        }
    }
}

extension Silo_V1_Session: @retroactive Identifiable {}

/// Every browser and device signed in to this account; sign any of them out.
private struct SessionsPage: View {
    @Environment(AppModel.self) private var model
    @State private var sessions: [Silo_V1_Session] = []
    @State private var confirmOthers = false

    var body: some View {
        List {
            Section {
                ForEach(sessions) { session in
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(device(session.userAgent)).font(.silo(.headline))
                            if session.current {
                                Text("This device").font(.silo(.caption, .medium)).foregroundStyle(Theme.emerald)
                            }
                        }
                        Text([session.ip, session.method == "oidc" ? "Single sign-on" : "", "seen \(relativeTime(session.lastSeenAt))"]
                            .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.silo(.caption))
                            .foregroundStyle(.secondary)
                    }
                    .swipeActions {
                        if !session.current {
                            Button(role: .destructive) { Task { await revoke(session) } } label: { Label("Sign Out", systemImage: "rectangle.portrait.and.arrow.right") }
                        }
                    }
                }
            }
            if sessions.count > 1 {
                Section {
                    Button("Sign Out Everywhere Else", role: .destructive) { confirmOthers = true }
                }
            }
        }
        .navigationTitle("Sessions")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task { await load() }
        .confirmationDialog("Sign out every other session?", isPresented: $confirmOthers, titleVisibility: .visible) {
            Button("Sign Out Others", role: .destructive) { Task { await revokeOthers() } }
        }
    }

    /// A short name for a user agent: the app, or the browser and its platform.
    private func device(_ ua: String) -> String {
        if ua.isEmpty { return "Unknown device" }
        if ua.contains("CFNetwork") || ua.contains("Silo") { return "Silo for iOS" }
        // Order matters: Edge says Chrome too, and Chrome says Safari; Android says Linux.
        let browser = [("Edg/", "Edge"), ("Firefox/", "Firefox"), ("Chrome/", "Chrome"), ("Safari/", "Safari")]
            .first { ua.contains($0.0) }?.1 ?? "Browser"
        let platform = [("iPhone", "iPhone"), ("iPad", "iPad"), ("Android", "Android"), ("Mac OS X", "Mac"), ("Windows", "Windows"), ("Linux", "Linux")]
            .first { ua.contains($0.0) }?.1
        return platform.map { "\(browser) on \($0)" } ?? browser
    }

    private func load() async {
        do { sessions = try await model.client.listSessions() } catch { await model.report(error) }
    }

    private func revoke(_ session: Silo_V1_Session) async {
        do { sessions = try await model.client.revokeSession(id: session.id) } catch { await model.report(error) }
    }

    private func revokeOthers() async {
        do { sessions = try await model.client.revokeOtherSessions() } catch { await model.report(error) }
    }
}
