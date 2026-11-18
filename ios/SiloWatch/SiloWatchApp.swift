import CoreText
import SiloClient
import SwiftUI

/// Silo on the wrist: the Bots at a glance, what waits on you (answered with Allow once or Deny),
/// a dictated message and its reply, and the Feed. The phone hands over the sign-in
/// (`WatchLink`); after that the watch talks to the server itself, over Wi-Fi, LTE or the phone.
@main
struct SiloWatchApp: App {
    @State private var model = WatchModel.shared
    @Environment(\.scenePhase) private var scenePhase

    init() {
        SiloGroup.boot()
        Self.registerFonts()
        WatchLink.shared.activate()
    }

    private static func registerFonts() {
        for url in Bundle.main.urls(forResourcesWithExtension: "ttf", subdirectory: nil) ?? [] {
            CTFontManagerRegisterFontsForURL(url as CFURL, .process, nil)
        }
    }

    var body: some Scene {
        WindowGroup {
            WatchRoot()
                .environment(model)
                .tint(Theme.cobalt)
                .onChange(of: scenePhase) { _, phase in
                    guard phase == .active else { return }
                    Task { await model.refresh() }
                    if !model.signedIn { WatchLink.shared.ask() }
                }
        }
    }
}
