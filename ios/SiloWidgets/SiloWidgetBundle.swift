import SiloClient
import SwiftUI
import WidgetKit

@main
struct SiloWidgetBundle: WidgetBundle {
    init() { SiloGroup.boot() }

    var body: some Widget {
        BotWidget()
        BotsWidget()
        NeedsYouWidget()
        BoardWidget()
        CallLiveActivity()
        WorkLiveActivity()
        CallControlWidget()
        NewChatControlWidget()
        NeedsYouControlWidget()
    }
}
