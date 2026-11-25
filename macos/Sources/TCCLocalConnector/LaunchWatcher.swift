import AppKit

protocol LaunchObservedApplication {
    var bundleIdentifier: String? { get }
    func terminate() -> Bool
}

protocol LaunchWatchCenter {
    func addObserver(_ observer: AnyObject, selector: Selector, name: NSNotification.Name?, object: Any?)
    func removeObserver(_ observer: AnyObject)
}

private final class DefaultLaunchWatchCenter: LaunchWatchCenter {
    private let center = NSWorkspace.shared.notificationCenter

    func addObserver(_ observer: AnyObject, selector: Selector, name: NSNotification.Name?, object: Any?) {
        center.addObserver(observer, selector: selector, name: name, object: object)
    }

    func removeObserver(_ observer: AnyObject) {
        center.removeObserver(observer)
    }
}

private struct DefaultObservedApplication: LaunchObservedApplication {
    private let app: NSRunningApplication

    init(_ app: NSRunningApplication) {
        self.app = app
    }

    var bundleIdentifier: String? {
        app.bundleIdentifier
    }

    func terminate() -> Bool {
        app.terminate()
    }
}

@objc final class LaunchWatcher: NSObject {
    private let center: LaunchWatchCenter
    private let appFromNotification: (Notification) -> LaunchObservedApplication?
    private let lockNotifier: LaunchLockNotifier

    init(
        center: LaunchWatchCenter = DefaultLaunchWatchCenter(),
        lockNotifier: LaunchLockNotifier = TerminalNotifierLockNotifier(),
        appFromNotification: @escaping (Notification) -> LaunchObservedApplication? = {
            ($0.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication).map(DefaultObservedApplication.init)
        }
    ) {
        self.center = center
        self.lockNotifier = lockNotifier
        self.appFromNotification = appFromNotification
    }

    func update(enforceStopBundleIDs: Set<String>, notices: [String: LockNotice] = [:]) {
        stop()
        center.addObserver(
            self,
            selector: #selector(handleLaunch(_:)),
            name: NSWorkspace.didLaunchApplicationNotification,
            object: nil
        )
        self.enforceStopBundleIDs = enforceStopBundleIDs
        self.notices = notices
        isActive = true
    }

    @objc private func handleLaunch(_ notification: Notification) {
        guard let app = appFromNotification(notification),
              let bundleID = app.bundleIdentifier,
              enforceStopBundleIDs.contains(bundleID) else {
            return
        }
        _ = app.terminate()
        let notice = notices[bundleID]
        lockNotifier.notifyLocked(bundleID: bundleID, title: notice?.title, message: notice?.message)
    }

    func stop() {
        if isActive {
            center.removeObserver(self)
            isActive = false
            enforceStopBundleIDs = []
            notices = [:]
        }
    }

    deinit {
        stop()
    }

    private var enforceStopBundleIDs: Set<String> = []
    private var notices: [String: LockNotice] = [:]
    private var isActive = false
}

struct LockNotice: Equatable {
    let title: String?
    let message: String?
}
