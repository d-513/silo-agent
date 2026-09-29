import SwiftUI

/// Every Bot page except Chat is still on the web client.
struct PlaceholderPane: View {
    let tab: BotTab

    var body: some View {
        ContentUnavailableView {
            Label(tab.title, systemImage: tab.symbol)
        } description: {
            Text(tab == .desktop ? "Desktop and Console stay on the web client." : "Not ported to the iOS client yet. Use the web client for this page.")
        }
    }
}
