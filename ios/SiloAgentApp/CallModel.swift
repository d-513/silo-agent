import ActivityKit
import AVFoundation
import Observation
import SiloCall
import SiloClient
import SwiftUI
import UIKit

/// The one call. It belongs to the app, not to a page: it goes on while you move around, leave the
/// app or lock the phone. It starts on the loudspeaker; raise the phone to your ear and the sound
/// moves to the earpiece, lower it and it comes back. Headphones and a car take over by themselves.
@MainActor
@Observable
final class CallModel: NSObject {
    static let shared = CallModel()

    struct Who: Equatable {
        var id: String
        var name: String
        var crest: Int32
    }

    private(set) var snap: CallSnapshot?
    private(set) var who: Who?
    /// The call screen; otherwise the call is the bar over the page.
    var expanded = false
    private(set) var route: CallRoute = .speaker
    private(set) var routes: [CallRoute] = []
    /// The phone is at the ear, so the sound is on the earpiece for now.
    private(set) var atEar = false
    private(set) var failure: String?

    /// A call that has not ended.
    var live: Bool { snap.map { $0.state != .ended } ?? false }

    private var session: CallSession?
    private let system = CallSystem()
    private var wanted: CallRoute = .speaker
    private var ownAudio = false
    private var activity: Activity<CallActivityAttributes>?
    private var lastPush = Date.distantPast
    private var pushTask: Task<Void, Never>?
    private var greeted = false
    private var observers: [NSObjectProtocol] = []
    // Which call a snapshot belongs to: the last one's ending can land after this one started.
    private var generation = 0
    private var audioUp = false
    // What you asked for, kept here because something else having the audio mutes the call too,
    // and a snapshot is a moment behind.
    private var youMuted = false
    private var audioTaken = false

    private override init() {
        super.init()
        system.onActivated = { [weak self] in
            self?.audioLive()
            self?.resume()
        }
        system.onDeactivated = { [weak self] in self?.interrupted(.began) }
        system.onEnd = { [weak self] in self?.session?.hangup() }
        system.onMute = { [weak self] muted in self?.muteAsked(muted) }
    }

    // MARK: starting and ending

    /// Rings a Bot in a chat: `""` is the main chat, `"new"` a chat of its own.
    func start(_ who: Who, chat: String = "", screen: Bool = true, host: String? = nil) async {
        if live {
            if self.who?.id == who.id {
                expanded = screen
                return
            }
            hangUp()
        }
        let server = host ?? SiloStore.load().server
        guard !server.isEmpty else { return }
        finishUp()
        var first = CallSnapshot(chatID: chat)
        first.startedAt = Date()
        snap = first
        self.who = who
        expanded = screen
        failure = nil
        greeted = false
        wanted = .speaker
        // It would fight the call for the audio session.
        AppModel.shared.readAloud.stop()
        func refuse(_ reason: String) {
            first.state = .ended
            first.ended = .error
            first.error = reason
            snap = first
            lingerThenGo()
        }
        // The question is a dialog, and from the Action Button there is nothing to show it on.
        if AVAudioApplication.shared.recordPermission == .undetermined, UIApplication.shared.applicationState != .active {
            refuse("Open Silo and call from there once, to allow the microphone.")
            return
        }
        guard await AVAudioApplication.requestRecordPermission() else {
            refuse("Microphone access is off. Turn it on in Settings.")
            return
        }
        var microphone = true
        #if targetEnvironment(simulator)
        // Voice I/O aborts the app in a simulator the Mac has not let near its microphone, and that
        // cannot be caught. The Bot is heard; set SILO_DEV_MIC to talk to it there.
        microphone = ProcessInfo.processInfo.environment["SILO_DEV_MIC"] != nil
        #endif
        let mine = generation
        let session = CallSession(host: server, botID: who.id, chat: chat, microphone: microphone) { [weak self] snapshot in
            Task { @MainActor in
                guard let self, self.generation == mine else { return }
                self.changed(snapshot)
            }
        }
        self.session = session
        watchAudio()
        startActivity(who)
        UIApplication.shared.isIdleTimerDisabled = true
        ownAudio = !(await system.start(botID: who.id, botName: who.name))
        // Hung up meanwhile.
        guard self.session === session else { return }
        if ownAudio {
            do {
                try CallAudioSession.configure(microphone: microphone)
                try CallAudioSession.activate()
                audioLive()
            } catch {
                session.fail("The phone would not give the call its audio: \(error.localizedDescription)")
                return
            }
        } else {
            awaitActivation(session)
        }
        // Only now: CallKit has to know the call before it can hear that it connected.
        session.start()
    }

