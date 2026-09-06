using System.Text.Json;
using System.Text.Json.Serialization;

namespace ConnectorCore;

public sealed class BackendRequest
{
    [JsonPropertyName("version")] public int Version { get; init; } = Constants.SupportedProtocolVersion;
    [JsonPropertyName("id")] public required string Id { get; init; }
    [JsonPropertyName("method")] public required string Method { get; init; }
    [JsonPropertyName("params")][JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public object? Params { get; init; }
}

public sealed class RpcError
{
    [JsonPropertyName("code")] public string Code { get; init; } = "";
    [JsonPropertyName("message")] public string Message { get; init; } = "";
}

public sealed class BackendResponse
{
    [JsonPropertyName("version")] public int Version { get; init; }
    [JsonPropertyName("id")] public string? Id { get; init; }
    [JsonPropertyName("result")] public JsonElement? Result { get; init; }
    [JsonPropertyName("error")] public RpcError? Error { get; init; }
}

public sealed class BackendEvent
{
    [JsonPropertyName("version")] public int Version { get; init; }
    [JsonPropertyName("event")] public string Event { get; init; } = "";
    [JsonPropertyName("data")] public JsonElement? Data { get; init; }
}

public sealed class PlanAction
{
    [JsonPropertyName("kind")] public string Type { get; init; } = "";
    [JsonPropertyName("action_id")] public string Id { get; init; } = "";
    [JsonPropertyName("bundle_id")] public string? BundleId { get; init; }
    [JsonPropertyName("title")] public string? Title { get; init; }
    [JsonPropertyName("message")] public string? Message { get; init; }
    [JsonPropertyName("grace_seconds")] public int? GraceSeconds { get; init; }
    [JsonPropertyName("reason")] public string? Reason { get; init; }
}

public sealed class PlanPayload
{
    [JsonPropertyName("cycle_id")] public long CycleId { get; init; }
    [JsonPropertyName("dry_run")] public bool DryRun { get; init; }
    [JsonPropertyName("actions")] public List<PlanAction> Actions { get; init; } = new();
    [JsonPropertyName("enforce_stop_bundle_ids")] public List<string> EnforceStopBundleIds { get; init; } = new();
}

public sealed class RunningTaskPayload
{
    [JsonPropertyName("name")] public string Name { get; init; } = "";
    [JsonPropertyName("task_id")] public string TaskId { get; init; } = "";
}

public sealed class StatusPayload
{
    [JsonPropertyName("state")] public string State { get; init; } = "";
    [JsonPropertyName("cycle_id")] public long CycleId { get; init; }
    [JsonPropertyName("parse_ok")] public bool ParseOk { get; init; }
    [JsonPropertyName("running_tasks")] public List<RunningTaskPayload> RunningTasks { get; init; } = new();
    [JsonPropertyName("last_error")] public string? LastError { get; init; }
}

public sealed class NotifyPayload
{
    [JsonPropertyName("level")] public string Level { get; init; } = "";
    [JsonPropertyName("code")] public string Code { get; init; } = "";
    [JsonPropertyName("title")] public string Title { get; init; } = "";
    [JsonPropertyName("message")] public string Message { get; init; } = "";
    [JsonPropertyName("at")] public string? At { get; init; }
}

public enum BackendState { Starting, Running, BackendDown, BackendDownPermanent, BackendIncompatible, Terminated }

public enum ActionOutcomeKind { Accepted, Skipped, Rejected }

public readonly record struct ActionOutcome(ActionOutcomeKind Kind, string? Code = null)
{
    public static readonly ActionOutcome Accepted = new(ActionOutcomeKind.Accepted);
    public static readonly ActionOutcome Skipped = new(ActionOutcomeKind.Skipped);
    public static ActionOutcome Rejected(string code) => new(ActionOutcomeKind.Rejected, code);
}
