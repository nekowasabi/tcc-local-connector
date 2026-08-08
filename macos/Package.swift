// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "TCCLocalConnector", platforms: [.macOS(.v14)], products: [.library(name: "ConnectorCore", targets: ["ConnectorCore"]), .executable(name: "TCCLocalConnector", targets: ["TCCLocalConnector"])], targets: [.target(name: "ConnectorCore"), .executableTarget(name: "TCCLocalConnector", dependencies: ["ConnectorCore"]), .testTarget(name: "ConnectorCoreTests", dependencies: ["ConnectorCore"]), .testTarget(name: "TCCLocalConnectorTests", dependencies: ["TCCLocalConnector"])])
