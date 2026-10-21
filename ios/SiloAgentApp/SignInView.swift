import Observation
import SiloClient
import SwiftUI

/// The signed-out app in two steps: connect to a server (checked before any password is typed),
/// then sign in to it. A remembered server starts on the second step.
struct SignInView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                AuthMark()
                    .padding(.bottom, 44)
                Group {
                    if model.serverURL.isEmpty {
                        ConnectStep()
                    } else if model.needsCode {
                        CodeStep()
                    } else {
                        AccountStep()
                    }
                }
                .transition(.asymmetric(insertion: .move(edge: .trailing).combined(with: .opacity),
                                        removal: .opacity))
            }
            .padding(.horizontal, 28)
            .padding(.top, 56)
            .padding(.bottom, 32)
            .frame(maxWidth: 460, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
        .scrollBounceBehavior(.basedOnSize)
        .background(Theme.canvas.ignoresSafeArea())
        .animation(.snappy(duration: 0.35), value: model.serverURL.isEmpty)
        .animation(.snappy(duration: 0.35), value: model.needsCode)
    }
}

// MARK: - Steps

/// Where the person's Silo is.
private struct ConnectStep: View {
    @Environment(AppModel.self) private var model
    @FocusState private var focused: Bool

    var body: some View {
        @Bindable var model = model
        VStack(alignment: .leading, spacing: 0) {
            CrestRow().padding(.bottom, 22)
            Text("Welcome to Silo")
                .font(.silo(32, .medium))
                .tracking(-0.6)
                .foregroundStyle(Theme.ink)
            Text("Your Bots live on your own server. Connect to it to talk to them, approve what they ask, and see what they made.")
                .font(.silo(15))
                .lineSpacing(3)
                .foregroundStyle(Theme.ink2)
                .padding(.top, 10)

            AuthField(label: "Server address", error: model.serverError != nil) {
                TextField("", text: $model.serverInput, prompt: Text("silo.example.com").foregroundStyle(Theme.ink3))
                    .font(.siloMono(16))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    .textContentType(.URL)
                    .submitLabel(.continue)
                    .focused($focused)
                    .onSubmit { Task { await model.connect() } }
            }
            .padding(.top, 32)

            if let error = model.serverError { AuthError(text: error).padding(.top, 10) }

            Button {
                Task { await model.connect() }
            } label: {
                HStack(spacing: 8) {
                    if model.checkingServer { ProgressView().tint(Theme.onInk) }
                    Text(model.checkingServer ? "Connecting…" : "Continue")
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.ink)
            .disabled(model.checkingServer || model.serverInput.trimmingCharacters(in: .whitespaces).isEmpty)
            .padding(.top, 20)

            Text("The address is whatever you open Silo at in a browser. Whoever set Silo up can tell you.")
                .font(.silo(13))
                .lineSpacing(2)
                .foregroundStyle(Theme.ink3)
                .padding(.top, 16)
        }
        .onAppear { focused = model.serverInput.isEmpty }
    }
}

/// Email and password, under the server they go to.
private struct AccountStep: View {
    @Environment(AppModel.self) private var model
    @FocusState private var field: Field?

    private enum Field { case email, password }

    private var passwordOff: Bool { model.serverOptions?.password == false }

    var body: some View {
        @Bindable var model = model
        VStack(alignment: .leading, spacing: 0) {
            ServerChip()
            Text("Sign in")
                .font(.silo(32, .medium))
                .tracking(-0.6)
                .foregroundStyle(Theme.ink)
                .padding(.top, 18)

            if passwordOff {
                Text("This server signs in with \(model.serverOptions?.oidcLabel.isEmpty == false ? model.serverOptions!.oidcLabel : "single sign-on") only, which the app cannot do yet. Sign in on the web client.")
                    .font(.silo(15))
                    .lineSpacing(3)
                    .foregroundStyle(Theme.ink2)
                    .padding(.top, 10)
            } else {
                AuthField(label: "Email") {
                    TextField("", text: $model.email, prompt: Text("you@example.com").foregroundStyle(Theme.ink3))
                        .font(.silo(16))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.emailAddress)
                        .textContentType(.username)
                        .submitLabel(.next)
                        .focused($field, equals: .email)
                        .onSubmit { field = .password }
                }
                .padding(.top, 28)

                AuthField(label: "Password") {
                    SecureField("", text: $model.password)
                        .font(.silo(16))
                        .textContentType(.password)
                        .submitLabel(.go)
                        .focused($field, equals: .password)
                        .onSubmit { Task { await model.signIn() } }
                }
                .padding(.top, 14)

                if let error = model.signInError { AuthError(text: error).padding(.top, 10) }

                Button {
                    Task { await model.signIn() }
                } label: {
                    HStack(spacing: 8) {
                        if model.signingIn { ProgressView().tint(Theme.onInk) }
                        Text(model.signingIn ? "Signing in…" : "Sign in")
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.ink)
                .disabled(model.signingIn || model.email.isEmpty || model.password.isEmpty)
                .padding(.top, 20)
            }
        }
        .task { await model.loadServerOptions() }
        .onAppear { field = model.email.isEmpty ? .email : .password }
    }
}

/// The second factor, once the password was right.
private struct CodeStep: View {
    @Environment(AppModel.self) private var model
    @FocusState private var focused: Bool

