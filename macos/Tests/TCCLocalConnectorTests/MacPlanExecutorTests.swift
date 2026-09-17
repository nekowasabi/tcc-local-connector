import XCTest
import ConnectorCore
@testable import TCCLocalConnector

final class MacPlanExecutorTests: XCTestCase {
    func testStartApp_LaunchesWhenNotRunning() async {
        let workspace = FakeLaunchWorkspace()
        let action = PlanAction(type: "app.start", id: "1-1", bundleID: "com.example.app")
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.accepted])
        XCTAssertEqual(workspace.openedBundleIDs, ["com.example.app"])
    }

    func testStartApp_SkipsWhenAlreadyRunning() async {
        let app = FakeRunningApplication(bundleID: "com.example.app")
        let workspace = FakeLaunchWorkspace(runningApplications: ["com.example.app": [app]])
        let action = PlanAction(type: "app.start", id: "1-1", bundleID: "com.example.app")
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.skipped])
        XCTAssertEqual(workspace.openedBundleIDs.count, 0)
    }

    func testStartApp_FailedAppNotFound() async {
        let workspace = FakeLaunchWorkspace()
        let action = PlanAction(type: "app.start", id: "1-1", bundleID: "missing.bundle")
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.rejected("app_not_found")])
    }

    func testStartApp_Timeout() async {
        let action = PlanAction(type: "app.start", id: "1-1", bundleID: "com.example.app")
        let workspace = FakeLaunchWorkspace(openShouldTimeout: true)
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.rejected("timeout")])
    }

    func testStopApp_SkipsWhenNotRunning() async {
        let action = PlanAction(type: "app.stop", id: "1-1", bundleID: "com.example.app")
        let executor = MacPlanExecutor(startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.skipped])
    }

    func testStopApp_GracefulTermination() async {
        let app = FakeRunningApplication(bundleID: "com.example.app")
        app.shouldTerminate = true
        let workspace = FakeLaunchWorkspace(runningApplications: ["com.example.app": [app]])
        let action = PlanAction(type: "app.stop", id: "1-1", bundleID: "com.example.app", graceSeconds: 1)
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.accepted])
        XCTAssertTrue(app.wasTerminated)
    }

    func testStopApp_AttemptsLaterTargetAfterFirstQuitRefused() async {
        let slack = FakeRunningApplication(bundleID: "com.tinyspeck.slackmacgap")
        slack.shouldTerminate = false
        let kindle = FakeRunningApplication(bundleID: "com.amazon.Lassen")
        kindle.shouldTerminate = true
        let workspace = FakeLaunchWorkspace(runningApplications: [
            "com.tinyspeck.slackmacgap": [slack],
            "com.amazon.Lassen": [kindle],
        ])
        let actions = [
            PlanAction(type: "app.stop", id: "1-1", bundleID: "com.tinyspeck.slackmacgap", graceSeconds: 1),
            PlanAction(type: "app.stop", id: "1-2", bundleID: "com.amazon.Lassen", graceSeconds: 1),
        ]
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute(actions, dryRun: false)

        XCTAssertEqual(outcomes, [.rejected("quit_refused"), .accepted])
        XCTAssertEqual(workspace.terminatedBundleIDs, ["com.tinyspeck.slackmacgap", "com.amazon.Lassen"])
        XCTAssertTrue(kindle.wasTerminated)
    }

    func testStopApp_RefusedNoRetry() async {
        let app = FakeRunningApplication(bundleID: "com.example.app")
        app.shouldTerminate = false
        let workspace = FakeLaunchWorkspace(runningApplications: ["com.example.app": [app]])
        let action = PlanAction(type: "app.stop", id: "1-1", bundleID: "com.example.app", graceSeconds: 1)
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.rejected("quit_refused")])
        XCTAssertFalse(app.wasTerminated)
    }

    func testNotify_AlwaysAccepted() async {
        let action = PlanAction(type: "notify", id: "1-1", title: "テスト", message: "通知")
        let executor = MacPlanExecutor(startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([action], dryRun: false)

        XCTAssertEqual(outcomes, [.accepted])
    }

    func testPlanOverlap_CompletesCurrentBeforeNext() async {
        let workspace = FakeLaunchWorkspace(openShouldDelay: true)
        let actionStart = PlanAction(type: "app.start", id: "1-1", bundleID: "com.example.app")
        let actionStop = PlanAction(type: "app.stop", id: "1-2", bundleID: "com.example.app")
        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)

        let outcomes = await executor.execute([actionStart, actionStop], dryRun: false)

        XCTAssertEqual(outcomes, [.rejected("timeout"), .skipped])
    }

    func testDryRun_NoSideEffects() async {
        let workspace = FakeLaunchWorkspace(openShouldTimeout: true)
        let actions = [
            PlanAction(type: "app.start", id: "1-1", bundleID: "com.example.app"),
            PlanAction(type: "app.stop", id: "1-2", bundleID: "com.example.app"),
            PlanAction(type: "notify", id: "1-3", title: "テスト", message: "メッセージ"),
            PlanAction(type: "unsupported", id: "1-4")
        ]

        let executor = MacPlanExecutor(workspace: workspace, startTimeoutSeconds: 0.1)
        let outcomes = await executor.execute(actions, dryRun: true)

        XCTAssertEqual(outcomes, [.skipped, .skipped, .skipped, .skipped])
        XCTAssertEqual(workspace.openedBundleIDs.count, 0)
    }
}

