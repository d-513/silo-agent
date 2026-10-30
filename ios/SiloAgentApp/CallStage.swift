import SiloCall
import SiloClient
import SwiftUI

/// A call with the screen to itself: the Bot's face with rings that carry the voice, what it is
/// saying in the reply serif, what it heard from you, and a dock of controls. The thread is still
/// written underneath; Show chat folds the call into the bar. Web: `web/src/call/CallStage.tsx`.
struct CallStage: View {
    @Environment(AppModel.self) private var model
    let call: CallModel

    var body: some View {
        if let s = call.snap, let who = call.who {
            let bot = model.bot(who.id)
            let doing = bot.flatMap { chatDoing($0, s.chatID) }
            VStack(spacing: 0) {
                head(s, who)
                VStack(spacing: 0) {
                    Spacer(minLength: 8)
                    CallFace(s: s, crest: who.crest, name: who.name) { call.interrupt() }
                    Text(who.name).font(.silo(22, .medium)).foregroundStyle(Theme.ink).padding(.top, 2)
                    words(s, who, doing)
                    CallCaptions(s: s, name: who.name)
                    Spacer(minLength: 8)
                }
                .padding(.horizontal, 24)
                .frame(maxWidth: .infinity)
                CallDock(call: call, s: s)
            }
            .background(Theme.canvas.ignoresSafeArea())
            .sensoryFeedback(.impact(weight: .medium), trigger: s.state == .speaking)
            .sensoryFeedback(.success, trigger: s.callID.isEmpty)
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Call with \(who.name)")
        }
    }

    private func head(_ s: CallSnapshot, _ who: CallModel.Who) -> some View {
        let ended = s.state == .ended
        return HStack(spacing: 8) {
            if !ended { Lamp(status: .online) }
            Text(ended ? "Call ended" : "On a call · \(place(s))").font(.silo(13)).foregroundStyle(Theme.ink3).lineLimit(1)
            if !ended { CallClock(since: s.startedAt) }
            Spacer(minLength: 8)
            Button {
                withAnimation(Motion.quiet(0.32)) { call.expanded = false }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "text.bubble").font(.system(size: 13))
                    Text("Show chat")
                }
            }
            .buttonStyle(.quiet)
            .controlSize(.small)
        }
        .padding(.horizontal, 16)
        .frame(height: 48)
    }

    private func place(_ s: CallSnapshot) -> String {
        guard let chat = model.chats.first(where: { $0.id == s.chatID }) else { return "Main chat" }
        return chat.main ? "Main chat" : chat.title.isEmpty ? "New chat" : chat.title
    }

    @ViewBuilder
    private func words(_ s: CallSnapshot, _ who: CallModel.Who, _ doing: Silo_V1_BotActivity?) -> some View {
        switch s.state {
        case .needsYou:
            HStack(spacing: 8) {
                Lamp(status: .needsYou)
                Text(stateWords(s, botName: who.name)).font(.silo(13, .medium)).foregroundStyle(Theme.vermilion)
            }
            .padding(.horizontal, 12)
            .frame(height: 32)
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous).stroke(Theme.vermilion))
            .padding(.top, 6)
        case .working:
            Button {
                withAnimation(Motion.quiet(0.32)) { call.expanded = false }
            } label: {
                HStack(spacing: 8) {
                    Lamp(status: .working)
                    Text(callWork(s, doing: doing.map(Doing.init))).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(1)
                    if !progress(doing).isEmpty {
                        Text(progress(doing)).font(.siloMono(11.5)).foregroundStyle(Theme.ink3)
                    }
                }
                .padding(.horizontal, 12)
                .frame(height: 32)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            }
            .buttonStyle(.press)
            .padding(.top, 6)
        default:
            Text(stateWords(s, botName: who.name))
                .font(.silo(13.5))
                .foregroundStyle(s.state == .ended && !s.error.isEmpty ? Theme.vermilion : Theme.ink3)
                .multilineTextAlignment(.center)
                .padding(.top, 4)
                .id(stateWords(s, botName: who.name))
                .transition(.opacity)
        }
    }
}

