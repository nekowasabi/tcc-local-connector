import AppKit
import Darwin
import Foundation

@MainActor
final class BrowserPolicyHeartbeat: NSObject {
    static let policyVersion = 1
    static let intervalSeconds: TimeInterval = 5
    static let directoryRelativePath = "TCCLocalConnector/BrowserPolicy"
    static let fileName = "firefox-owner-heartbeat.json"

    private struct Payload: Encodable {
        let version: Int
        let updatedAt: String

        enum CodingKeys: String, CodingKey {
            case version
            case updatedAt = "updated_at"
        }
    }

    private let fileManager: FileManager
    private let applicationSupportURL: URL
    private let now: () -> Date
    private var timer: Timer?
    private var generation = 0
    private var isActive = false

    var heartbeatURL: URL {
        applicationSupportURL
            .appendingPathComponent(Self.directoryRelativePath, isDirectory: true)
            .appendingPathComponent(Self.fileName)
    }

    init(
        applicationSupportURL: URL? = nil,
        fileManager: FileManager = .default,
        now: @escaping () -> Date = Date.init
    ) {
        self.fileManager = fileManager
        self.applicationSupportURL = applicationSupportURL
            ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        self.now = now
        super.init()
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(applicationWillTerminate),
            name: NSApplication.willTerminateNotification,
            object: nil
        )
    }

    deinit {
        NotificationCenter.default.removeObserver(self)
    }

    @discardableResult
    func startOrRefresh() -> Bool {
        generation += 1
        let currentGeneration = generation
        timer?.invalidate()
        timer = nil
        isActive = true

        guard writeHeartbeat() else {
            stop()
            return false
        }

        timer = Timer.scheduledTimer(withTimeInterval: Self.intervalSeconds, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.refresh(generation: currentGeneration)
            }
        }
        return true
    }

    func stop() {
        generation += 1
        isActive = false
        timer?.invalidate()
        timer = nil
        try? fileManager.removeItem(at: heartbeatURL)
    }

    func refreshForTesting() {
        refresh(generation: generation)
    }

    private func refresh(generation expectedGeneration: Int) {
        guard isActive, generation == expectedGeneration else { return }
        if !writeHeartbeat() {
            stop()
        }
    }

    private func writeHeartbeat() -> Bool {
        let directoryURL = heartbeatURL.deletingLastPathComponent()
        let temporaryURL = directoryURL.appendingPathComponent(".heartbeat-\(UUID().uuidString).tmp")
        do {
            try fileManager.createDirectory(
                at: directoryURL,
                withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700]
            )
            try fileManager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directoryURL.path)

            let payload = Payload(version: Self.policyVersion, updatedAt: Self.rfc3339Nano(now()))
            let data = try JSONEncoder().encode(payload)
            guard fileManager.createFile(
                atPath: temporaryURL.path,
                contents: data,
                attributes: [.posixPermissions: 0o600]
            ) else {
                throw CocoaError(.fileWriteUnknown)
            }
            try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporaryURL.path)
            // Why: POSIX rename handles both first creation and replacement atomically in one operation.
            guard rename(temporaryURL.path, heartbeatURL.path) == 0 else {
                throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
            }
            return true
        } catch {
            try? fileManager.removeItem(at: temporaryURL)
            try? fileManager.removeItem(at: heartbeatURL)
            return false
        }
    }

    private static func rfc3339Nano(_ date: Date) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "en_US_POSIX")
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        let components = calendar.dateComponents(
            [.year, .month, .day, .hour, .minute, .second, .nanosecond],
            from: date
        )
        return String(
            format: "%04d-%02d-%02dT%02d:%02d:%02d.%09dZ",
            locale: Locale(identifier: "en_US_POSIX"),
            components.year!,
            components.month!,
            components.day!,
            components.hour!,
            components.minute!,
            components.second!,
            components.nanosecond!
        )
    }

    @objc private func applicationWillTerminate() {
        stop()
    }
}
