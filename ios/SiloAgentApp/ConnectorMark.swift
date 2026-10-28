import SiloClient
import SwiftUI
import UIKit

/// A connector's own mark (`/connectors/<id>/image`, cookie auth), or the plug when it has none.
struct ConnectorMark: View {
    @Environment(AppModel.self) private var model
    let id: String
    let hasImage: Bool
    var size: CGFloat = 28
    @State private var image: UIImage?

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFill()
            } else {
                Image(systemName: "powerplug")
                    .font(.system(size: size * 0.5))
                    .foregroundStyle(Theme.ink2)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Theme.well)
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
        .accessibilityHidden(true)
        .task(id: id) {
            guard hasImage, !id.isEmpty else { return }
            image = await MarkCache.shared.image(id: id, client: model.client)
        }
    }
}

/// Marks fetched this session, so a list does not download one per row on every redraw.
@MainActor
private final class MarkCache {
    static let shared = MarkCache()
    private var images: [String: UIImage] = [:]

    func image(id: String, client: SiloClient) async -> UIImage? {
        if let hit = images[id] { return hit }
        guard let data = try? await client.download(path: "/connectors/\(id)/image"),
              let image = UIImage(data: data) else { return nil }
        images[id] = image
        return image
    }
}

/// A rules section's icon: a connector's mark, else the glyph of the Bot page it is about.
struct RuleSectionMark: View {
    let section: Silo_V1_RuleSection

    var body: some View {
        if !section.connectorID.isEmpty {
            ConnectorMark(id: section.connectorID, hasImage: section.hasImage_p, size: 22)
        } else {
            Image(systemName: glyph)
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(Theme.ink2)
                .frame(width: 22, height: 22)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                .accessibilityHidden(true)
        }
    }

    private var glyph: String {
        switch section.id {
        case "bot": "cpu"
        case "secrets": "key"
        case "channels": "dot.radiowaves.left.and.right"
        default: "powerplug"
        }
    }
}