/// The Bot's crest inside three rings: cobalt while you speak, ink while the Bot does, swelling with
/// the voice; one quiet ring otherwise, breathing while it thinks. A press while it speaks stops it.
struct CallFace: View {
    let s: CallSnapshot
    let crest: Int32
    let name: String
    let onInterrupt: () -> Void
    @Environment(\.accessibilityReduceMotion) private var still

    private var tone: Color {
        switch callTone(s) {
        case .you: return Theme.cobalt
        case .bot, .thinking: return Theme.ink
        case .needsYou, .muted: return Theme.vermilion
        case .working: return Theme.emerald
        case .ended, .quiet: return Theme.lineStrong
        }
    }

    var body: some View {
        let loud = s.state == .hearing || s.state == .speaking
        let face = ZStack {
            ForEach(0..<3, id: \.self) { index in
                let i = CGFloat(index)
                let opacity = loud ? [0.9, 0.45, 0.2][index] : index == 0 ? (s.state == .ended ? 0 : s.state == .listening ? 1 : 0.35) : 0
                Circle()
                    .strokeBorder(tone, lineWidth: index == 0 ? 2 : 1.25)
                    .frame(width: 128 + i * 34, height: 128 + i * 34)
                    .scaleEffect(loud && !still ? 1 + CGFloat(s.level) * (0.32 - i * 0.06) : 1)
                    .opacity(opacity)
                    .breathing(index == 0 && s.state == .thinking)
            }
            CrestView(index: crest, size: 104)
                .opacity(s.state == .connecting ? 0.4 : 1)
            if s.state == .connecting { Spinner(size: 22) }
            if s.muted {
                Image(systemName: "mic.slash.fill")
                    .font(.system(size: 14))
                    .foregroundStyle(Theme.onAccent)
                    .frame(width: 32, height: 32)
                    .background(Theme.vermilion, in: Circle())
                    .overlay(Circle().stroke(Theme.canvas, lineWidth: 3))
                    .offset(x: 34, y: 34)
                    .transition(.scale(scale: 0.6).combined(with: .opacity))
            }
        }
        .frame(width: 250, height: 210)
        .animation(.linear(duration: 0.09), value: s.level)
        .animation(Motion.state, value: callTone(s))
        if s.state == .speaking {
            Button(action: onInterrupt) { face }
                .buttonStyle(.plain)
                .accessibilityLabel("Stop \(name) talking")
        } else {
            face
        }
    }
}

/// The sentence the Bot is on in the reply serif, the one before it fading above, and your last
/// turn as heard beneath them.
private struct CallCaptions: View {
    let s: CallSnapshot
    let name: String
    @State private var lines = (prev: "", cur: "")

    private var hint: String {
        guard s.state == .listening, s.heard.isEmpty, lines.cur.isEmpty else { return "" }
        if s.muted { return "\(name) can't hear you. Unmute to talk." }
        if s.ptt { return "Hold the button while you talk." }
        return "Talk to \(name). Speak over it any time to cut in. Raise the phone to your ear to take it off the speaker."
    }

    var body: some View {
        VStack(spacing: 10) {
            VStack(spacing: 6) {
                if !s.error.isEmpty && s.state != .ended {
                    Text(s.error).font(.silo(14)).foregroundStyle(Theme.vermilion)
                } else if !hint.isEmpty {
                    Text(hint).font(.silo(14)).foregroundStyle(Theme.ink3)
                } else {
                    if !lines.prev.isEmpty {
                        Text(lines.prev).font(.reply(16)).foregroundStyle(Theme.ink3).lineLimit(2)
                    }
                    if !lines.cur.isEmpty {
                        Text(lines.cur).font(.reply(21)).foregroundStyle(Theme.ink).lineLimit(5)
                            .id(lines.cur)
                            .transition(.opacity.combined(with: .offset(y: 6)))
                    }
                }
            }
            .multilineTextAlignment(.center)
            .frame(minHeight: 124, alignment: .bottom)
            Text(s.hearing.isEmpty ? (s.heard.isEmpty ? "" : "“\(s.heard)”") : "“\(s.hearing)…”")
                .font(.silo(14))
                .foregroundStyle(Theme.ink3)
                .lineLimit(2)
                .multilineTextAlignment(.center)
                .frame(minHeight: 22)
        }
        .padding(.top, 18)
        .animation(Motion.settle(0.32), value: lines.cur)
        .onChange(of: s.saying, initial: true) { _, saying in
            guard !saying.isEmpty, saying != lines.cur else { return }
            lines = (lines.cur, saying)
        }
    }
}

