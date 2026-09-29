import SiloClient
import SwiftUI

/// Where the Bot's navigation stack can go.
enum BotRoute: Hashable {
    case chat(String)
    case chats
    case page(BotTab)
}

/// The Bot pages. `automations`, `memories` and `feed` sit beside the chats list on web; the
/// rest are the tab strip. Desktop stays web-only (see `ios/todo_skipped.md`).
enum BotTab: String, CaseIterable, Identifiable {
    case chat
    case automations
    case memories
    case feed
    case desktop
    case files
    case drives
    case connectors
    case channels
    case skills
    case secrets
    case rules
    case container
    case settings

    var id: String { rawValue }

    var title: String {
        switch self {
        case .chat: return "Chat"
        case .automations: return "Automations"
        case .memories: return "Memories"
        case .feed: return "Feed"
        case .drives: return "Drives"
        case .desktop: return "Desktop"
        case .files: return "Files"
        case .connectors: return "Connectors"
        case .channels: return "Channels"
        case .skills: return "Skills"
        case .secrets: return "Secrets"
        case .rules: return "Rules"
        case .container: return "Containers"
        case .settings: return "Settings"
        }
    }

    var symbol: String {
        switch self {
        case .chat: return "bubble.left.and.bubble.right"
        case .automations: return "clock.arrow.circlepath"
        case .memories: return "brain"
        case .feed: return "tray.full"
        case .drives: return "externaldrive"
        case .desktop: return "display"
        case .files: return "folder"
        case .connectors: return "puzzlepiece.extension"
        case .channels: return "antenna.radiowaves.left.and.right"
        case .skills: return "book"
        case .secrets: return "key"
        case .rules: return "checklist"
        case .container: return "shippingbox"
        case .settings: return "slider.horizontal.3"
        }
    }
}

func statusColor(_ status: String) -> Color {
    switch status {
    case "working", "online", "idle":
        return Theme.lamp
    case "needs_you":
        return Theme.vermilion
    case "stopped", "starting":
        return .secondary
    default:
        return .secondary
    }
}

func statusLabel(_ status: String) -> String {
    if status == "idle" { return "online" }
    return status.replacingOccurrences(of: "_", with: " ")
}

struct StatusDot: View {
    let status: String
    var size: CGFloat = 8

    var body: some View {
        Circle()
            .fill(statusColor(status))
            .frame(width: size, height: size)
            .overlay {
                if status == "working" {
                    Circle().stroke(statusColor(status).opacity(0.3), lineWidth: 3)
                }
            }
            .accessibilityLabel(statusLabel(status))
    }
}

struct StatusLine: View {
    let status: String

    var body: some View {
        HStack(spacing: 6) {
            StatusDot(status: status, size: 8)
            Text(statusLabel(status))
                .font(.caption.weight(.medium))
                .foregroundStyle(statusColor(status))
        }
    }
}

/// Human-readable sizes for file/attachment rows.
func formatBytes(_ count: Int64) -> String {
    let formatter = ByteCountFormatter()
    formatter.countStyle = .file
    return formatter.string(fromByteCount: count)
}

/// Pretty-prints tool arguments when they are valid JSON; otherwise passes them through.
func prettyJSON(_ raw: String) -> String {
    guard let data = raw.data(using: .utf8),
          let object = try? JSONSerialization.jsonObject(with: data),
          let pretty = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted]),
          let string = String(data: pretty, encoding: .utf8)
    else {
        return raw
    }
    return string
}



/// "5 min ago", "yesterday" for an RFC 3339 time; empty when unparsable.
func relativeTime(_ iso: String) -> String {
    guard let date = parseDate(iso) else { return "" }
    let formatter = RelativeDateTimeFormatter()
    formatter.unitsStyle = .short
    return formatter.localizedString(for: date, relativeTo: Date())
}

func parseDate(_ iso: String) -> Date? {
    guard !iso.isEmpty else { return nil }
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    if let date = fractional.date(from: iso) { return date }
    return ISO8601DateFormatter().date(from: iso)
}

func shortDay(_ iso: String) -> String {
    guard let date = parseDate(iso) else { return "" }
    return date.formatted(.dateTime.month(.abbreviated).day().year())
}
