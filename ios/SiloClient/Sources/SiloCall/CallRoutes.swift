#if os(iOS)
import AVFoundation

/// Where a call's sound goes: the loudspeaker, the earpiece, or whatever is plugged in or paired.
public enum CallRoute: Equatable, Sendable, Hashable {
    case speaker
    case earpiece
    case external(name: String, kind: Kind)

    public enum Kind: Equatable, Sendable, Hashable {
        case bluetooth, headphones, car, other
    }

    public var title: String {
        switch self {
        case .speaker: return "Speaker"
        case .earpiece: return "iPhone"
        case .external(let name, _): return name
        }
    }

    public var symbol: String {
        switch self {
        case .speaker: return "speaker.wave.2.fill"
        case .earpiece: return "iphone"
        case .external(_, .bluetooth): return "airpods"
        case .external(_, .headphones): return "headphones"
        case .external(_, .car): return "car.fill"
        case .external(_, .other): return "hifispeaker.fill"
        }
    }
}

/// The audio session of a call. CallKit activates it on a real call; everything else here is
/// about where the sound goes once it is live.
public enum CallAudioSession {
    /// Ready for a voice call: echo cancelling, Bluetooth headsets allowed. Earpiece unless told.
    public static func configure(microphone: Bool = true) throws {
        let session = AVAudioSession.sharedInstance()
        if microphone {
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.allowBluetoothHFP])
        } else {
            try session.setCategory(.playback, mode: .spokenAudio)
        }
        try session.setPreferredSampleRate(48_000)
        try session.setPreferredIOBufferDuration(0.01)
    }

    public static func activate() throws {
        try AVAudioSession.sharedInstance().setActive(true)
    }

    public static func deactivate() {
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    /// The route the sound is on now.
    public static var current: CallRoute {
        let session = AVAudioSession.sharedInstance()
        guard let port = session.currentRoute.outputs.first else { return .earpiece }
        switch port.portType {
        case .builtInSpeaker: return .speaker
        case .builtInReceiver: return .earpiece
        default: return route(of: port)
        }
    }

    private static func route(of port: AVAudioSessionPortDescription) -> CallRoute {
        let kind: CallRoute.Kind
        switch port.portType {
        case .bluetoothHFP, .bluetoothA2DP, .bluetoothLE: kind = .bluetooth
        case .headphones, .headsetMic, .usbAudio: kind = .headphones
        case .carAudio: kind = .car
        default: kind = .other
        }
        return .external(name: port.portName, kind: kind)
    }

    /// Whether something is connected that takes the sound by itself: headphones, AirPods, a car.
    public static var hasExternal: Bool {
        if case .external = current { return true }
        return false
    }

    /// Everything a call can use right now, the current one included.
    public static var available: [CallRoute] {
        let session = AVAudioSession.sharedInstance()
        var routes: [CallRoute] = [.earpiece, .speaker]
        for port in session.availableInputs ?? [] where port.portType != .builtInMic {
            routes.append(route(of: port))
        }
        let now = current
        if !routes.contains(now) { routes.append(now) }
        return routes
    }

    /// Moves the sound. Asking for the speaker or earpiece also lets go of a headset.
    public static func select(_ route: CallRoute) throws {
        let session = AVAudioSession.sharedInstance()
        switch route {
        case .speaker:
            try session.setPreferredInput(session.availableInputs?.first { $0.portType == .builtInMic })
            try session.overrideOutputAudioPort(.speaker)
        case .earpiece:
            try session.setPreferredInput(session.availableInputs?.first { $0.portType == .builtInMic })
            try session.overrideOutputAudioPort(.none)
        case .external(let name, _):
            try session.overrideOutputAudioPort(.none)
            try session.setPreferredInput(session.availableInputs?.first { $0.portName == name })
        }
    }
}
#endif