/// Route, hold to talk, mute, and End. The route is where the phone's call differs from the web's.
private struct CallDock: View {
    let call: CallModel
    let s: CallSnapshot

    private let tile: CGFloat = 54

    var body: some View {
        if s.state == .ended {
            Button {
                withAnimation(Motion.state) { call.dismiss() }
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: "text.bubble").font(.system(size: 14))
                    Text("Back to the chat")
                }
            }
            .buttonStyle(.quiet)
            .padding(.bottom, 24)
        } else {
            VStack(spacing: 12) {
                if s.ptt { holdButton }
                HStack(spacing: 12) {
                    routeButton
                    toggle(symbol: "hand.raised", on: s.ptt, label: "Hold to talk") { call.setPtt(!s.ptt) }
                    muteButton
                    Spacer(minLength: 0)
                    Button { call.hangUp() } label: {
                        Image(systemName: "phone.down.fill")
                            .font(.system(size: 20))
                            .foregroundStyle(Theme.onAccent)
                            .frame(width: 78, height: tile)
                            .background(Theme.vermilion, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
                    }
                    .buttonStyle(.press)
                    .accessibilityLabel("End call")
                }
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 14)
            .transition(.rise)
        }
    }

    private var holdButton: some View {
        Text(s.holding ? "Talking…" : "Hold to talk")
            .font(.silo(15, .medium))
            .foregroundStyle(s.holding ? Theme.onAccent : Theme.ink)
            .frame(maxWidth: .infinity)
            .frame(height: 54)
            .background(s.holding ? Theme.cobalt : Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(s.holding ? .clear : Theme.line))
            .scaleEffect(s.holding ? 0.98 : 1)
            .animation(Motion.quiet(0.12), value: s.holding)
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { _ in if !s.holding { call.hold(true) } }
                    .onEnded { _ in call.hold(false) }
            )
            .sensoryFeedback(.impact(weight: .light), trigger: s.holding)
            .accessibilityLabel("Hold to talk")
    }

    private var routeButton: some View {
        SiloMenu {
            MenuHeading(text: "Sound")
            ForEach(call.routes, id: \.self) { route in
                MenuRow(title: route.title, symbol: route.symbol, checked: route == call.route) { call.use(route) }
            }
        } label: {
            tileBody(symbol: call.route.symbol, on: call.route != .earpiece)
        }
        .accessibilityLabel("Sound: \(call.route.title)")
    }

    private var muteButton: some View {
        Button { call.setMuted(!s.muted) } label: {
            Image(systemName: s.muted ? "mic.slash.fill" : "mic")
                .font(.system(size: 20))
                .foregroundStyle(s.muted ? Theme.vermilion : Theme.ink2)
                .frame(width: tile, height: tile)
                .background(s.muted ? Theme.vermilionPale : Theme.surface,
                            in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(s.muted ? .clear : Theme.line))
        }
        .buttonStyle(.press)
        .sensoryFeedback(.selection, trigger: s.muted)
        .accessibilityLabel(s.muted ? "Unmute" : "Mute")
    }

    private func toggle(symbol: String, on: Bool, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) { tileBody(symbol: symbol, on: on) }
            .buttonStyle(.press)
            .accessibilityLabel(label)
            .accessibilityAddTraits(on ? .isSelected : [])
    }

    private func tileBody(symbol: String, on: Bool) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 20))
            .foregroundStyle(on ? Theme.ink : Theme.ink2)
            .frame(width: tile, height: tile)
            .background(on ? Theme.well : Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
    }
}

