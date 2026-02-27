// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Shen",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(
            name: "Shen",
            path: "Shen"
        ),
    ]
)
