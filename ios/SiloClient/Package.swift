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
    ],
    dependencies: [
        .package(url: "https://github.com/connectrpc/connect-swift.git", from: "1.2.3"),
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.37.0"),
    ],
    targets: [
        .target(
            name: "SiloClient",
            dependencies: [
                .product(name: "Connect", package: "connect-swift"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ]
        ),
    ]
)
