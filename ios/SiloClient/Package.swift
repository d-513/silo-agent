// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "SiloClient",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        .library(name: "SiloClient", targets: ["SiloClient"]),
        // Calls: WebRTC and the voice detector. Only the app links it; the extensions stay small.
        .library(name: "SiloCall", targets: ["SiloCall"]),
    ],
    dependencies: [
        .package(url: "https://github.com/connectrpc/connect-swift.git", from: "1.2.3"),
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.37.0"),
        .package(url: "https://github.com/stasel/WebRTC.git", exact: "156.0.0"),
        .package(url: "https://github.com/microsoft/onnxruntime-swift-package-manager.git", exact: "1.19.2"),
    ],
    targets: [
        .target(
            name: "SiloClient",
            dependencies: [
                .product(name: "Connect", package: "connect-swift"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ]
        ),
        .target(
            name: "SiloCall",
            dependencies: [
                "SiloClient",
                .product(name: "WebRTC", package: "WebRTC"),
                .product(name: "onnxruntime", package: "onnxruntime-swift-package-manager"),
            ],
            resources: [.copy("silero_vad_v5.onnx")]
        ),
        .testTarget(name: "SiloClientTests", dependencies: ["SiloClient"]),
    ]
)
