import Observation
import SiloClient
import SwiftUI

/// The signed-out app, one instrument in two steps: connect to a server (checked before any password
/// is typed), then sign in to it, with the second factor after a right password. A remembered server
/// starts on the second step. Nothing is swapped: the title and the button stay and change their
/// words, the address field becomes the band of the server it reached, and the next step's fields
/// unfold between them.
struct SignInView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.accessibilityReduceMotion) private var still
    @FocusState private var focus: Field?

    private enum Step: Hashable { case connect, account, code }
    private enum Field: Hashable { case server, email, password, code }

    private var step: Step {
        if model.serverURL.isEmpty { return .connect }
        return model.needsCode ? .code : .account
    }

    private var passwordOff: Bool { step == .account && model.serverOptions?.password == false }

    private var title: String {
        switch step {
        case .connect: "Welcome to Silo"
        case .account: "Sign in"
        case .code: "Two-factor code"
        }
    }

    private var lede: String? {
        switch step {
        case .connect: "Your Bots live on your own server. Connect to it to talk to them, approve what they ask, and see what they made."
        case .code: "Enter the 6-digit code from your authenticator app, or one of your recovery codes."
        case .account:
            passwordOff
                ? "This server signs in with \(model.serverOptions?.oidcLabel.isEmpty == false ? model.serverOptions!.oidcLabel : "single sign-on") only, which the app cannot do yet. Sign in on the web client."
                : nil
        }
    }

    private var busy: Bool { step == .connect ? model.checkingServer : model.signingIn }

    /// The button's word. It is the same button from the first step to the last.
    private var word: String {
        if step == .connect { return model.checkingServer ? "Connecting…" : "Continue" }
        return model.signingIn ? "Signing in…" : "Sign in"
    }

    /// Whether there is something to send. A press while busy is the model's to ignore, so the button
    /// keeps its ink, and its spinner, through the wait.
    private var ready: Bool {
        switch step {
        case .connect: !model.serverInput.trimmingCharacters(in: .whitespaces).isEmpty
        case .account: !model.email.isEmpty && !model.password.isEmpty
        case .code: !model.code.isEmpty
        }
    }

    private var fold: Animation { still ? Motion.quiet(0.12) : Motion.fold }
    private var arrive: AnyTransition { still ? .opacity : .rise }

    var body: some View {
        GeometryReader { box in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AuthMark()
                    StepRail(at: step == .connect ? 0 : 1)
                        .padding(.top, 20)
                    if step == .connect {
                        CrestRow().padding(.top, 32).transition(.opacity)
                    }
                    head.padding(.top, step == .connect ? 20 : 32)
                    fields.padding(.top, 28)
                    if !passwordOff {
                        submit.padding(.top, 20).transition(.opacity)
                    }
                    foot
                }
                .padding(.horizontal, 28)
                .padding(.vertical, 28)
                .frame(maxWidth: 456, alignment: .leading)
                .frame(maxWidth: .infinity, minHeight: box.size.height)
            }
            .scrollDismissesKeyboard(.interactively)
            .scrollBounceBehavior(.basedOnSize)
        }
        .background(Theme.canvas.ignoresSafeArea())
        .animation(fold, value: step)
        .animation(fold, value: passwordOff)
        .animation(Motion.state, value: model.serverError)
        .animation(Motion.state, value: model.signInError)
        .task(id: step) {
            switch step {
            case .connect:
                focus = model.serverInput.isEmpty ? .server : nil
            case .account:
                focus = model.email.isEmpty ? .email : .password
                await model.loadServerOptions()
            case .code:
                focus = .code
            }
        }
        .sensoryFeedback(.success, trigger: step) { _, new in new != .connect }
        .sensoryFeedback(.error, trigger: model.serverError) { _, new in new != nil }
        .sensoryFeedback(.error, trigger: model.signInError) { _, new in new != nil }
    }

    /// The title stays where it is and changes its word; the sentence under it follows.
    private var head: some View {
        VStack(alignment: .leading, spacing: 12) {
            ZStack(alignment: .topLeading) {
                Text(title)
                    .font(.silo(40, .medium))
                    .tracking(-0.8)
                    .lineHeight(.multiple(factor: 48 / 52))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .id(title)
                    .transition(wordChange)
            }
            if let lede {
                ZStack(alignment: .topLeading) {
                    Text(lede)
                        .font(.silo(15))
                        .lineHeight(.multiple(factor: 24 / 19.5))
                        .foregroundStyle(Theme.ink2)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .id(lede)
                        .transition(.opacity)
                }
                .transition(.opacity)
            }
        }
    }

    @ViewBuilder
    private var fields: some View {
        @Bindable var model = model
        VStack(alignment: .leading, spacing: 14) {
            // The field and the band share a place: what you typed becomes what you are connected to.
            ZStack(alignment: .bottom) {
                if step == .connect {
                    SiloField(label: "Server address", prompt: "silo.example.com", text: $model.serverInput, mono: true, error: model.serverError)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .submitLabel(.continue)
                        .focused($focus, equals: .server)
                        .onSubmit { Task { await model.connect() } }
                        .transition(.opacity)
                } else {
                    ServerBand().transition(.opacity)
                }
            }
            if step == .account && !passwordOff {
                SiloField(label: "Email", prompt: "you@example.com", text: $model.email)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.emailAddress)
                    .textContentType(.username)
                    .submitLabel(.next)
                    .focused($focus, equals: .email)
                    .onSubmit { focus = .password }
                    .transition(arrive)
                SiloField(label: "Password", text: $model.password, secure: true, error: model.signInError)
                    .textContentType(.password)
                    .submitLabel(.go)
                    .focused($focus, equals: .password)
                    .onSubmit { Task { await model.signIn() } }
                    .transition(arrive)
            }
            if step == .code {
                SiloField(label: "Code", text: $model.code, mono: true, error: model.signInError)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .textContentType(.oneTimeCode)
                    .submitLabel(.go)
                    .focused($focus, equals: .code)
                    .onSubmit { Task { await model.signIn() } }
                    .transition(arrive)
            }
        }
    }

    private var submit: some View {
        Button {
            Task {
                if step == .connect { await model.connect() } else { await model.signIn() }
            }
        } label: {
            HStack(spacing: 8) {
                if busy {
                    Spinner(size: 14, color: Theme.onInk)
                        .transition(.scale(scale: 0.6).combined(with: .opacity))
                }
                ZStack {
                    Text(word)
                        .id(word)
                        .transition(wordChange)
                }
            }
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.ink)
        .disabled(!ready)
        .animation(Motion.state, value: word)
        .animation(Motion.state, value: ready)
    }

    @ViewBuilder
    private var foot: some View {
        switch step {
        case .connect:
            Text("The address is whatever you open Silo at in a browser. Whoever set Silo up can tell you.")
                .font(.silo(13))
                .lineHeight(.multiple(factor: 20 / 16.9))
                .foregroundStyle(Theme.ink3)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 14)
                .transition(.opacity)
        case .code:
            Button {
                model.cancelCode()
            } label: {
                Text("Use another account").frame(maxWidth: .infinity)
            }
            .buttonStyle(.ghost)
            .padding(.top, 8)
            .transition(arrive)
        case .account:
            EmptyView()
        }
    }

    /// A word changing in place: the old one fades out, the new one comes up 4pt.
    private var wordChange: AnyTransition {
        .asymmetric(insertion: still ? .opacity : .opacity.combined(with: .offset(y: 4)), removal: .opacity)
    }
}

