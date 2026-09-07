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
        XCTAssertEqual(LaunchLockNotification.title, "Locked")
        XCTAssertTrue(LaunchLockNotification.body(bundleID: "com.example.app").contains("locked"))
    }

    func testTerminalNotifierLockNotifier_PostsLockedSystemNotification() {
        let runner = FakeTerminalNotifierRunner()
        let notifier = TerminalNotifierLockNotifier(runner: runner)

        notifier.notifyLocked(bundleID: "com.example.app")

        XCTAssertEqual(runner.invocations.count, 1)
        XCTAssertEqual(runner.invocations.first, [
            "-title", LaunchLockNotification.title,
            "-message", LaunchLockNotification.body(bundleID: "com.example.app"),
        ])
        XCTAssertTrue((runner.invocations.first ?? []).contains { $0.contains("locked") })
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

    func notifyLocked(bundleID: String) {
        bundleIDs.append(bundleID)
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
