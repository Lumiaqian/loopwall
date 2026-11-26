// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Platform",
    platforms: [.macOS(.v13)],
    products: [.library(name: "Platform", type: .dynamic, targets: ["Platform"])],
    dependencies: [
        .package(url: "https://github.com/google/nearby.git", revision: "02ebb956776f8357f544daddf60f99aeb2e3e4a4"),
    ],
    targets: [
        .target(name: "Platform", dependencies: [.product(name: "NearbyConnections", package: "nearby")]),
    ],
    swiftLanguageModes: [.v5],
)
