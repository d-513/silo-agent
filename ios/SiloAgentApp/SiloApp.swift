import CoreText
import SwiftUI
import UIKit

@main
struct SiloApp: App {
    @State private var model = AppModel()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        Self.registerFonts()
        Self.styleBars()
    }

    /// Geist, Geist Mono and Source Serif 4 ship in the bundle (OFL); register whatever is there.
    private static func registerFonts() {
        let urls = (Bundle.main.urls(forResourcesWithExtension: "ttf", subdirectory: nil) ?? [])
            + (Bundle.main.urls(forResourcesWithExtension: "ttf", subdirectory: "Fonts") ?? [])
        for url in urls {
            CTFontManagerRegisterFontsForURL(url as CFURL, .process, nil)
        }
    }

    /// Navigation titles in Geist and ink, on paper.
    private static func styleBars() {
        let ink = UIColor(Theme.ink)
        let appearance = UINavigationBarAppearance()
        appearance.configureWithTransparentBackground()
        if let title = UIFont(name: "Geist-Regular", size: 17) {
            let semibold = title.withWeight(.semibold)
            appearance.titleTextAttributes = [.font: UIFontMetrics(forTextStyle: .headline).scaledFont(for: semibold), .foregroundColor: ink]
        }
        if let large = UIFont(name: "Geist-Regular", size: 32) {
            let medium = large.withWeight(.medium)
            appearance.largeTitleTextAttributes = [.font: UIFontMetrics(forTextStyle: .largeTitle).scaledFont(for: medium), .foregroundColor: ink]
        }
        UINavigationBar.appearance().standardAppearance = appearance
        UINavigationBar.appearance().scrollEdgeAppearance = appearance
        UINavigationBar.appearance().compactAppearance = appearance
    }

    var body: some Scene {
        WindowGroup {
            Group {
                if model.restoring && model.user == nil {
                    VStack(spacing: 12) {
                        ProgressView()
                        if let error = model.restoreError {
                            Text(error).font(.silo(.footnote)).foregroundStyle(.secondary).multilineTextAlignment(.center)
                            Text("Retrying…").font(.silo(.caption)).foregroundStyle(.tertiary)
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
            .font(.silo(.body))
            .foregroundStyle(Theme.ink)
            .tint(Theme.cobalt)
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

private extension UIFont {
    /// A weight of a variable font, by its `wght` axis.
    func withWeight(_ weight: UIFont.Weight) -> UIFont {
        let wght = 0x7767_6874 // 'wght'
        let value: CGFloat
        switch weight {
        case .medium: value = 500
        case .semibold: value = 600
        case .bold: value = 700
        default: value = 400
        }
        let descriptor = fontDescriptor.addingAttributes([
            UIFontDescriptor.AttributeName(rawValue: kCTFontVariationAttribute as String): [wght: value],
        ])
        return UIFont(descriptor: descriptor, size: pointSize)
    }
}
