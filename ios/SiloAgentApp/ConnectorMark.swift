import SiloClient
import SwiftUI
import UIKit

/// A connector's own mark (`/connectors/<id>/image`, cookie auth), or the plug when it has none. A
/// logo is drawn for a light page, so in the dark it keeps a paper tile: a 2pt halo, so nothing moves.
struct ConnectorMark: View {
    @Environment(AppModel.self) private var model
    @Environment(\.colorScheme) private var scheme
    let id: String
    let hasImage: Bool
    var size: CGFloat = 28
    @State private var image: UIImage?
    @State private var missing = false

    private var shape: RoundedRectangle {
        RoundedRectangle(cornerRadius: size >= 28 ? Theme.Radius.sm : Theme.Radius.xs, style: .continuous)
    }

    var body: some View {
        ZStack {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
                    .frame(width: size, height: size)
                    .background(scheme == .dark ? Theme.onHatch : .clear)
                    .clipShape(shape)
                    .background { if scheme == .dark { shape.inset(by: -2).fill(Theme.onHatch) } }
                    .transition(.opacity)
            } else {
                // A mark on its way reserves the tile; the plug is only for a connector with none.
                shape.fill(Theme.well)
                if !hasImage || missing {
                    Image(systemName: "powerplug")
                        .font(.system(size: size * 0.45))
                        .foregroundStyle(Theme.ink3)
                        .transition(.opacity)
                }
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
        .task(id: id) {
            guard hasImage, !id.isEmpty else { return }
            if let held = MarkCache.shared.held(id) {
                image = held
                return
            }
            let fetched = await MarkCache.shared.image(id: id, client: model.client)
            withAnimation(Motion.quiet(0.2)) {
                image = fetched
                missing = fetched == nil
            }
        }
    }
}

/// Marks fetched this session, so a list does not download one per row on every redraw.
@MainActor
private final class MarkCache {
    static let shared = MarkCache()
    private var images: [String: UIImage] = [:]

    func held(_ id: String) -> UIImage? { images[id] }

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
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
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
