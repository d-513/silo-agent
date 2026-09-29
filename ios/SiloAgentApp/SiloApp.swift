import SwiftUI

@main
struct SiloApp: App {
    @State private var model = AppModel()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            Group {
                if model.restoring && model.user == nil {
                    VStack(spacing: 12) {
                        ProgressView()
                        if let error = model.restoreError {
                            Text(error).font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
                            Text("Retrying…").font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                    .padding()
                } else if model.user == nil {
                    SignInView()
                } else {
                    RootView()
                }
            }
            .environment(model)
            .task {
                await model.restoreSession()
                #if DEBUG
                await DebugLaunch.run(model)
                #endif
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { Task { await model.resume() } }
            }
        }
    }
}