    var body: some View {
        @Bindable var model = model
        VStack(alignment: .leading, spacing: 0) {
            ServerChip()
            Text("Two-factor code")
                .font(.silo(32, .medium))
                .tracking(-0.6)
                .foregroundStyle(Theme.ink)
                .padding(.top, 18)
            Text("Enter the 6-digit code from your authenticator app, or one of your recovery codes.")
                .font(.silo(15))
                .lineSpacing(3)
                .foregroundStyle(Theme.ink2)
                .padding(.top, 10)

            AuthField(label: "Code") {
                TextField("", text: $model.code)
                    .font(.siloMono(20))
                    .tracking(2)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .textContentType(.oneTimeCode)
                    .submitLabel(.go)
                    .focused($focused)
                    .onSubmit { Task { await model.signIn() } }
            }
            .padding(.top, 28)

            if let error = model.signInError { AuthError(text: error).padding(.top, 10) }

            Button {
                Task { await model.signIn() }
            } label: {
                Text(model.signingIn ? "Signing in…" : "Sign in").frame(maxWidth: .infinity)
            }
            .buttonStyle(.ink)
            .disabled(model.signingIn || model.code.isEmpty)
            .padding(.top, 20)

            Button("Use another account") { model.cancelCode() }
                .buttonStyle(.quiet)
                .frame(maxWidth: .infinity)
                .padding(.top, 8)
        }
        .onAppear { focused = true }
    }
}

// MARK: - Pieces

/// The Silo glyph and name, as on the web's sign-in card.
struct AuthMark: View {
    var body: some View {
        HStack(spacing: 12) {
            SiloGlyph()
                .fill(Theme.ink, style: FillStyle(eoFill: true))
                .frame(width: 22, height: 22)
                .frame(width: 40, height: 40)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            Text("Silo Agent").font(.silo(15, .medium)).foregroundStyle(Theme.ink)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A few crests, the Bots' faces, settling in once as the app opens.
private struct CrestRow: View {
    private static let crests: [Int32] = [61, 54, 16, 34, 75] // cobalt hex, emerald cloud, wine circle, yellow squircle, pink pill
    @State private var shown = false

    var body: some View {
        HStack(spacing: 10) {
            ForEach(Array(Self.crests.enumerated()), id: \.offset) { index, crest in
                CrestView(index: crest, size: 34)
                    .opacity(shown ? 1 : 0)
                    .offset(y: shown ? 0 : 8)
                    .animation(.spring(duration: 0.5, bounce: 0.25).delay(0.08 * Double(index)), value: shown)
            }
        }
        .accessibilityHidden(true)
        .onAppear { shown = true }
    }
}

/// The server the next step talks to, with the way back to pick another.
private struct ServerChip: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        HStack(spacing: 8) {
            Circle().fill(Theme.lamp).frame(width: 7, height: 7)
            Text(serverLabel(model.serverURL))
                .font(.siloMono(13))
                .foregroundStyle(Theme.ink2)
                .lineLimit(1)
                .truncationMode(.middle)
            Button("Change") { model.changeServer() }
                .font(.silo(13, .medium))
                .foregroundStyle(Theme.cobalt)
                .buttonStyle(.plain)
        }
        .padding(.horizontal, 12)
        .frame(height: 32)
        .background(Theme.well, in: Capsule())
    }
}

/// A labelled field: the label above in ink-3, the field on surface with a hairline.
private struct AuthField<Field: View>: View {
    let label: String
    var error = false
    @ViewBuilder let field: () -> Field

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(label).font(.silo(12.5, .medium)).foregroundStyle(Theme.ink3)
            field()
                .foregroundStyle(Theme.ink)
                .tint(Theme.cobalt)
                .padding(.horizontal, 14)
                .frame(height: 50)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                        .stroke(error ? Theme.vermilion : Theme.lineStrong)
                }
        }
    }
}

private struct AuthError: View {
    let text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle().fill(Theme.vermilion).frame(width: 6, height: 6).alignmentGuide(.firstTextBaseline) { $0[.bottom] + 2 }
            Text(text).font(.silo(13)).foregroundStyle(Theme.vermilion)
        }
        .accessibilityElement(children: .combine)
    }
}

/// The Silo mark (`web/src/SiloMark.tsx`): a silo with a slot, on a 32-unit grid.
struct SiloGlyph: Shape {
    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 32
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        var path = Path()
        path.move(to: p(6, 12))
        path.addArc(center: p(16, 12), radius: 10 * s, startAngle: .degrees(180), endAngle: .degrees(0), clockwise: false)
        path.addLine(to: p(26, 26))
        path.addQuadCurve(to: p(24, 28), control: p(26, 28))
        path.addLine(to: p(8, 28))
        path.addQuadCurve(to: p(6, 26), control: p(6, 28))
        path.closeSubpath()
        path.addRoundedRect(in: CGRect(origin: p(11.8, 11.2), size: CGSize(width: 8.4 * s, height: 1.6 * s)),
                            cornerSize: CGSize(width: 0.8 * s, height: 0.8 * s))
        return path
    }
}
