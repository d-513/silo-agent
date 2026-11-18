import SwiftUI
import UIKit
import VisionKit

/// The system's document camera: edges found, perspective fixed, pages kept. What it gives back is
/// one PDF, page per scan, which the Bot reads like any upload (Knowledge's tools OCR a scan).
struct DocumentScanner: UIViewControllerRepresentable {
    let done: (Data?) -> Void

    static var isAvailable: Bool { VNDocumentCameraViewController.isSupported }

    func makeUIViewController(context: Context) -> VNDocumentCameraViewController {
        let camera = VNDocumentCameraViewController()
        camera.delegate = context.coordinator
        return camera
    }

    func updateUIViewController(_ controller: VNDocumentCameraViewController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(done: done) }

    final class Coordinator: NSObject, VNDocumentCameraViewControllerDelegate {
        let done: (Data?) -> Void

        init(done: @escaping (Data?) -> Void) { self.done = done }

        func documentCameraViewController(_ controller: VNDocumentCameraViewController, didFinishWith scan: VNDocumentCameraScan) {
            let pages = (0..<scan.pageCount).map { scan.imageOfPage(at: $0) }
            done(Self.pdf(pages))
        }

        func documentCameraViewControllerDidCancel(_ controller: VNDocumentCameraViewController) { done(nil) }

        func documentCameraViewController(_ controller: VNDocumentCameraViewController, didFailWithError error: Error) { done(nil) }

        /// Each page at its own size, JPEG-compressed so a ten-page scan stays a few MB.
        static func pdf(_ pages: [UIImage]) -> Data? {
            guard let first = pages.first else { return nil }
            let renderer = UIGraphicsPDFRenderer(bounds: CGRect(origin: .zero, size: first.size))
            return renderer.pdfData { context in
                for page in pages {
                    let bounds = CGRect(origin: .zero, size: page.size)
                    context.beginPage(withBounds: bounds, pageInfo: [:])
                    let compact = page.jpegData(compressionQuality: 0.72).flatMap(UIImage.init(data:)) ?? page
                    compact.draw(in: bounds)
                }
            }
        }
    }
}