// MARK: - Pieces

/// The skrzat and the name, as on the web's sign-in card.
struct AuthMark: View {
    var body: some View {
        HStack(spacing: 12) {
            Image("silo-mark")
                .resizable()
                .interpolation(.high)
                .frame(width: 44, height: 44)
            Text("Silo Agent").font(.silo(15, .medium)).foregroundStyle(Theme.ink)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Where you are in the two steps: a hairline with the tabs' 2pt cobalt bar sliding along it.
private struct StepRail: View {
    let at: Int
    var count = 2

    var body: some View {
        GeometryReader { box in
            let width = box.size.width / CGFloat(count)
            ZStack(alignment: .leading) {
                Theme.line.frame(height: 1)
                Theme.cobalt.frame(width: width, height: 2).offset(x: width * CGFloat(at))
            }
        }
        .frame(height: 2)
        .accessibilityHidden(true)
    }
}

/// A few crests, the Bots' faces, settling in once as the app opens.
private struct CrestRow: View {
    private static let crests: [Int32] = [61, 54, 16, 34, 75] // cobalt hex, emerald cloud, wine circle, yellow squircle, pink pill
    @State private var shown = false
    @Environment(\.accessibilityReduceMotion) private var still

    var body: some View {
        HStack(spacing: 10) {
            ForEach(Array(Self.crests.enumerated()), id: \.offset) { index, crest in
                CrestView(index: crest, size: 34)
                    .opacity(shown ? 1 : 0)
                    .offset(y: shown || still ? 0 : 8)
                    .animation(Motion.arrival.delay(0.06 * Double(index)), value: shown)
            }
        }
        .accessibilityHidden(true)
        .onAppear { shown = true }
    }
}

/// The server the next step talks to, with the way back to pick another.
private struct ServerBand: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        HStack(spacing: 10) {
            Lamp(status: .online)
            Text(serverLabel(model.serverURL))
                .font(.siloMono(13))
                .foregroundStyle(Theme.ink2)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button {
                model.changeServer()
            } label: {
                Text("Change")
                    .font(.silo(13, .medium))
                    .foregroundStyle(Theme.cobalt)
                    .padding(.horizontal, 8)
                    .frame(minHeight: 40)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.press)
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .frame(minHeight: 44)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}
