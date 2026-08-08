import AppKit
import XCTest
@testable import TCCLocalConnector

final class AppStoreTests: XCTestCase {
    func testLaunchWatcher_ImmediateTerminateOnMatch() {
        let notificationCenter = FakeNotificationCenter()
        let launchedApp = FakeLaunchApplication(bundleIdentifier: "com.example.app")
        let watcher = LaunchWatcher(center: notificationCenter) { notification in
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
