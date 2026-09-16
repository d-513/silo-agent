import SiloClient
import SwiftUI

/// The Bot pages. Only `chat` is implemented; the rest are placeholders.
enum BotTab: String, CaseIterable, Identifiable {
    case chat
    case desktop
    case files
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
        case .desktop: return "Desktop"
        case .files: return "Files"
        case .connectors: return "Connectors"
        case .channels: return "Channels"
        case .skills: return "Skills"
        case .secrets: return "Secrets"
        case .rules: return "Rules"
        case .container: return "Container"
        case .settings: return "Settings"
        }
    }

    var symbol: String {
        switch self {
        case .chat: return "bubble.left.and.bubble.right"
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
        return .green
    case "needs_you":
        return .red
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

