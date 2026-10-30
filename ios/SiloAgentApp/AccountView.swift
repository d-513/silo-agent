import CoreImage.CIFilterBuiltins
import Observation
import SiloClient
import SwiftUI
import UIKit

/// Who is signed in, how they sign in (email, password, two-factor), where they are signed in, and
/// Sign out. Admin stays on the web client. As on the web it is one page of panels: a row opens in
/// place rather than pushing a page of its own. Opened as a sheet from the sidebar's foot (`modal`,
/// whose caller applies `.siloSheet()`), or pushed as a page.
struct AccountView: View {
    @Environment(\.dismiss) private var dismiss
    var modal = false

    var body: some View {
        if modal {
            VStack(spacing: 0) {
                SheetHead(title: "Account", cancel: "Done", onCancel: { dismiss() })
                AccountPanels().background(Theme.canvas)
            }
        } else {
            SiloPage(title: "Account") { AccountPanels() }
        }
    }
}

private struct AccountPanels: View {
    @Environment(AppModel.self) private var model
    @State private var sessions: [Silo_V1_Session]?

    var body: some View {
        PageScroll {
            Panel(padded: true) { identity }
            PanelSection(title: "Sign-in") {
                EmailFold()
                if model.user?.hasPassword_p == true {
                    PasswordFold()
                    TwoFactorFold()
                }
                SessionsFold(sessions: $sessions, load: load)
            }
            PanelSection(footer: "Admin settings, libraries and the desktop are on the web client.") {
                PanelRow(title: "Control plane", detail: model.serverURL, mono: true)
            }
            ArmedButton(title: "Sign out", armedTitle: "Tap again to sign out", symbol: "rectangle.portrait.and.arrow.right", wide: true) {
                Task { await model.signOut() }
            }
        }
        .refreshable { await load() }
    }

    private var identity: some View {
        let email = model.user?.email ?? ""
        return HStack(spacing: 14) {
            Text(personInitials(email))
                .font(.silo(16, .semibold))
                .foregroundStyle(Theme.onInk)
                .frame(width: 44, height: 44)
                .background(Theme.ink, in: Circle())
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(email.isEmpty ? "Signed in" : email)
                    .font(.silo(15, .semibold))
                    .tracking(-0.15)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(2)
                if model.user?.admin == true {
                    Text("Administrator").font(.silo(12.5)).foregroundStyle(Theme.ink3)
                }
            }
        }
    }

    private func load() async {
        do { sessions = try await model.client.listSessions() } catch { await model.report(error) }
    }
}

// MARK: - A row that opens in place

/// A panel's row with what it holds folded under it. The chevron turns as it opens.
private struct AccountFold<Content: View>: View {
    var title: String
    var detail: String?
    /// A word at the row's end.
    var value: String?
    /// What the open body sits on: a `well`, unless its rows are the panel's own.
    var ground: Color = Theme.well
    @Binding var open: Bool
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            PanelRow(title: title, detail: detail, action: { withAnimation(Motion.fold) { open.toggle() } }) {
                if let value {
                    Text(value).font(.silo(13)).foregroundStyle(Theme.ink3)
                }
                Image(systemName: "chevron.right")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.ink3)
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
            .accessibilityValue(open ? "Open" : "Closed")
            if open {
                Theme.line.frame(height: 1).transition(.opacity)
                VStack(alignment: .leading, spacing: 12) { content }
                    .padding(14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background { Rectangle().fill(ground) }
                    .transition(.opacity)
            }
        }
        .sensoryFeedback(.selection, trigger: open)
    }
}

/// A quiet sentence under a field.
private struct Note: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text).font(.silo(12.5)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
    }
}

private func describe(_ error: Error) -> String {
    (error as? SiloError)?.errorDescription ?? error.localizedDescription
}

// MARK: - Email

private struct EmailFold: View {
    @Environment(AppModel.self) private var model
    @State private var open = false
    @State private var email = ""
    @State private var failure: String?

