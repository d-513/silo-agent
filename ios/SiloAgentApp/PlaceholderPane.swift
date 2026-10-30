import SwiftUI

/// Every Bot page except Chat is still on the web client. It sits inside the page frame, so all it
/// brings is the well that says so.
struct PlaceholderPane: View {
    let tab: BotTab

    var body: some View {
        EmptyWell(symbol: tab.symbol, title: tab.title, message: message)
            .frame(maxWidth: 560)
            .padding(16)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Theme.canvas.ignoresSafeArea())
            .rise()
    }

    private var message: String {
        tab == .desktop ? "Desktop and Console stay on the web client." : "Not ported to the iOS client yet. Use the web client for this page."
    }
}
