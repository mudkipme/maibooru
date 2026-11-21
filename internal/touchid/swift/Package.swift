// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Touchid",
    platforms: [.macOS(.v13)],
    products: [.library(name: "Touchid", type: .dynamic, targets: ["Touchid"])],
    dependencies: [],
    targets: [
        .target(name: "Touchid"),
    ],
    swiftLanguageModes: [.v5]
)
