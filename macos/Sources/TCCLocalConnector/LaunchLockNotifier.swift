import Foundation

protocol LaunchLockNotifier: AnyObject {
    func notifyLocked(bundleID: String)
}

protocol TerminalNotifierRunning {
    func run(arguments: [String])
}

enum LaunchLockNotification {
    static let title = "Locked"
    static func body(bundleID: String) -> String {
        "\(bundleID) is locked while a matching task is running"
    }
}

final class ProcessTerminalNotifierRunner: TerminalNotifierRunning {
    func run(arguments: [String]) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["terminal-notifier"] + arguments
        // Why: a GUI-launched app inherits launchd's PATH (/usr/bin:/bin:...), which lacks Homebrew, so env could not find terminal-notifier and the failure was silent.
        var environment = ProcessInfo.processInfo.environment
        let basePath = environment["PATH"] ?? "/usr/bin:/bin:/usr/sbin:/sbin"
        environment["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + basePath
        process.environment = environment
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try? process.run()
    }
}

final class TerminalNotifierLockNotifier: LaunchLockNotifier {
    private let runner: TerminalNotifierRunning

    init(runner: TerminalNotifierRunning = ProcessTerminalNotifierRunner()) {
        self.runner = runner
    }

    func notifyLocked(bundleID: String) {
        // Why: terminal-notifier instead of UNUserNotificationCenter — MenuBarExtra is treated as foreground and suppresses banners.
        runner.run(arguments: [
            "-title", LaunchLockNotification.title,
            "-message", LaunchLockNotification.body(bundleID: bundleID),
        ])
    }
}
