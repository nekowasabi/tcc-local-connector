import ConnectorCore
import Foundation
import XCTest
@testable import TCCLocalConnector

@MainActor
final class BrowserPolicyHeartbeatTests: XCTestCase {
    func testHeartbeatHasStrictJSONTimestampAndPermissions() throws {
        let root = try makeTemporaryDirectory()
        let fixedDate = Date(timeIntervalSince1970: 1_786_363_496.789)
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root, now: { fixedDate })

        XCTAssertTrue(heartbeat.startOrRefresh())

        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: Data(contentsOf: heartbeat.heartbeatURL)) as? [String: Any]
        )
        XCTAssertEqual(Set(object.keys), ["version", "updated_at"])
        XCTAssertEqual(object["version"] as? Int, 1)
        XCTAssertNotNil((object["updated_at"] as? String)?.range(
            of: #"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$"#,
            options: .regularExpression
        ))
        XCTAssertNil(object["generation"])

        let directoryAttributes = try FileManager.default.attributesOfItem(
            atPath: heartbeat.heartbeatURL.deletingLastPathComponent().path
        )
        let fileAttributes = try FileManager.default.attributesOfItem(atPath: heartbeat.heartbeatURL.path)
        let directoryPermissions = try XCTUnwrap(directoryAttributes[.posixPermissions] as? NSNumber)
        let filePermissions = try XCTUnwrap(fileAttributes[.posixPermissions] as? NSNumber)
        XCTAssertEqual(directoryPermissions.intValue & 0o777, 0o700)
        XCTAssertEqual(filePermissions.intValue & 0o777, 0o600)
        XCTAssertEqual(BrowserPolicyHeartbeat.intervalSeconds, 5)
    }

    func testRefreshAtomicallyReplacesFileAndLeavesNoTemporaryFile() throws {
        let root = try makeTemporaryDirectory()
        var now = Date(timeIntervalSince1970: 1_786_363_496)
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root, now: { now })
        XCTAssertTrue(heartbeat.startOrRefresh())
        let original = try Data(contentsOf: heartbeat.heartbeatURL)

        now = now.addingTimeInterval(5)
        heartbeat.refreshForTesting()

        let updated = try Data(contentsOf: heartbeat.heartbeatURL)
        XCTAssertNotEqual(updated, original)
        let directoryContents = try FileManager.default.contentsOfDirectory(
            at: heartbeat.heartbeatURL.deletingLastPathComponent(),
            includingPropertiesForKeys: nil
        )
        XCTAssertEqual(directoryContents.map(\.lastPathComponent), [BrowserPolicyHeartbeat.fileName])
    }

    func testStopDeletesHeartbeatAndInvalidatesPendingRefresh() throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        XCTAssertTrue(heartbeat.startOrRefresh())

        heartbeat.stop()
        heartbeat.refreshForTesting()

        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testWriteFailureDoesNotCrashOrKeepHeartbeat() throws {
        let rootFile = try makeTemporaryDirectory().appendingPathComponent("not-a-directory")
        XCTAssertTrue(FileManager.default.createFile(atPath: rootFile.path, contents: Data()))
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: rootFile)

        XCTAssertFalse(heartbeat.startOrRefresh())
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testApplicationTerminationDeletesHeartbeat() throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        XCTAssertTrue(heartbeat.startOrRefresh())

        NotificationCenter.default.post(name: NSApplication.willTerminateNotification, object: nil)

        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testHealthyStatusStartsHeartbeatButConfigPathsDoNot() async throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        let backend = HeartbeatMenuBackend()
        let controller = MenuController(
            backend: backend,
            system: HeartbeatMenuSystem(),
            heartbeat: heartbeat
        )
        await backend.enqueueResponse(try response(#"{"version":1,"id":"paths","result":{"config":"/tmp/config.yml"}}"#))
        await controller.processPendingMessagesForTesting()
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        await backend.enqueueResponse(try response(
            #"{"version":1,"id":"status","result":{"state":"active","cycle_id":7,"parse_ok":true,"running_tasks":[],"last_error":null}}"#
        ))
        await controller.processPendingMessagesForTesting()

        XCTAssertTrue(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testReloadKeepsHeartbeatAndPauseDeletesIt() async throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        let backend = HeartbeatMenuBackend()
        let controller = MenuController(
            backend: backend,
            system: HeartbeatMenuSystem(),
            heartbeat: heartbeat
        )
        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()
        XCTAssertTrue(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        controller.perform(.reloadConfig)
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertTrue(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        controller.perform(.pause(900))
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testActiveStatusWithParseOKFalseKeepsHeartbeat() async throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        let backend = HeartbeatMenuBackend()
        let controller = MenuController(
            backend: backend,
            system: HeartbeatMenuSystem(),
            heartbeat: heartbeat
        )
        await backend.enqueueResponse(try response(
            #"{"version":1,"id":"status","result":{"state":"active","cycle_id":7,"parse_ok":false,"running_tasks":[],"last_error":"missing"}}"#
        ))
        await controller.processPendingMessagesForTesting()
        XCTAssertTrue(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testPausedStatusAndRPCErrorDeleteHeartbeat() async throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        let backend = HeartbeatMenuBackend()
        let controller = MenuController(
            backend: backend,
            system: HeartbeatMenuSystem(),
            heartbeat: heartbeat
        )
        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()
        XCTAssertTrue(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        await backend.enqueueResponse(try response(
            #"{"version":1,"id":"status","result":{"state":"paused","cycle_id":7,"parse_ok":true,"running_tasks":[],"last_error":null}}"#
        ))
        await controller.processPendingMessagesForTesting()
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()
        await backend.enqueueResponse(try response(
            #"{"version":1,"id":"status","error":{"code":"backend_error","message":"failed"}}"#
        ))
        await controller.processPendingMessagesForTesting()
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testDecodeAndBackendFailuresDeleteHeartbeat() async throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        let backend = HeartbeatMenuBackend()
        let controller = MenuController(
            backend: backend,
            system: HeartbeatMenuSystem(),
            heartbeat: heartbeat
        )
        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()
        await backend.enqueueResponse(try response(
            #"{"version":1,"id":"status","result":{"state":42}}"#
        ))
        await controller.processPendingMessagesForTesting()
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        await backend.enqueueResponse(try healthyStatusResponse())
        await backend.setState(.backendDown)
        controller.perform(.showStatus)
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
    }

    func testStatusStopPauseAndQuitDeleteHeartbeatWithoutRegeneration() async throws {
        let root = try makeTemporaryDirectory()
        let heartbeat = BrowserPolicyHeartbeat(applicationSupportURL: root)
        let backend = HeartbeatMenuBackend()
        let system = HeartbeatMenuSystem()
        let controller = MenuController(backend: backend, system: system, heartbeat: heartbeat)
        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()

        controller.perform(.showStatus)
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()
        controller.perform(.pause(60))
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))

        await backend.enqueueResponse(try healthyStatusResponse())
        await controller.processPendingMessagesForTesting()
        controller.perform(.quit)
        heartbeat.refreshForTesting()
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertFalse(FileManager.default.fileExists(atPath: heartbeat.heartbeatURL.path))
        XCTAssertTrue(system.didTerminate)
    }

    private func makeTemporaryDirectory() throws -> URL {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("BrowserPolicyHeartbeatTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock {
            try? FileManager.default.removeItem(at: directory)
        }
        return directory
    }

    private func healthyStatusResponse() throws -> BackendResponse {
        try response(
            #"{"version":1,"id":"status","result":{"state":"active","cycle_id":7,"parse_ok":true,"running_tasks":[],"last_error":null}}"#
        )
    }

    private func response(_ json: String) throws -> BackendResponse {
        try JSONDecoder().decode(BackendResponse.self, from: Data(json.utf8))
    }
}

private actor HeartbeatMenuBackend: MenuBackend {
    var state: BackendState = .running
    private var responses: [BackendResponse] = []

    func launch(executableURL: URL, arguments: [String]) async throws {}
    func send(method: String, params: JSONValue?) async throws {}
    func takePlans() async -> [PlanPayload] { [] }
    func takeNotifications() async -> [NotifyPayload] { [] }
    func shutdown() async {}

    func takeResponses() async -> [BackendResponse] {
        defer { responses.removeAll() }
        return responses
    }

    func enqueueResponse(_ response: BackendResponse) {
        responses.append(response)
    }

    func setState(_ state: BackendState) {
        self.state = state
    }
}

@MainActor
private final class HeartbeatMenuSystem: MenuSystem, @unchecked Sendable {
    private(set) var didTerminate = false

    func open(path: String) {}
    func terminate() { didTerminate = true }
}
