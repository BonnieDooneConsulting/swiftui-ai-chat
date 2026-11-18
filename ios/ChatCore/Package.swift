// swift-tools-version: 6.3
import PackageDescription

let package = Package(
    name: "ChatCore",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [
        .library(name: "ChatCore", targets: ["ChatCore"]),
    ],
    targets: [
        .target(name: "ChatCore"),
        .testTarget(name: "ChatCoreTests", dependencies: ["ChatCore"]),
    ]
)