    private var trimmed: String { email.trimmingCharacters(in: .whitespaces) }

    var body: some View {
        AccountFold(title: "Email", detail: model.user?.email, open: $open) {
            SiloField(label: "Email", text: $email, error: failure)
                .textContentType(.emailAddress)
                .keyboardType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Note("You sign in with this address. Silo sends no mail, so it is changed at once.")
            SaveButton(enabled: !trimmed.isEmpty && email != model.user?.email) { await save() }
        }
        .onChange(of: open, initial: true) { _, on in
            if on {
                email = model.user?.email ?? ""
                failure = nil
            }
        }
    }

    private func save() async -> Bool {
        failure = nil
        do {
            model.applyUser(try await model.client.changeEmail(trimmed))
            email = model.user?.email ?? trimmed
            return true
        } catch {
            failure = describe(error)
            return false
        }
    }
}

// MARK: - Password

private struct PasswordFold: View {
    @Environment(AppModel.self) private var model
    @State private var open = false
    @State private var current = ""
    @State private var password = ""
    @State private var again = ""
    @State private var failure: String?

    private var differ: Bool { !again.isEmpty && again != password }

    var body: some View {
        AccountFold(title: "Change password", open: $open) {
            SiloField(label: "Current password", text: $current, secure: true)
                .textContentType(.password)
            SiloField(label: "New password", text: $password, secure: true)
                .textContentType(.newPassword)
            SiloField(label: "New password again", text: $again, secure: true, error: differ ? "The two new passwords differ." : failure)
                .textContentType(.newPassword)
            Note("Your other sessions are signed out; this one stays.")
            SaveButton(enabled: !current.isEmpty && !password.isEmpty && password == again) { await save() }
        }
    }

    private func save() async -> Bool {
        failure = nil
        do {
            model.applyUser(try await model.client.changePassword(current: current, password: password))
            current = ""
            password = ""
            again = ""
            return true
        } catch {
            failure = describe(error)
            return false
        }
    }
}

// MARK: - Two-factor

