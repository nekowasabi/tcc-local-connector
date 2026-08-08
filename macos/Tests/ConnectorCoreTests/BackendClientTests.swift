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

    func testHandshakeTimeout_TriggersRestart() async {
        let client = BackendClient()
        await client.start()
        let delay = await client.nextRestartDelay()
        let state = await client.state
        XCTAssertEqual(delay, 1)
        XCTAssertEqual(state, .backendDown)
    }

    func testProtocolVersionMismatch_NoRestart() async {
        let client = BackendClient()
        await client.acceptReady(protocolVersion: Constants.supportedProtocolVersion + 1, capabilities: Set(Constants.requiredCapabilities))
        let state = await client.state
        let delay = await client.nextRestartDelay()
        XCTAssertEqual(state, .backendIncompatible)
        XCTAssertNil(delay)
    }

    func testMissingCapability_NoRestart() async {
        let client = BackendClient()
        await client.acceptReady(protocolVersion: Constants.supportedProtocolVersion, capabilities: ["status"])
        let state = await client.state
        let delay = await client.nextRestartDelay()
        XCTAssertEqual(state, .backendIncompatible)
        XCTAssertNil(delay)
    }

    func testRestartPolicyUsesBoundedExponentialBackoff() {
        var policy = RestartPolicy()
        XCTAssertEqual((0..<5).compactMap { _ in policy.nextDelay() }, [1, 2, 4, 8, 16])
        XCTAssertNil(policy.nextDelay())
    }

    func testIncompatibleReadyDoesNotRestart() async {
        let client = BackendClient()
        await client.acceptReady(protocolVersion: 999, capabilities: [])
        let state = await client.state
        let delay = await client.nextRestartDelay()
        XCTAssertEqual(state, .backendIncompatible)
        XCTAssertNil(delay)
    }

    func testDecodesPlanAndNotifyEventsAcrossChunks() async {
        let client = BackendClient()
        let plan = "{\"version\":1,\"event\":\"event.plan\",\"data\":{\"cycle_id\":7,\"dry_run\":true,\"actions\":[],\"enforce_stop_bundle_ids\":[\"com.example.App\"]}}\n"
        let envelope = try! JSONDecoder().decode(BackendEvent.self, from: Data(plan.utf8))
        let payload = try! JSONEncoder().encode(envelope.data)
        XCTAssertEqual(try! JSONDecoder().decode(PlanPayload.self, from: payload).cycleID, 7)
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

    func testRestartBackoffSequence() async {
        let client = BackendClient()
        let delay1 = await client.nextRestartDelay()
        let delay2 = await client.nextRestartDelay()
        let delay3 = await client.nextRestartDelay()
        let delay4 = await client.nextRestartDelay()
        let delay5 = await client.nextRestartDelay()
        let delay6 = await client.nextRestartDelay()
        let state = await client.state
        let delay7 = await client.nextRestartDelay()
        XCTAssertEqual(delay1, 1)
        XCTAssertEqual(delay2, 2)
        XCTAssertEqual(delay3, 4)
        XCTAssertEqual(delay4, 8)
        XCTAssertEqual(delay5, 16)
        XCTAssertNil(delay6)
        XCTAssertEqual(state, .backendDownPermanent)
        XCTAssertNil(delay7)
    }

    func testShutdownEscalation() async {
        let client = BackendClient()
        await client.shutdown()
        let state = await client.state
        XCTAssertEqual(state, .terminated)
    }
}
