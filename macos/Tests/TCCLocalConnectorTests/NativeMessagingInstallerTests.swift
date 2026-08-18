import Foundation
import XCTest
@testable import TCCLocalConnector

final class NativeMessagingInstallerTests: XCTestCase {
    func testCatalogSkipsUnconfiguredChromeAndInstallsFirefox() throws {
        let env = try makeEnvironment()
        let report = NativeMessagingInstaller(
            applicationSupportURL: env.support,
            appBundleURL: env.app,
            resourcesURL: env.resources,
            browsers: NativeMessagingCatalog.browsers
        ).installPresentHosts()

        XCTAssertEqual(report.installed, ["firefox"])
        XCTAssertEqual(report.skipped, ["chrome"])
        XCTAssertEqual(report.failed, [])

        let manifest = try readManifest(env.support
            .appendingPathComponent("Mozilla/NativeMessagingHosts/jp.takets.tcc_local_connector.firefox.json"))
        XCTAssertEqual(manifest["name"] as? String, "jp.takets.tcc_local_connector.firefox")
        XCTAssertEqual(manifest["type"] as? String, "stdio")
        XCTAssertEqual(manifest["path"] as? String, env.host.path)
        XCTAssertEqual(manifest["allowed_extensions"] as? [String], [
            "firefox-domain-blocker@tcc-local-connector.takets.jp",
        ])
        XCTAssertNil(manifest["allowed_origins"])
        XCTAssertFalse(FileManager.default.fileExists(atPath: env.support
            .appendingPathComponent("Google/Chrome/NativeMessagingHosts/jp.takets.tcc_local_connector.chrome.json").path))

        let directoryPermissions = try XCTUnwrap(
            FileManager.default.attributesOfItem(
                atPath: env.support.appendingPathComponent("Mozilla/NativeMessagingHosts").path
            )[.posixPermissions] as? NSNumber
        )
        let filePermissions = try XCTUnwrap(
            FileManager.default.attributesOfItem(
                atPath: env.support.appendingPathComponent(
                    "Mozilla/NativeMessagingHosts/jp.takets.tcc_local_connector.firefox.json"
                ).path
            )[.posixPermissions] as? NSNumber
        )
        XCTAssertEqual(directoryPermissions.intValue & 0o777, 0o700)
        XCTAssertEqual(filePermissions.intValue & 0o777, 0o600)
    }

    func testConfiguredChromeWritesAllowedOriginsWhenHostExists() throws {
        let env = try makeEnvironment()
        var chrome = NativeMessagingCatalog.browsers.first { $0.id == "chrome" }!
        chrome.allowedOrigins = ["chrome-extension://abcdefghijklmnopqrstuvwxyzabcdef/"]
        let report = NativeMessagingInstaller(
            applicationSupportURL: env.support,
            appBundleURL: env.app,
            resourcesURL: env.resources,
            browsers: [chrome]
        ).installPresentHosts()

        XCTAssertEqual(report, NativeMessagingInstallReport(installed: ["chrome"], skipped: [], failed: []))
        let manifest = try readManifest(env.support
            .appendingPathComponent("Google/Chrome/NativeMessagingHosts/jp.takets.tcc_local_connector.chrome.json"))
        XCTAssertEqual(manifest["allowed_origins"] as? [String], [
            "chrome-extension://abcdefghijklmnopqrstuvwxyzabcdef/",
        ])
        XCTAssertNil(manifest["allowed_extensions"])
        XCTAssertEqual(manifest["path"] as? String, env.host.path)
    }

    func testMissingHostIsSkipped() throws {
        let env = try makeEnvironment()
        try FileManager.default.removeItem(at: env.host)
        let report = NativeMessagingInstaller(
            applicationSupportURL: env.support,
            appBundleURL: env.app,
            resourcesURL: env.resources,
            browsers: NativeMessagingCatalog.browsers
        ).installPresentHosts()
        XCTAssertEqual(report.installed, [])
        XCTAssertEqual(Set(report.skipped), Set(["firefox", "chrome"]))
        XCTAssertEqual(report.failed, [])
    }

    func testHostOutsideAppBundleIsSkipped() throws {
        let env = try makeEnvironment()
        let outside = try makeTemporaryDirectory().appendingPathComponent("tcc-firefox-native-host")
        XCTAssertTrue(FileManager.default.createFile(atPath: outside.path, contents: Data(), attributes: [.posixPermissions: 0o700]))
        let report = NativeMessagingInstaller(
            applicationSupportURL: env.support,
            appBundleURL: env.app,
            resourcesURL: outside.deletingLastPathComponent(),
            browsers: [NativeMessagingCatalog.browsers[0]]
        ).installPresentHosts()
        XCTAssertEqual(report.skipped, ["firefox"])
        XCTAssertEqual(report.installed, [])
    }

    private struct Environment {
        var support: URL
        var app: URL
        var resources: URL
        var host: URL
    }

    private func makeEnvironment() throws -> Environment {
        let root = try makeTemporaryDirectory()
        let app = root.appendingPathComponent("TCCLocalConnector.app")
        let resources = app.appendingPathComponent("Contents/Resources")
        try FileManager.default.createDirectory(at: resources, withIntermediateDirectories: true)
        let host = resources.appendingPathComponent("tcc-firefox-native-host")
        XCTAssertTrue(FileManager.default.createFile(atPath: host.path, contents: Data("#".utf8), attributes: [.posixPermissions: 0o700]))
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: host.path)
        return Environment(support: root.appendingPathComponent("Application Support"), app: app, resources: resources, host: host)
    }

    private func readManifest(_ url: URL) throws -> [String: Any] {
        let object = try JSONSerialization.jsonObject(with: Data(contentsOf: url))
        return try XCTUnwrap(object as? [String: Any])
    }

    private func makeTemporaryDirectory() throws -> URL {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("NativeMessagingInstallerTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        return directory
    }
}
