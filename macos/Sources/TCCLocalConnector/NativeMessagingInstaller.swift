import Darwin
import Foundation

struct NativeMessagingBrowser: Equatable {
    var id: String
    var hostResourceName: String
    var manifestName: String
    var manifestDirectory: String
    var description: String
    var allowedExtensions: [String]
    var allowedOrigins: [String]

    var isConfigured: Bool { !allowedExtensions.isEmpty || !allowedOrigins.isEmpty }
}

enum NativeMessagingCatalog {
    static let browsers: [NativeMessagingBrowser] = [
        NativeMessagingBrowser(
            id: "firefox",
            hostResourceName: "tcc-firefox-native-host",
            manifestName: "jp.takets.tcc_local_connector.firefox",
            manifestDirectory: "Mozilla/NativeMessagingHosts",
            description: "TCC Local Connector Firefox native messaging host",
            allowedExtensions: ["firefox-domain-blocker@tcc-local-connector.takets.jp"],
            allowedOrigins: []
        ),
    ]
}

struct NativeMessagingInstallReport: Equatable {
    var installed: [String] = []
    var skipped: [String] = []
    var failed: [String] = []
}

struct NativeMessagingInstaller {
    var applicationSupportURL: URL
    var appBundleURL: URL
    var resourcesURL: URL
    var browsers: [NativeMessagingBrowser]
    var fileManager: FileManager = .default

    @discardableResult
    static func registerFromAppBundle(
        bundle: Bundle = .main,
        fileManager: FileManager = .default
    ) -> Bool {
        guard let resourcesURL = bundle.resourceURL else { return true }
        let appBundleURL = resourcesURL.deletingLastPathComponent().deletingLastPathComponent()
        let supportURL = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        let report = NativeMessagingInstaller(
            applicationSupportURL: supportURL,
            appBundleURL: appBundleURL,
            resourcesURL: resourcesURL,
            browsers: NativeMessagingCatalog.browsers,
            fileManager: fileManager
        ).installPresentHosts()
        return report.failed.isEmpty
    }

    func installPresentHosts() -> NativeMessagingInstallReport {
        var report = NativeMessagingInstallReport()
        for browser in browsers {
            switch install(browser) {
            case .installed: report.installed.append(browser.id)
            case .skipped: report.skipped.append(browser.id)
            case .failed: report.failed.append(browser.id)
            }
        }
        return report
    }

    private enum Outcome { case installed, skipped, failed }

    private func install(_ browser: NativeMessagingBrowser) -> Outcome {
        guard browser.isConfigured else { return .skipped }
        guard let hostURL = resolveHost(named: browser.hostResourceName) else { return .skipped }
        do {
            try writeManifest(browser, hostPath: hostURL.path)
            return .installed
        } catch {
            return .failed
        }
    }

    private func resolveHost(named name: String) -> URL? {
        let hostURL = resourcesURL.appendingPathComponent(name)
        let requiredParent = appBundleURL
            .appendingPathComponent("Contents", isDirectory: true)
            .appendingPathComponent("Resources", isDirectory: true)
            .resolvingSymlinksInPath()
            .path
        let resolved = hostURL.resolvingSymlinksInPath().path
        guard resolved.hasPrefix(requiredParent + "/") else { return nil }
        guard let values = try? hostURL.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .isExecutableKey]),
              values.isRegularFile == true,
              values.isSymbolicLink != true,
              values.isExecutable == true else {
            return nil
        }
        let mode = (try? fileManager.attributesOfItem(atPath: hostURL.path)[.posixPermissions] as? NSNumber)?.intValue ?? 0
        guard mode & 0o777 == 0o700 else { return nil }
        return hostURL
    }

    private func writeManifest(_ browser: NativeMessagingBrowser, hostPath: String) throws {
        let directoryURL = applicationSupportURL.appendingPathComponent(browser.manifestDirectory, isDirectory: true)
        let manifestURL = directoryURL.appendingPathComponent(browser.manifestName + ".json")
        if fileManager.fileExists(atPath: manifestURL.path) {
            let existing = try manifestURL.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
            guard existing.isRegularFile == true, existing.isSymbolicLink != true else {
                throw CocoaError(.fileWriteUnknown)
            }
        }

        try fileManager.createDirectory(
            at: directoryURL,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )
        try fileManager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directoryURL.path)

        var object: [String: Any] = [
            "name": browser.manifestName,
            "description": browser.description,
            "path": hostPath,
            "type": "stdio",
        ]
        if !browser.allowedExtensions.isEmpty {
            object["allowed_extensions"] = browser.allowedExtensions
        }
        if !browser.allowedOrigins.isEmpty {
            object["allowed_origins"] = browser.allowedOrigins
        }
        let data = try JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys])
        let temporaryURL = directoryURL.appendingPathComponent(".\(browser.manifestName).json.\(UUID().uuidString)")
        guard fileManager.createFile(atPath: temporaryURL.path, contents: data, attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporaryURL.path)
        // Why: POSIX rename instead of copy+replace. Reason: Firefox/Chrome must not observe a partial manifest.
        guard rename(temporaryURL.path, manifestURL.path) == 0 else {
            try? fileManager.removeItem(at: temporaryURL)
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
    }
}
