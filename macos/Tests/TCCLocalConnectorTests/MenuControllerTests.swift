import XCTest
@testable import TCCLocalConnector
import ConnectorCore

@MainActor
final class MenuControllerTests: XCTestCase {
    func testMenuActionsReachBackendOrSystem() async throws {
        let backend = MockMenuBackend()
        let system = MockMenuSystem()
        let controller = MenuController(backend: backend, system: system)

        for action in [
            MenuAction.refresh,
            .reloadConfig,
            .openConfig,
            .openLog,
            .pause(Constants.pausePresetShortSeconds),
            .resume,
            .showStatus,
            .showWarning
        ] {
            controller.perform(action)
        }
        controller.perform(.quit)
        try await Task.sleep(for: .milliseconds(100))

        let methods = await backend.sentMethods
        XCTAssertTrue(methods.contains("refresh_now"))
        XCTAssertTrue(methods.contains("reload_config"))
        XCTAssertTrue(methods.filter { $0 == "config_paths" }.count >= 2)
        XCTAssertTrue(methods.contains("pause"))
        XCTAssertTrue(methods.contains("resume"))
        XCTAssertGreaterThanOrEqual(methods.filter { $0 == "status" }.count, 2)
        XCTAssertTrue(system.didTerminate)
    }

    func testPendingPlanIsReportedAfterExecution() async {
        let backend = MockMenuBackend()
        let controller = MenuController(backend: backend, system: MockMenuSystem())
        let plan = try! JSONDecoder().decode(PlanPayload.self, from: Data(#"{"cycle_id":42,"dry_run":true,"actions":[],"enforce_stop_bundle_ids":[]}"#.utf8))
        await backend.enqueuePlan(plan)

        await controller.processPendingMessagesForTesting()

        let methods = await backend.sentMethods
        XCTAssertEqual(methods, ["report_actions"])
    }

    func testResponsesAndNotificationsUpdateVisibleState() async {
        let backend = MockMenuBackend()
        let controller = MenuController(backend: backend, system: MockMenuSystem())
        let response = try! JSONDecoder().decode(BackendResponse.self, from: Data(#"{"version":1,"id":"status","result":{"state":"active","cycle_id":7,"parse_ok":true,"running_tasks":[],"last_error":null}}"#.utf8))
        let notification = try! JSONDecoder().decode(NotifyPayload.self, from: Data(#"{"level":"warn","code":"notice","title":"Notice","message":"warning","at":null}"#.utf8))
        await backend.enqueueResponse(response)
        await backend.enqueueNotification(notification)

        await controller.processPendingMessagesForTesting()

        XCTAssertEqual(controller.status?.cycleID, 7)
        XCTAssertEqual(controller.recentWarning, "warning")
    }

    func testPauseStartAndEndAreNotified() async {
        let backend = MockMenuBackend()
        var notices: [String] = []
        let controller = MenuController(backend: backend, system: MockMenuSystem(), notify: { title, _ in notices.append(title) })
        let decode = { (json: String) in try! JSONDecoder().decode(BackendResponse.self, from: Data(json.utf8)) }
        await backend.enqueueResponse(decode(#"{"version":1,"id":"menu-pause-1","result":{"until":"2026-11-26T12:00:00+09:00"}}"#))
        await backend.enqueueResponse(decode(#"{"version":1,"id":"status","result":{"state":"paused","cycle_id":1,"parse_ok":true,"running_tasks":[],"last_error":null}}"#))
        await controller.processPendingMessagesForTesting()
        XCTAssertTrue(controller.isPaused)
        XCTAssertNotNil(controller.pausedUntil)

        await backend.enqueueResponse(decode(#"{"version":1,"id":"status","result":{"state":"active","cycle_id":2,"parse_ok":true,"running_tasks":[],"last_error":null}}"#))
        await backend.enqueueResponse(decode(#"{"version":1,"id":"menu-pause-2","error":{"code":"pause_rejected","message":"一時停止は1時間までです"}}"#))
        await controller.processPendingMessagesForTesting()
        XCTAssertFalse(controller.isPaused)
        XCTAssertNil(controller.pausedUntil)
        XCTAssertEqual(notices, ["一時停止しました", "一時停止が終わりました", "一時停止できませんでした"])
    }

    func testOpenActionsUseReportedPaths() async throws {
        let backend = MockMenuBackend()
        let system = MockMenuSystem()
        let controller = MenuController(backend: backend, system: system)
        let response = try! JSONDecoder().decode(BackendResponse.self, from: Data(#"{"version":1,"id":"paths","result":{"config":"/tmp/config.yml","log":"/tmp/backend.log","pause":"/tmp/pause.json","ledger":"/tmp/ledger.json","state_dir":"/tmp"}}"#.utf8))
        await backend.enqueueResponse(response)
        await controller.processPendingMessagesForTesting()

        controller.perform(.openConfig)
        controller.perform(.openLog)
        try await Task.sleep(for: .milliseconds(100))

        XCTAssertEqual(system.openedPaths, ["/tmp/config.yml", "/tmp/backend.log"])
    }
}

private actor MockMenuBackend: MenuBackend {
    private(set) var sentMethods: [String] = []
    private var plans: [PlanPayload] = []
    private var responses: [BackendResponse] = []
    private var notifications: [NotifyPayload] = []
    var state: BackendState = .running

    func launch(executableURL: URL, arguments: [String]) async throws {}

    func send(method: String, params: JSONValue?) async throws {
        sentMethods.append(method)
    }

    func takeResponses() async -> [BackendResponse] {
        defer { responses.removeAll() }
        return responses
    }

    func takePlans() async -> [PlanPayload] {
        defer { plans.removeAll() }
        return plans
    }

    func takeNotifications() async -> [NotifyPayload] {
        defer { notifications.removeAll() }
        return notifications
    }

    func shutdown() async {}

    func enqueuePlan(_ plan: PlanPayload) {
        plans.append(plan)
    }

    func enqueueResponse(_ response: BackendResponse) {
        responses.append(response)
    }

    func enqueueNotification(_ notification: NotifyPayload) {
        notifications.append(notification)
    }
}

@MainActor
private final class MockMenuSystem: MenuSystem, @unchecked Sendable {
    private(set) var openedPaths: [String] = []
    private(set) var didTerminate = false

    func open(path: String) { openedPaths.append(path) }
    func terminate() { didTerminate = true }
}
