import XCTest
@testable import ConnectorCore

final class BackendClientTests: XCTestCase {
    func testHandshakeSuccess() async {
        let client = BackendClient()
        await client.acceptReady(
            protocolVersion: Constants.supportedProtocolVersion,
            capabilities: Set(Constants.requiredCapabilities)
        )
        let state = await client.state
        XCTAssertEqual(state, .running)
    }

    func testProtocolVersionMismatch() async {
        let client = BackendClient()
        await client.acceptReady(protocolVersion: Constants.supportedProtocolVersion + 1, capabilities: Set(Constants.requiredCapabilities))
        let state = await client.state
        XCTAssertEqual(state, .backendIncompatible)
    }

    func testMissingCapability() async {
        let client = BackendClient()
        await client.acceptReady(protocolVersion: Constants.supportedProtocolVersion, capabilities: ["status"])
        let state = await client.state
        XCTAssertEqual(state, .backendIncompatible)
    }

    func testDecodesPlanAndNotifyEventsAcrossChunks() async {
        let client = BackendClient()
        let plan = "{\"version\":1,\"event\":\"event.plan\",\"data\":{\"cycle_id\":7,\"dry_run\":true,\"actions\":[],\"enforce_stop_bundle_ids\":[\"com.example.App\"]}}\n"
        await client.consumeStdout(Data(plan.prefix(17).utf8))
        await client.consumeStdout(Data(plan.dropFirst(17).utf8))
        let notify = "{\"version\":1,\"event\":\"event.notify\",\"data\":{\"level\":\"warn\",\"code\":\"action_refused\",\"title\":\"x\",\"message\":\"y\"}}\n"
        await client.consumeStdout(Data(notify.utf8))
        let plans = await client.plans
        let notifications = await client.notifications
        XCTAssertEqual(plans.first?.cycleID, 7)
        XCTAssertEqual(plans.first?.dryRun, true)
        XCTAssertEqual(plans.first?.enforceStopBundleIDs, ["com.example.App"])
        XCTAssertEqual(notifications.first?.code, "action_refused")
    }

    func testEncodesNumericPauseParameterForGo() throws {
        let request = BackendRequest(id: "pause-1", method: "pause", params: .object(["duration_seconds": .number(900)]))
        let body = try JSONEncoder().encode(request)
        let object = try JSONSerialization.jsonObject(with: body) as! [String: Any]
        XCTAssertEqual((object["params"] as? [String: Any])?["duration_seconds"] as? Int, 900)
    }

    func testShutdown() async {
        let client = BackendClient()
        await client.shutdown()
        let state = await client.state
        XCTAssertEqual(state, .terminated)
    }
}
