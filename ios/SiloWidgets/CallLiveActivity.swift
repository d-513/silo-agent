import AppIntents
import ActivityKit
import SiloClient
import SwiftUI
import WidgetKit

/// A call on the Dynamic Island and the lock screen: the Bot's face, what is going on, what it is
/// saying, and Mute, Speaker and End. The tone is the call screen's.
struct CallLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: CallActivityAttributes.self) { context in
            LockCall(context: context)
                .activityBackgroundTint(Theme.hatch)
                .activitySystemActionForegroundColor(Theme.onHatch)
        } dynamicIsland: { context in
            let state = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    CrestView(index: context.attributes.crest, size: 44).padding(.leading, 4).padding(.top, 4)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Text(state.startedAt, style: .timer)
                        .font(.siloMono(13))
                        .foregroundStyle(Theme.onHatch.opacity(0.8))
                        .multilineTextAlignment(.trailing)
                        .frame(width: 56)
                        .padding(.top, 6)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(context.attributes.botName).font(.silo(15, .semibold)).foregroundStyle(Theme.onHatch).lineLimit(1)
                        Text(state.words).font(.silo(12.5)).foregroundStyle(toneColor(state.tone)).lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.top, 4)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: 10) {
                        if !state.caption.isEmpty {
                            Text(state.caption).font(.reply(15)).foregroundStyle(Theme.onHatch).lineLimit(2)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        CallButtons(state: state)
                    }
                    .padding(.top, 6)
                }
            } compactLeading: {
                CrestView(index: context.attributes.crest, size: 22)
            } compactTrailing: {
                if state.tone == .bot || state.tone == .you {
                    Image(systemName: "waveform").font(.system(size: 13)).foregroundStyle(toneColor(state.tone))
                        .symbolEffect(.variableColor.iterative, isActive: true)
                } else if state.muted {
                    Image(systemName: "mic.slash.fill").font(.system(size: 12)).foregroundStyle(Theme.vermilionOnHatch)
                } else {
                    Text(state.startedAt, style: .timer)
                        .font(.siloMono(11))
                        .foregroundStyle(Theme.onHatch.opacity(0.8))
                        .frame(width: 34)
                        .multilineTextAlignment(.trailing)
                }
            } minimal: {
                CrestView(index: context.attributes.crest, size: 20)
            }
            .keylineTint(toneColor(state.tone))
            .widgetURL(SiloLink.call(bot: context.attributes.botID).url)
        }
    }
}

private func toneColor(_ tone: CallTone) -> Color {
    switch tone {
    case .you: return Color(hex: 0x8AA4FF)
    case .needsYou, .muted: return Theme.vermilionOnHatch
    case .working: return Color(hex: 0x4FC38D)
    default: return Theme.onHatch.opacity(0.75)
    }
}

extension Theme {
    /// Vermilion on the hatch, which is dark in both themes.
    static let vermilionOnHatch = Color(hex: 0xF27565)
}

private struct LockCall: View {
    let context: ActivityViewContext<CallActivityAttributes>

    var body: some View {
        let state = context.state
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                CrestView(index: context.attributes.crest, size: 42)
                VStack(alignment: .leading, spacing: 1) {
                    Text(context.attributes.botName).font(.silo(15, .semibold)).foregroundStyle(Theme.onHatch).lineLimit(1)
                    Text(state.words).font(.silo(13)).foregroundStyle(toneColor(state.tone)).lineLimit(1)
                }
                Spacer(minLength: 0)
                if !state.ended {
                    Text(state.startedAt, style: .timer)
                        .font(.siloMono(13))
                        .foregroundStyle(Theme.onHatch.opacity(0.8))
                        .multilineTextAlignment(.trailing)
                        .frame(width: 60)
                }
            }
            if !state.caption.isEmpty {
                Text(state.caption).font(.reply(15)).foregroundStyle(Theme.onHatch).lineLimit(2)
            }
            if !state.ended { CallButtons(state: state) }
        }
        .padding(16)
    }
}

/// Mute, Speaker and End, as the call's own dock in small.
struct CallButtons: View {
    let state: CallActivityAttributes.ContentState

    var body: some View {
        HStack(spacing: 8) {
            Button(intent: CallControlIntent(.mute)) {
                tile(state.muted ? "mic.slash.fill" : "mic", on: state.muted)
            }
            Button(intent: CallControlIntent(.speaker)) {
                tile(state.routeSymbol, on: state.speaker)
            }
            Button(intent: CallControlIntent(.end)) {
                Image(systemName: "phone.down.fill")
                    .font(.system(size: 15))
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity)
                    .frame(height: 40)
                    .background(Color(hex: 0xC4372B), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            }
        }
        .buttonStyle(.plain)
    }

    private func tile(_ symbol: String, on: Bool) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 15))
            .foregroundStyle(on ? Color(hex: 0x141413) : Theme.onHatch)
            .frame(maxWidth: .infinity)
            .frame(height: 40)
            .background(on ? Theme.onHatch : Theme.onHatch.opacity(0.14), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}
