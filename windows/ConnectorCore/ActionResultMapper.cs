namespace ConnectorCore;

public static class ActionResultMapper
{
    /// <summary>Maps an executor outcome to the report_actions status/code pair.</summary>
    public static (string Status, string? Code) Map(ActionOutcome outcome) => outcome switch
    {
        { Kind: ActionOutcomeKind.Accepted } => ("accepted", null),
        { Kind: ActionOutcomeKind.Skipped } => ("skipped", null),
        { Code: "quit_refused" } => ("refused", "quit_refused"),
        { Code: "timeout" } => ("timeout", "timeout"),
        _ => ("failed", outcome.Code),
    };
}