/// Two-factor for password sign-in: set up with an authenticator (Passwords on iOS takes the link
/// directly), turn off, or get new recovery codes. Each change asks for a current code. What it is in
/// the middle of lives here, above the fold, so closing the row never loses a code shown only once.
private struct TwoFactorFold: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @State private var open = false
    @State private var password = ""
    @State private var setup: Silo_V1_StartTOTPResponse?
    @State private var code = ""
    @State private var codes: [String] = []
    @State private var busy = false
    @State private var failure: String?

    private var on: Bool { model.user?.totp == true }
    private var ready: Bool { !busy && code.count >= 6 }

    var body: some View {
        AccountFold(title: "Two-factor", value: on ? "On" : "Off", open: $open) {
            if !codes.isEmpty {
                recovery.transition(.opacity)
            } else if on {
                turnedOn.transition(.opacity)
            } else if let setup {
                scanning(setup).transition(.opacity)
            } else {
                turnedOff.transition(.opacity)
            }
        }
        .animation(Motion.state, value: failure)
        .sensoryFeedback(.success, trigger: on)
        .sensoryFeedback(.success, trigger: codes.isEmpty) { _, none in !none }
    }

    private var turnedOff: some View {
        VStack(alignment: .leading, spacing: 12) {
            SiloField(label: "Password", text: $password, secure: true, error: failure)
                .textContentType(.password)
            Note("After your password, Silo will also ask for a code from your authenticator. Confirm your password to begin.")
            Button { Task { await start() } } label: { working("Set up two-factor") }
                .buttonStyle(.ink)
                .disabled(busy || password.isEmpty)
        }
    }

    private func scanning(_ setup: Silo_V1_StartTOTPResponse) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Note("Scan the code with an authenticator, or add it to Passwords on this device, then enter the code it shows.")
            if let image = qrImage(setup) {
                let shape = RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
                // A QR code is black on white whatever the theme, or it does not scan.
                Image(uiImage: image)
                    .interpolation(.none)
                    .resizable()
                    .scaledToFit()
                    .frame(width: 168, height: 168)
                    .clipShape(shape)
                    .overlay(shape.strokeBorder(Theme.lineStrong, lineWidth: 1))
                    .frame(maxWidth: .infinity)
                    .accessibilityLabel("Setup code")
            }
            if let url = URL(string: setup.url) {
                Button { openURL(url) } label: {
                    HStack(spacing: 8) {
                        Image(systemName: "key.viewfinder").font(.system(size: 14))
                        Text("Add to Passwords")
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.quiet)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text("Key").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                HStack(spacing: 4) {
                    Text(setup.secret)
                        .font(.siloMono(12.5))
                        .foregroundStyle(Theme.ink)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    CopyButton(text: setup.secret)
                }
            }
            codeField
            HStack(spacing: 8) {
                Button { Task { await confirm() } } label: { working("Turn on") }
                    .buttonStyle(.ink)
                    .disabled(!ready)
                Button("Cancel") {
                    self.setup = nil
                    code = ""
                    failure = nil
                }
                .buttonStyle(.ghost)
            }
        }
    }

    private var turnedOn: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: "checkmark.shield").font(.system(size: 14))
                Text("Two-factor is on").font(.silo(13.5, .medium))
            }
            .foregroundStyle(Theme.emerald)
            codeField
            Note("Enter a code from your authenticator, or a recovery code, to change anything here. With two-factor off, your password alone will sign you in.")
            HStack(spacing: 8) {
                Button { Task { await renewCodes() } } label: { working("New recovery codes") }
                    .buttonStyle(.quiet)
                    .controlSize(.small)
                    .disabled(!ready)
                ArmedButton(title: "Turn off", armedTitle: "Tap again to turn off", small: true) {
                    Task { await disable() }
                }
                .disabled(!ready)
                .opacity(ready ? 1 : 0.5)
            }
        }
    }

    private var recovery: some View {
        let all = codes.joined(separator: "\n")
        return VStack(alignment: .leading, spacing: 12) {
            Text("Recovery codes").font(.silo(13.5, .medium)).foregroundStyle(Theme.ink)
            Note("Each one signs you in once without your authenticator. Keep them somewhere safe: they are not shown again.")
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], alignment: .leading, spacing: 6) {
                ForEach(codes, id: \.self) { Text($0).font(.siloMono(13)).foregroundStyle(Theme.ink).textSelection(.enabled) }
            }
            .padding(12)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            HStack(spacing: 8) {
                Button("Done") {
                    withAnimation(Motion.state) {
                        codes = []
                        code = ""
                    }
                }
                .buttonStyle(.ink)
                .controlSize(.small)
                CopyAllButton(text: all)
                ShareLink(item: all) {
                    HStack(spacing: 6) {
                        Image(systemName: "square.and.arrow.up").font(.system(size: 13))
                        Text("Save…")
                    }
                }
                .buttonStyle(.quiet)
                .controlSize(.small)
            }
        }
    }

    private var codeField: some View {
        SiloField(label: "Code", prompt: "6-digit code", text: $code, mono: true, error: failure)
            .keyboardType(.numberPad)
            .textContentType(.oneTimeCode)
    }

    /// A button's word, with the small wait in front of it.
    private func working(_ title: String) -> some View {
        HStack(spacing: 8) {
            if busy { Spinner(size: 13, color: Theme.ink3) }
            Text(title)
        }
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
        do { try await body() } catch { failure = describe(error) }
    }

    private func start() async {
        await run {
            let out = try await model.client.startTOTP(password: password)
            withAnimation(Motion.state) {
                setup = out
                password = ""
            }
        }
    }

    private func confirm() async {
        await run {
            let out = try await model.client.confirmTOTP(code: code.trimmingCharacters(in: .whitespaces))
            withAnimation(Motion.state) {
                if out.hasUser { model.applyUser(out.user) }
                codes = out.codes
                setup = nil
                code = ""
            }
        }
    }

    private func disable() async {
        await run {
            let user = try await model.client.disableTOTP(code: code.trimmingCharacters(in: .whitespaces))
            withAnimation(Motion.state) {
                model.applyUser(user)
                code = ""
            }
        }
    }

    private func renewCodes() async {
        await run {
            let out = try await model.client.newRecoveryCodes(code: code.trimmingCharacters(in: .whitespaces))
            withAnimation(Motion.state) {
                codes = out.codes
                code = ""
            }
        }
    }
}