/// The call's length, ticking.
struct CallClock: View {
    let since: Date

    var body: some View {
        TimelineView(.periodic(from: since, by: 1)) { context in
            Text(callClock(seconds: Int(context.date.timeIntervalSince(since))))
                .font(.siloMono(12.5))
                .foregroundStyle(Theme.ink3)
                .monospacedDigit()
        }
    }
}

/// The call while you do something else: the orb, what is going on, the clock, Mute and End. It sits
/// over every page of the app, and a press opens the call screen.
struct CallBar: View {
    @Environment(AppModel.self) private var model
    let call: CallModel

    var body: some View {
        if let s = call.snap, let who = call.who {
            let doing = model.bot(who.id).flatMap { chatDoing($0, s.chatID) }
            let ended = s.state == .ended
            HStack(spacing: 10) {
                Button {
                    if s.state == .speaking {
                        call.interrupt()
                    } else {
                        withAnimation(Motion.quiet(0.32)) { call.expanded = true }
                    }
                } label: {
                    orb(s)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(s.state == .speaking ? "Stop \(who.name) talking" : "Open the call screen")
                Button {
                    withAnimation(Motion.quiet(0.32)) { call.expanded = true }
                } label: {
                    VStack(alignment: .leading, spacing: 1) {
                        HStack(spacing: 6) {
                            Text(stateWords(s, botName: who.name, doing: doing.map(Doing.init)))
                                .font(.silo(13.5, .medium))
                                .foregroundStyle(s.state == .needsYou || (ended && !s.error.isEmpty) ? Theme.vermilion : Theme.ink)
                                .lineLimit(1)
                            if !ended { CallClock(since: s.startedAt) }
                        }
                        Text(caption(s)).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                if ended {
                    Button { call.dismiss() } label: { Image(systemName: "xmark") }
                        .buttonStyle(.glyph)
                        .accessibilityLabel("Dismiss")
                } else {
                    Button { call.setMuted(!s.muted) } label: {
                        Image(systemName: s.muted ? "mic.slash.fill" : "mic")
                    }
                    .buttonStyle(.glyph(tint: s.muted ? Theme.vermilion : Theme.ink2))
                    .accessibilityLabel(s.muted ? "Unmute" : "Mute")
                    Button { call.hangUp() } label: {
                        Image(systemName: "phone.down.fill")
                            .font(.system(size: 14))
                            .foregroundStyle(Theme.onAccent)
                            .frame(width: 44, height: 36)
                            .background(Theme.vermilion, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                    }
                    .buttonStyle(.press)
                    .accessibilityLabel("End call")
                }
            }
            .padding(.leading, 8)
            .padding(.trailing, 10)
            .padding(.vertical, 6)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.line))
            .floatShadow()
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .transition(.rise)
        }
    }

    private func caption(_ s: CallSnapshot) -> String {
        if s.state == .speaking { return s.saying }
        if !s.hearing.isEmpty { return "“\(s.hearing)…”" }
        if !s.heard.isEmpty { return "“\(s.heard)”" }
        return s.state == .listening ? "Talk any time" : " "
    }

    @ViewBuilder
    private func orb(_ s: CallSnapshot) -> some View {
        let loud = s.state == .hearing || s.state == .speaking
        ZStack {
            if s.state == .connecting {
                Spinner(size: 16)
            } else {
                Circle()
                    .fill(orbFill(callTone(s)))
                    .frame(width: 20, height: 20)
                    .scaleEffect(loud ? 1 + CGFloat(s.level) * 0.45 : 1)
                    .breathing(s.state == .thinking)
                    .animation(.linear(duration: 0.09), value: s.level)
            }
        }
        .frame(width: 40, height: 40)
    }

    private func orbFill(_ tone: CallTone) -> Color {
        switch tone {
        case .you: return Theme.cobalt
        case .bot: return Theme.ink
        case .needsYou: return Theme.vermilion
        case .muted: return Theme.vermilionPale
        case .working: return Theme.emerald
        case .thinking: return Theme.ink3
        case .ended, .quiet: return Theme.lineStrong
        }
    }
}
