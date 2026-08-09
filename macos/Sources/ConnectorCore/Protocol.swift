import Foundation
public enum JSONValue: Codable, Sendable, Equatable {
    case string(String), number(Double), bool(Bool), object([String: JSONValue]), array([JSONValue]), null
    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null }
        else if let value = try? container.decode(Bool.self) { self = .bool(value) }
        else if let value = try? container.decode(Double.self) { self = .number(value) }
        else if let value = try? container.decode(String.self) { self = .string(value) }
        else if let value = try? container.decode([String: JSONValue].self) { self = .object(value) }
        else { self = .array(try container.decode([JSONValue].self)) }
    }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self { case .string(let value): try container.encode(value); case .number(let value): if value.rounded() == value { try container.encode(Int64(value)) } else { try container.encode(value) }; case .bool(let value): try container.encode(value); case .object(let value): try container.encode(value); case .array(let value): try container.encode(value); case .null: try container.encodeNil() }
    }
}
public struct BackendRequest: Codable, Sendable {
    public let version: Int
    public let id: String
    public let method: String
    public let params: JSONValue?
    public init(id: String, method: String, params: [String: String]? = nil) {
        version = Constants.supportedProtocolVersion; self.id = id; self.method = method
        self.params = params.map { .object($0.mapValues(JSONValue.string)) }
    }
    public init(id: String, method: String, params: JSONValue) {
        version = Constants.supportedProtocolVersion; self.id = id; self.method = method; self.params = params
    }
}
public struct RPCError: Codable, Sendable { public let code: String; public let message: String }
public struct BackendResponse: Codable, Sendable { public let version: Int; public let id: String?; public let result: JSONValue?; public let error: RPCError? }
public struct BackendEvent: Codable, Sendable { public let version: Int; public let event: String; public let data: JSONValue? }
public enum BackendState: String, Codable, Sendable { case starting, running, backendDown, backendDownPermanent, backendIncompatible, terminated }
public struct PlanAction: Codable, Sendable {
    public let type: String
    public let id: String
    public let bundleID: String?
    public let title: String?
    public let message: String?
    public let graceSeconds: Int?
    public let reason: String?
    public let level: String?

    enum CodingKeys: String, CodingKey { case type = "kind", id = "action_id", bundleID = "bundle_id", title, message, graceSeconds = "grace_seconds", reason, level }
    public init(type: String, id: String, bundleID: String? = nil, title: String? = nil, message: String? = nil, graceSeconds: Int? = nil, reason: String? = nil, level: String? = nil) {
        self.type = type; self.id = id; self.bundleID = bundleID; self.title = title; self.message = message; self.graceSeconds = graceSeconds; self.reason = reason; self.level = level
    }
}
public struct PlanPayload: Codable, Sendable { public let cycleID: Int64; public let dryRun: Bool; public let actions: [PlanAction]; public let enforceStopBundleIDs: [String]; enum CodingKeys: String, CodingKey { case cycleID = "cycle_id", dryRun = "dry_run", actions, enforceStopBundleIDs = "enforce_stop_bundle_ids" } }
public struct RunningTaskPayload: Codable, Sendable {
    public let name: String
    public let taskID: String

    enum CodingKeys: String, CodingKey { case name; case taskID = "task_id" }
}

public struct StatusPayload: Codable, Sendable {
    public let state: String
    public let cycleID: Int64
    public let parseOK: Bool
    public let runningTasks: [RunningTaskPayload]
    public let lastError: String?

    enum CodingKeys: String, CodingKey {
        case state
        case cycleID = "cycle_id"
        case parseOK = "parse_ok"
        case runningTasks = "running_tasks"
        case lastError = "last_error"
    }
}
public struct NotifyPayload: Codable, Sendable { public let level: String; public let code: String; public let title: String; public let message: String; public let at: Date? }
public struct StateChangedPayload: Codable, Sendable { public let state: String; public let cycleID: Int64; enum CodingKeys: String, CodingKey { case state; case cycleID = "cycle_id" } }