/// Copy with its word: the glyph morphs into an emerald check for 1.5s, then back.
private struct CopyAllButton: View {
    let text: String
    @State private var copied = false

    var body: some View {
        Button {
            UIPasteboard.general.string = text
            withAnimation(Motion.settle(0.26)) { copied = true }
            Task {
                try? await Task.sleep(for: .seconds(1.5))
                withAnimation(Motion.state) { copied = false }
            }
        } label: {
            HStack(spacing: 6) {
                ZStack {
                    Image(systemName: "doc.on.doc").opacity(copied ? 0 : 1).rotationEffect(.degrees(copied ? 30 : 0)).scaleEffect(copied ? 0.5 : 1)
                    Image(systemName: "checkmark").foregroundStyle(Theme.emerald).opacity(copied ? 1 : 0).rotationEffect(.degrees(copied ? 0 : -30)).scaleEffect(copied ? 1 : 0.5)
                }
                .font(.system(size: 13))
                Text("Copy all")
            }
        }
        .buttonStyle(.quiet)
        .controlSize(.small)
        .sensoryFeedback(.success, trigger: copied) { _, on in on }
    }
}

// MARK: - Sessions

extension Silo_V1_Session: @retroactive Identifiable {}

/// Every browser and device signed in to this account; sign any of them out. It loads as it opens.
private struct SessionsFold: View {
    @Environment(AppModel.self) private var model
    @Binding var sessions: [Silo_V1_Session]?
    let load: () async -> Void
    @State private var open = false

    var body: some View {
        AccountFold(title: "Where you're signed in", ground: Theme.surface, open: $open) {
            if let sessions {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(sessions.enumerated()), id: \.element.id) { index, session in
                        if index > 0 { Theme.line.frame(height: 1) }
                        row(session).transition(.opacity)
                    }
                }
                if sessions.count > 1 {
                    ArmedButton(title: "Sign out everywhere else", armedTitle: "Tap again to sign them out", small: true, wide: true) {
                        Task { await revokeOthers() }
                    }
                    .transition(.opacity)
                }
            } else {
                SkeletonRows(count: 2, height: 48)
            }
        }
        .animation(Motion.state, value: sessions?.count)
        .onChange(of: open) { _, on in
            if on { Task { await load() } }
        }
        .sensoryFeedback(.success, trigger: sessions?.count) { old, new in (old ?? 0) > (new ?? 0) }
    }

    private func row(_ session: Silo_V1_Session) -> some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(device(session.userAgent)).font(.silo(14, .medium)).foregroundStyle(Theme.ink)
                Text([session.ip, session.method == "oidc" ? "Single sign-on" : "", session.current ? "Active now" : "seen \(relativeTime(session.lastSeenAt))"]
                    .filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if session.current {
                Chip(text: "This device")
            } else {
                ArmedButton(title: "Sign out", small: true) { Task { await revoke(session) } }
            }
        }
        .padding(.vertical, 10)
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

    private func revoke(_ session: Silo_V1_Session) async {
        do { sessions = try await model.client.revokeSession(id: session.id) } catch { await model.report(error) }
    }

    private func revokeOthers() async {
        do { sessions = try await model.client.revokeOtherSessions() } catch { await model.report(error) }
    }
}
