import AppKit
import XCTest
@testable import TCCLocalConnector

final class LaunchWatcherTests: XCTestCase {
    func testLaunchWatcher_ImmediateTerminateOnMatch() {
        let notificationCenter = FakeNotificationCenter()
        let launchedApp = FakeLaunchApplication(bundleIdentifier: "com.example.app")
        let lockNotifier = FakeLockNotifier()
        let watcher = LaunchWatcher(center: notificationCenter, lockNotifier: lockNotifier) { notification in
            notification.userInfo?[NSWorkspace.applicationUserInfoKey] as? FakeLaunchApplication
        }

        watcher.update(enforceStopBundleIDs: ["com.example.app"])
        notificationCenter.send(
            Notification(
                name: NSWorkspace.didLaunchApplicationNotification,
                object: nil,
                userInfo: [NSWorkspace.applicationUserInfoKey: launchedApp]
            )
        )

        XCTAssertTrue(launchedApp.didTerminate)
        XCTAssertEqual(lockNotifier.bundleIDs, ["com.example.app"])
        XCTAssertEqual(LaunchLockNotification.title, "ロック中")
        XCTAssertTrue(LaunchLockNotification.body(bundleID: "com.example.app").contains("止めています"))
    }

    func testTerminalNotifierLockNotifier_PostsLockedSystemNotification() {
        let runner = FakeTerminalNotifierRunner()
        let notifier = TerminalNotifierLockNotifier(runner: runner)

        notifier.notifyLocked(bundleID: "com.example.app", title: nil, message: nil)

        XCTAssertEqual(runner.invocations.count, 1)
        XCTAssertEqual(runner.invocations.first, [
            "-title", LaunchLockNotification.title,
            "-subtitle", "com.example.app",
            "-message", LaunchLockNotification.body(bundleID: "com.example.app"),
        ])
        XCTAssertTrue((runner.invocations.first ?? []).contains { $0.contains("止めています") })
    }

    func testTerminalNotifierLockNotifier_UsesRuleNotice() {
        let runner = FakeTerminalNotifierRunner()
        let notifier = TerminalNotifierLockNotifier(runner: runner)

        notifier.notifyLocked(bundleID: "com.example.app", title: "ロック中: sns-gate", message: "SNS タスク以外は止める")

        XCTAssertEqual(runner.invocations.first, [
            "-title", "ロック中: sns-gate",
            "-subtitle", "com.example.app",
            "-message", "SNS タスク以外は止める",
        ])
    }

    func testLaunchWatcher_PassesRuleNoticeOnTerminate() {
        let notificationCenter = FakeNotificationCenter()
        let launchedApp = FakeLaunchApplication(bundleIdentifier: "com.example.app")
        let lockNotifier = FakeLockNotifier()
        let watcher = LaunchWatcher(center: notificationCenter, lockNotifier: lockNotifier) { notification in
            notification.userInfo?[NSWorkspace.applicationUserInfoKey] as? FakeLaunchApplication
        }
        let notice = LockNotice(title: "ロック中: sns-gate", message: "説明")

        watcher.update(enforceStopBundleIDs: ["com.example.app"], notices: ["com.example.app": notice])
        notificationCenter.send(
            Notification(
                name: NSWorkspace.didLaunchApplicationNotification,
                object: nil,
                userInfo: [NSWorkspace.applicationUserInfoKey: launchedApp]
            )
        )

        XCTAssertEqual(lockNotifier.notices, [notice])
    }

    func testLaunchWatcher_NonMatchingLaunchDoesNotTerminateOrNotify() {
        let notificationCenter = FakeNotificationCenter()
        let launchedApp = FakeLaunchApplication(bundleIdentifier: "com.other.app")
        let lockNotifier = FakeLockNotifier()
        let watcher = LaunchWatcher(center: notificationCenter, lockNotifier: lockNotifier) { notification in
            notification.userInfo?[NSWorkspace.applicationUserInfoKey] as? FakeLaunchApplication
        }

        watcher.update(enforceStopBundleIDs: ["com.example.app"])
        notificationCenter.send(
            Notification(
                name: NSWorkspace.didLaunchApplicationNotification,
                object: nil,
                userInfo: [NSWorkspace.applicationUserInfoKey: launchedApp]
            )
        )

        XCTAssertFalse(launchedApp.didTerminate)
        XCTAssertEqual(lockNotifier.bundleIDs, [])
    }
}

private final class FakeTerminalNotifierRunner: TerminalNotifierRunning {
    private(set) var invocations: [[String]] = []

    func run(arguments: [String]) {
        invocations.append(arguments)
    }
}

private final class FakeLockNotifier: LaunchLockNotifier {
    private(set) var bundleIDs: [String] = []

    private(set) var notices: [LockNotice] = []

    func notifyLocked(bundleID: String, title: String?, message: String?) {
        bundleIDs.append(bundleID)
        notices.append(LockNotice(title: title, message: message))
    }
}

private final class FakeNotificationCenter: LaunchWatchCenter {
    private var observers: [ObjectIdentifier: (observer: WeakContainer, selector: Selector)] = [:]

    func addObserver(_ observer: AnyObject, selector: Selector, name: NSNotification.Name?, object: Any?) {
        observers[ObjectIdentifier(observer)] = (WeakContainer(observer), selector)
    }

    func removeObserver(_ observer: AnyObject) {
        observers.removeValue(forKey: ObjectIdentifier(observer))
    }

    func send(_ notification: Notification) {
        for entry in observers.values {
            entry.observer.value?.perform(entry.selector, with: notification)
        }
    }
}

private final class WeakContainer {
    weak var value: NSObject?

    init(_ value: AnyObject) {
        self.value = value as? NSObject
    }
}

private final class FakeLaunchApplication: LaunchObservedApplication {
    let bundleIdentifier: String?
    private(set) var didTerminate = false

    init(bundleIdentifier: String) {
        self.bundleIdentifier = bundleIdentifier
    }

    func terminate() -> Bool {
        didTerminate = true
        return true
    }
}
