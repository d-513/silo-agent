import SwiftUI

@main
struct SiloApp: App {
    @State private var model = AppModel()

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
        }
    }
}
