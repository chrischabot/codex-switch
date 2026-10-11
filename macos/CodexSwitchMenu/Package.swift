// swift-tools-version: 6.0

import PackageDescription

let package = Package(
    name: "CodexSwitchMenu",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "CodexSwitchMenu", targets: ["CodexSwitchMenu"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-protobuf.git", exact: "1.38.1"),
    ],
    targets: [
        .executableTarget(
            name: "CodexSwitchMenu",
            dependencies: [.product(name: "SwiftProtobuf", package: "swift-protobuf")],
            resources: [.copy("Resources")]
        ),
        .testTarget(
            name: "CodexSwitchMenuTests",
            dependencies: ["CodexSwitchMenu"]
        ),
    ]
)
