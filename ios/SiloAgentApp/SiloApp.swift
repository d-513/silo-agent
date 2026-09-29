import SwiftUI

@main
struct SiloApp: App {
    @State private var model = AppModel()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            Group {
                if model.restoring && model.user == nil {
                    ProgressView()
                } else if model.user == nil {
                    SignInView()
                } else {
                    RootView()
                }
            }
            .environment(model)
            .task { await model.restoreSession() }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { Task { await model.resume() } }
            }
        }
    }
}
