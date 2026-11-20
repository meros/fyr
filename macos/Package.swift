// swift-tools-version:5.9
// Fyr.app's shell (decision 0037): the menu bar, the window and the agent's
// registration. build.sh puts it into a bundle with fyr-host, fyr and fyrd.
import PackageDescription

let package = Package(
  name: "Fyr",
  platforms: [.macOS(.v13)],
  targets: [
    .executableTarget(name: "Fyr", path: "Sources/Fyr")
  ]
)
