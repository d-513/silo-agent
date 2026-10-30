import SiloClient
import SwiftUI
import WidgetKit

/// Paper in the light, the dark canvas in the dark; no system material.
extension View {
    func siloWidget() -> some View {
        containerBackground(Theme.canvas, for: .widget)
    }
}

/// The Bot a widget shows: the one picked, else the last one used.
func picked(_ entity: BotEntity?, in state: SharedState) -> SharedBot? {
    state.bots.first { $0.id == entity?.id } ?? state.lastBot
}

/// Open the app and sign in: shown until a widget has something to say.
struct SignedOutWidget: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            CrestView(index: packCrest(shape: 2, color: 11), size: 30)
            Spacer(minLength: 0)
            Text("Silo").font(.silo(15, .semibold)).foregroundStyle(Theme.ink)
            Text("Open the app and sign in.").font(.silo(12.5)).foregroundStyle(Theme.ink3)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }
}
