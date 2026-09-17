import AppKit
import ConnectorCore

protocol RunningApplication: Sendable {
    var isTerminated: Bool { get }
    func terminate() -> Bool
}

protocol LaunchWorkspace: Sendable {
    func applicationURL(bundleID: String) -> URL?
    func runningApplications(bundleID: String) -> [RunningApplication]
    func openApplication(at url: URL) async -> Error?
    func terminate(bundleID: String)
}

struct DefaultRunningApplication: RunningApplication, @unchecked Sendable {
    private let app: NSRunningApplication

    init(_ app: NSRunningApplication) {
        self.app = app
    }

    var isTerminated: Bool {
        app.isTerminated
    }

    func terminate() -> Bool {
        app.terminate()
    }
}

struct DefaultLaunchWorkspace: LaunchWorkspace {
    func applicationURL(bundleID: String) -> URL? {
        NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID)
    }

    func runningApplications(bundleID: String) -> [RunningApplication] {
        NSWorkspace.shared.runningApplications
            .filter { $0.bundleIdentifier == bundleID && !$0.isTerminated }
            .map(DefaultRunningApplication.init)
    }

    func openApplication(at url: URL) async -> Error? {
        await withCheckedContinuation { continuation in
            NSWorkspace.shared.openApplication(at: url, configuration: .init()) { _, error in
                continuation.resume(returning: error)
            }
        }
    }

    func terminate(bundleID: String) {
        NSWorkspace.shared.runningApplications
            .filter { $0.bundleIdentifier == bundleID && !$0.isTerminated }
            .forEach { _ = $0.terminate() }
    }
}

final class MacPlanExecutor: PlanExecutor, @unchecked Sendable {
    private let workspace: LaunchWorkspace
    private let startTimeoutSeconds: TimeInterval

    init(
        workspace: LaunchWorkspace = DefaultLaunchWorkspace(),
        startTimeoutSeconds: TimeInterval = TimeInterval(Constants.backendRequestTimeoutSeconds)
    ) {
        self.workspace = workspace
        self.startTimeoutSeconds = startTimeoutSeconds
    }

    func execute(_ actions: [PlanAction], dryRun: Bool) async -> [ActionOutcome] {
        var outcomes: [ActionOutcome] = []
        outcomes.reserveCapacity(actions.count)
        for action in actions {
            if dryRun {
                outcomes.append(.skipped)
                continue
            }
            switch action.type {
            case "app.start":
                outcomes.append(await startApp(action))
            case "app.stop":
                // Why: Keep going after quit_refused instead of aborting later ensure app.stop entries.
                outcomes.append(await stopApp(action))
            case "notify":
                outcomes.append(await notify(action))
            default:
                outcomes.append(.rejected("unsupported_action"))
            }
        }
        return outcomes
    }

    private func startApp(_ action: PlanAction) async -> ActionOutcome {
        guard let bundleID = action.bundleID else {
            return .rejected("invalid_action")
        }
        guard let url = workspace.applicationURL(bundleID: bundleID) else {
            return .rejected("app_not_found")
        }
        guard workspace.runningApplications(bundleID: bundleID).isEmpty else {
            return .skipped
        }

        let workspace = workspace
        let started = await withTimeout(seconds: startTimeoutSeconds) {
            await workspace.openApplication(at: url) == nil
        }
        guard let started else {
            return .rejected("timeout")
        }
        return started ? .accepted : .rejected("launch_failed")
    }

    private func stopApp(_ action: PlanAction) async -> ActionOutcome {
        guard let bundleID = action.bundleID else {
            return .rejected("invalid_action")
        }
        guard !workspace.runningApplications(bundleID: bundleID).isEmpty else {
            return .skipped
        }

        workspace.terminate(bundleID: bundleID)

        let grace = max(1, action.graceSeconds ?? 10)
        let deadline = Date().addingTimeInterval(TimeInterval(grace))
        while Date() < deadline {
            if workspace.runningApplications(bundleID: bundleID).isEmpty {
                return .accepted
            }
            do {
                try await Task.sleep(for: .milliseconds(Constants.appStopPollIntervalMilliseconds))
            } catch {
                return .rejected("cancelled")
            }
        }
        return .rejected("quit_refused")
    }

    private func notify(_ action: PlanAction) async -> ActionOutcome {
        return .accepted
    }

    private func withTimeout(seconds: TimeInterval, _ operation: @escaping @Sendable () async -> Bool) async -> Bool? {
        return await withTaskGroup(of: Bool?.self) { group in
            group.addTask {
                return await operation()
            }
            group.addTask {
                try? await Task.sleep(for: .seconds(seconds))
                return nil
            }
            let result = await group.next() ?? nil
            group.cancelAll()
            return result
        }
    }
}