private final class FakeRunningApplication: RunningApplication, @unchecked Sendable {
    private let bundleID: String
    private var terminated = false
    var shouldTerminate: Bool = true
    var wasTerminated: Bool { terminated }

    init(bundleID: String) {
        self.bundleID = bundleID
    }

    var isTerminated: Bool { terminated }

    func terminate() -> Bool {
        if shouldTerminate {
            terminated = true
            return true
        }
        return false
    }
}

private final class FakeLaunchWorkspace: LaunchWorkspace, @unchecked Sendable {
    private(set) var openedBundleIDs: [String] = []
    private(set) var terminatedBundleIDs: [String] = []
    private var appStore: [String: [FakeRunningApplication]]
    let openShouldTimeout: Bool
    let openShouldDelay: Bool
    let resolveBundleIDs: Set<String>

    init(
        runningApplications: [String: [FakeRunningApplication]] = [:],
        openShouldTimeout: Bool = false,
        openShouldDelay: Bool = false,
        resolveBundleIDs: Set<String> = ["com.example.app"]
    ) {
        self.appStore = runningApplications
        self.openShouldTimeout = openShouldTimeout
        self.openShouldDelay = openShouldDelay
        self.resolveBundleIDs = resolveBundleIDs
    }

    func applicationURL(bundleID: String) -> URL? {
        guard resolveBundleIDs.contains(bundleID) else {
            return nil
        }
        return URL(fileURLWithPath: "/Applications/\(bundleID)")
    }

    func runningApplications(bundleID: String) -> [RunningApplication] {
        appStore[bundleID]?.filter { !$0.isTerminated } ?? []
    }

    func openApplication(at url: URL) async -> Error? {
        guard let bundleID = resolveBundleIDs.first(where: { url.path.hasSuffix($0) }) else {
            return NSError(domain: "test", code: 1)
        }
        openedBundleIDs.append(bundleID)

        if openShouldDelay {
            try? await Task.sleep(for: .milliseconds(200))
            return nil
        }

        if openShouldTimeout {
            try? await Task.sleep(for: .milliseconds(200))
            return nil
        }

        if appStore[bundleID] == nil {
            appStore[bundleID] = []
        }
        return nil
    }

    func terminate(bundleID: String) {
        terminatedBundleIDs.append(bundleID)
        appStore[bundleID]?.forEach { _ = $0.terminate() }
    }
}
