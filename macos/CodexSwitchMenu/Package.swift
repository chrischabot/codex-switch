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
        .package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.9.4"),
    ],
    targets: [
        .executableTarget(
            name: "CodexSwitchMenu",
            dependencies: [
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
                .product(name: "Sparkle", package: "Sparkle"),
            ],
            resources: [.copy("Resources")],
            linkerSettings: [.unsafeFlags([
                "-Xlinker", "-rpath", "-Xlinker", "@executable_path/../Frameworks",
            ])]
        ),
        .testTarget(
            name: "CodexSwitchMenuTests",
            dependencies: ["CodexSwitchMenu"],
            linkerSettings: [.unsafeFlags([
                "-Xlinker", "-rpath", "-Xlinker",
                "@loader_path/../../../../../../artifacts/sparkle/Sparkle/Sparkle.xcframework/macos-arm64_x86_64",
            ])]
        ),
    ]
)
