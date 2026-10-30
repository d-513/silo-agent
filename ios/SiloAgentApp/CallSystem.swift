import AVFoundation
import CallKit
import SiloCall
import UIKit

/// The phone's own idea of a call. CallKit gives Silo's call what a phone call has: it survives
/// the lock screen, the system manages the audio session, a headset's button hangs up, and it is in
/// Recents. The simulator has none of it, and a call must work if CallKit refuses, so `start`
/// answers false and the caller runs the audio itself.
@MainActor
final class CallSystem: NSObject {
    var onActivated: (() -> Void)?
    var onDeactivated: (() -> Void)?
    var onEnd: (() -> Void)?
    var onMute: ((Bool) -> Void)?

    private let provider: CXProvider
    private let controller = CXCallController()
    private(set) var uuid: UUID?

    override init() {
        let config = CXProviderConfiguration()
        config.supportsVideo = false
        config.maximumCallGroups = 1
        config.maximumCallsPerCallGroup = 1
        config.supportedHandleTypes = [.generic]
        config.includesCallsInRecents = true
        provider = CXProvider(configuration: config)
        super.init()
        provider.setDelegate(self, queue: .main)
    }

    /// Asks the system for the call. False: run the audio yourself.
    func start(botID: String, botName: String) async -> Bool {
        #if targetEnvironment(simulator)
        return false
        #else
        let id = UUID()
        uuid = id
        let handle = CXHandle(type: .generic, value: botID)
        let action = CXStartCallAction(call: id, handle: handle)
        action.contactIdentifier = botName
        do {
            try await controller.request(CXTransaction(action: action))
            let update = CXCallUpdate()
            update.localizedCallerName = botName
            update.remoteHandle = handle
            update.hasVideo = false
            update.supportsHolding = false
            update.supportsGrouping = false
            update.supportsUngrouping = false
            update.supportsDTMF = false
            provider.reportCall(with: id, updated: update)
            return true
        } catch {
            uuid = nil
            return false
        }
        #endif
    }

    /// The control plane said hello.
    func connected() {
        guard let uuid else { return }
        provider.reportOutgoingCall(with: uuid, connectedAt: Date())
    }

    /// The owner ended it here.
    func hangUp() {
        guard let uuid else { return }
        self.uuid = nil
        controller.request(CXTransaction(action: CXEndCallAction(call: uuid))) { _ in }
    }

    /// The other side ended it.
    func ended(failed: Bool) {
        guard let uuid else { return }
        self.uuid = nil
        provider.reportCall(with: uuid, endedAt: Date(), reason: failed ? .failed : .remoteEnded)
    }

    func setMuted(_ muted: Bool) {
        guard let uuid else { return }
        controller.request(CXTransaction(action: CXSetMutedCallAction(call: uuid, muted: muted))) { _ in }
    }
}

extension CallSystem: CXProviderDelegate {
    nonisolated func providerDidReset(_ provider: CXProvider) {
        MainActor.assumeIsolated {
            uuid = nil
            onEnd?()
        }
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        try? CallAudioSession.configure()
        provider.reportOutgoingCall(with: action.callUUID, startedConnectingAt: Date())
        action.fulfill()
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        MainActor.assumeIsolated {
            if uuid != nil {
                uuid = nil
                onEnd?()
            }
        }
        action.fulfill()
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        let muted = action.isMuted
        MainActor.assumeIsolated { onMute?(muted) }
        action.fulfill()
    }

    nonisolated func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        MainActor.assumeIsolated { onActivated?() }
    }

    nonisolated func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        MainActor.assumeIsolated { onDeactivated?() }
    }
}