    // CallKit does not always say so, when the audio session was live already.
    private func awaitActivation(_ session: CallSession) {
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(3))
            guard let self, self.session === session, !self.audioUp else { return }
            do {
                try CallAudioSession.activate()
                self.audioLive()
            } catch {
                session.fail("The phone would not give the call its audio: \(error.localizedDescription)")
            }
        }
    }

    private func audioLive() {
        guard let session else { return }
        audioUp = true
        // Before the engine starts: moving the sound under it can stop it.
        // Headphones keep the sound; otherwise the room hears it until the phone goes to an ear.
        if !CallAudioSession.hasExternal { apply(wanted) }
        session.audioActivated()
        refreshRoutes()
        startProximity()
    }

    /// Ends the call, after handing over whatever you were still saying.
    func hangUp() {
        session?.hangup()
        system.hangUp()
    }

    /// Puts an ended call away.
    func dismiss() {
        snap = nil
        who = nil
        expanded = false
    }

    private func changed(_ next: CallSnapshot) {
        let before = snap
        snap = next
        if next.callID != "", !greeted {
            greeted = true
            system.connected()
        }
        if next.state == .ended, before?.state != .ended {
            ended(next)
        } else {
            pushActivity()
        }
    }

    private func ended(_ last: CallSnapshot) {
        // Whoever ended it, CallKit has to hear: it is a no-op once it knows.
        system.ended(failed: last.ended == .error || last.ended == .lost)
        if ownAudio { CallAudioSession.deactivate() }
        stopProximity()
        unwatchAudio()
        UIApplication.shared.isIdleTimerDisabled = false
        session = nil
        audioUp = false
        audioTaken = false
        failure = last.error.isEmpty ? nil : last.error
        endActivity(last)
        lingerThenGo()
    }

    // An ended call stays a moment, so its last word can be read.
    private func lingerThenGo() {
        let wait = snap?.error.isEmpty == false ? 9.0 : 2.5
        let was = snap?.startedAt
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(wait))
            guard let self, self.snap?.state == .ended, self.snap?.startedAt == was else { return }
            withAnimation(Motion.state) { self.dismiss() }
        }
    }

    private func finishUp() {
        generation += 1
        if let snap { endActivity(snap) }
        pushTask?.cancel()
        snap = nil
        session = nil
        audioUp = false
        audioTaken = false
        youMuted = false
    }

    // MARK: presses

    func setMuted(_ muted: Bool) {
        muteAsked(muted)
        system.setMuted(muted)
    }

    private func muteAsked(_ muted: Bool) {
        youMuted = muted
        if !audioTaken { session?.setMuted(muted) }
    }

    func setPtt(_ on: Bool) { session?.setPtt(on) }
    func hold(_ down: Bool) { session?.hold(down) }
    func interrupt() { session?.interrupt() }

    /// A press on the Island or the lock screen.
    func control(_ control: CallControl) {
        switch control {
        case .mute: setMuted(!(snap?.muted ?? false))
        case .speaker: use(route == .speaker ? .earpiece : .speaker)
        case .end: hangUp()
        case .stopTalking: interrupt()
        }
    }

    #if DEBUG
    func debugStopEngine() { session?.debugStopEngine() }
    var debugEngineRunning: Bool { session?.debugEngineRunning ?? false }
    #endif

    // MARK: where the sound goes

    func use(_ next: CallRoute) {
        wanted = next
        apply(next)
    }

    private func apply(_ next: CallRoute) {
        try? CallAudioSession.select(next)
        refreshRoutes()
    }

    private func refreshRoutes() {
        route = CallAudioSession.current
        routes = CallAudioSession.available
        pushActivity()
    }

    private func watchAudio() {
        unwatchAudio()
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { [weak self] note in
            let reason = (note.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt).flatMap(AVAudioSession.RouteChangeReason.init)
            MainActor.assumeIsolated { self?.routeChanged(reason) }
        })
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { [weak self] note in
            let type = (note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt).flatMap(AVAudioSession.InterruptionType.init)
            MainActor.assumeIsolated { self?.interrupted(type) }
        })
        observers.append(center.addObserver(forName: UIDevice.proximityStateDidChangeNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.proximityChanged() }
        })
        observers.append(center.addObserver(forName: AVAudioSession.mediaServicesWereResetNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.session?.fail("The phone's audio restarted. Call again.") }
        })
        // Nothing promises the end of an interruption, and the sensor may have gone quiet while
        // the phone was locked: coming back is the time to look again.
        observers.append(center.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.resume()
                if self?.audioUp == true { self?.proximityChanged() }
            }
        })
    }

    private func unwatchAudio() {
        observers.forEach(NotificationCenter.default.removeObserver)
        observers = []
    }

    // A headset that left takes the speaker with it (the override lasts one route), and the sound
    // would land on the earpiece of a phone lying on the table.
    private func routeChanged(_ reason: AVAudioSession.RouteChangeReason?) {
        if reason == .oldDeviceUnavailable {
            if case .external = wanted { wanted = .speaker }
            if audioUp, !CallAudioSession.hasExternal {
                apply(atEar ? .earpiece : wanted)
                return
            }
        }
        refreshRoutes()
    }

    // Something else took the audio (a phone call, Siri): you are not heard meanwhile, and the Bot
    // is told nothing. When it hands the audio back the call picks up. The engine stops by itself
    // in both cases, CallKit or not; only the session is CallKit's to activate.
    private func interrupted(_ type: AVAudioSession.InterruptionType?) {
        switch type {
        case .began:
            guard !audioTaken else { return }
            audioTaken = true
            session?.audioHeld()
            session?.setMuted(true)
        case .ended:
            resume()
        default:
            break
        }
    }

    private func resume() {
        guard audioTaken else { return }
        audioTaken = false
        if ownAudio { try? CallAudioSession.activate() }
        session?.audioActivated()
        if !youMuted { session?.setMuted(false) }
    }

    // MARK: to the ear and back

    private func startProximity() {
        UIDevice.current.isProximityMonitoringEnabled = true
    }

    private func stopProximity() {
        UIDevice.current.isProximityMonitoringEnabled = false
        atEar = false
    }

    private func proximityChanged() {
        let near = UIDevice.current.proximityState
        atEar = near
        // Headphones already have the sound.
        guard !CallAudioSession.hasExternal else { return }
        apply(near ? .earpiece : wanted)
    }

    // MARK: the Dynamic Island and the lock screen

    private func contentState(_ s: CallSnapshot, ended: Bool = false) -> CallActivityAttributes.ContentState {
        let doing = AppModel.shared.bot(who?.id ?? "").flatMap { chatDoing($0, s.chatID) }
        let caption: String
        if s.state == .speaking {
            caption = s.saying
        } else if !s.hearing.isEmpty {
            caption = "“\(s.hearing)…”"
        } else if !s.heard.isEmpty {
            caption = "“\(s.heard)”"
        } else {
            caption = ""
        }
        return .init(tone: callTone(s), words: stateWords(s, botName: who?.name ?? "The Bot", doing: doing.map(Doing.init)),
                     caption: caption, muted: s.muted, routeSymbol: route.symbol, speaker: route == .speaker,
                     startedAt: s.startedAt, ended: ended)
    }

    private func startActivity(_ who: Who) {
        guard ActivityAuthorizationInfo().areActivitiesEnabled, let snap else { return }
        let attributes = CallActivityAttributes(botID: who.id, botName: who.name, crest: who.crest)
        activity = try? Activity.request(attributes: attributes, content: .init(state: contentState(snap), staleDate: nil))
    }

    // What the words say changes often: the Island gets it at most once a second, and always the last.
    private func pushActivity() {
        guard let activity, let snap else { return }
        let state = contentState(snap)
        pushTask?.cancel()
        let wait = max(0, 1 - Date().timeIntervalSince(lastPush))
        pushTask = Task { [weak self] in
            if wait > 0 { try? await Task.sleep(for: .seconds(wait)) }
            guard !Task.isCancelled else { return }
            self?.lastPush = Date()
            await activity.update(.init(state: state, staleDate: nil))
        }
    }

    private func endActivity(_ last: CallSnapshot) {
        pushTask?.cancel()
        guard let activity else { return }
        self.activity = nil
        let state = contentState(last, ended: true)
        Task { await activity.end(.init(state: state, staleDate: nil), dismissalPolicy: .after(.now + 4)) }
    }
}
