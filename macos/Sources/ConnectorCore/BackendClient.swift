import Foundation
import Darwin

public protocol BackendClientDelegate: Sendable {
    func backendStateChanged(_ state: BackendState) async
}

public struct RestartPolicy: Sendable {
    public private(set) var attempts = 0

    public init() {}

    public mutating func nextDelay() -> Int? {
        guard attempts < Constants.backendRestartMaxAttempts else { return nil }
        let delay = min(
            Constants.backendRestartBaseDelaySeconds * Int(pow(Double(Constants.backendRestartDelayFactor), Double(attempts))),
            Constants.backendRestartMaxDelaySeconds
        )
        attempts += 1
        return delay
    }
}

public actor BackendClient {
    public private(set) var state: BackendState = .terminated
    private var policy = RestartPolicy()
	private var process: Process?
	private var input: Pipe?
    private var stdout: Pipe?
	private var stderr: Pipe?
    private var framer = LineFramer()
    public private(set) var plans: [PlanPayload] = []
    public private(set) var notifications: [NotifyPayload] = []
    public private(set) var responses: [BackendResponse] = []
    public private(set) var diagnostics: [String] = []
	private var nextRequestSequence = 0

    public init() {}

    public func start() {
        state = .starting
    }

	public func launch(executableURL: URL, arguments: [String]) throws {
		shutdown()
		let child = Process()
		let stdin = Pipe()
		child.executableURL = executableURL
		child.arguments = arguments
		child.standardInput = stdin
		let output = Pipe()
		let error = Pipe()
		child.standardOutput = output
		child.standardError = error
		output.fileHandleForReading.readabilityHandler = { [weak self] handle in
			let data = handle.availableData
			guard !data.isEmpty else { return }
			Task { await self?.consumeStdout(data) }
		}
		error.fileHandleForReading.readabilityHandler = { [weak self] handle in
			let data = handle.availableData
			guard !data.isEmpty else { return }
			Task { await self?.consumeStderr(data) }
		}
		try child.run()
		process = child
		input = stdin
		stdout = output
		stderr = error
		state = .starting
	}

	public func send(_ request: BackendRequest) throws {
		guard let input else { throw CocoaError(.fileNoSuchFile) }
		var payload = try JSONEncoder().encode(request)
		payload.append(0x0A)
		input.fileHandleForWriting.write(payload)
	}

	public func send(method: String, params: JSONValue? = nil) throws {
		nextRequestSequence += 1
		let request = params.map { BackendRequest(id: "menu-\(nextRequestSequence)", method: method, params: $0) }
			?? BackendRequest(id: "menu-\(nextRequestSequence)", method: method)
		try send(request)
	}

	public func takePlans() -> [PlanPayload] {
		defer { plans.removeAll() }
		return plans
	}

	public func takeResponses() -> [BackendResponse] {
		defer { responses.removeAll() }
		return responses
	}

    public func takeNotifications() -> [NotifyPayload] {
        defer { notifications.removeAll() }
        return notifications
    }

    public func consumeStdout(_ data: Data) {
        for line in framer.push(data) {
            guard !line.isEmpty else { continue }
            if let event = try? JSONDecoder().decode(BackendEvent.self, from: line) {
                handle(event)
            } else if let response = try? JSONDecoder().decode(BackendResponse.self, from: line) {
                responses.append(response)
            }
        }
    }

    public func consumeStderr(_ data: Data) {
        let message = String(decoding: data, as: UTF8.self)
        guard !message.isEmpty else { return }
        diagnostics.append(message)
        if diagnostics.count > Constants.notifierRecentCapacity { diagnostics.removeFirst() }
    }

    private func handle(_ event: BackendEvent) {
        guard let data = event.data, let payload = try? JSONEncoder().encode(data) else { return }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        switch event.event {
        case "ready":
            struct ReadyPayload: Decodable { let protocolVersion: Int; let capabilities: [Capability]; enum CodingKeys: String, CodingKey { case protocolVersion = "protocol_version", capabilities } }
            struct Capability: Decodable { let name: String }
            if let ready = try? decoder.decode(ReadyPayload.self, from: payload) { acceptReady(protocolVersion: ready.protocolVersion, capabilities: Set(ready.capabilities.map(\.name))) }
        case "event.plan":
            if let plan = try? decoder.decode(PlanPayload.self, from: payload) { plans.append(plan) }
        case "event.notify":
            if let notification = try? decoder.decode(NotifyPayload.self, from: payload) { notifications.append(notification) }
        default:
            break
        }
    }

    public func acceptReady(protocolVersion: Int, capabilities: Set<String>) {
        guard protocolVersion == Constants.supportedProtocolVersion,
              Constants.requiredCapabilities.isSubset(of: capabilities) else {
            state = .backendIncompatible
            return
        }
        state = .running
        policy = RestartPolicy()
    }

    public func nextRestartDelay() -> Int? {
        guard state != .backendIncompatible else { return nil }
        state = .backendDown
        guard let delay = policy.nextDelay() else {
            state = .backendDownPermanent
            return nil
        }
        return delay
    }

	public func shutdown() {
		stdout?.fileHandleForReading.readabilityHandler = nil
		stderr?.fileHandleForReading.readabilityHandler = nil
		input?.fileHandleForWriting.closeFile()
		input = nil
		stdout = nil
		stderr = nil
		if let process, process.isRunning {
			process.terminate()
		}
		process = nil
        state = .terminated
    }

    // Why: The backend child is owned by this connector, so escalation is allowed for the child only.
    public func hardKill(_ process: Process) {
        _ = kill(process.processIdentifier, SIGKILL)
    }
}
