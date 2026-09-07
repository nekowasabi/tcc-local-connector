namespace ConnectorCore;

public static class Constants
{
    public const int SupportedProtocolVersion = 1;
    public static readonly IReadOnlySet<string> RequiredCapabilities = new HashSet<string>
    {
        "status", "reload_config", "pause", "resume", "refresh_now", "config_paths", "report_actions",
    };
    public const int FrontendMaxLineBytes = 262144;
    public const int MenuRefreshIntervalSeconds = 5;
    public const int BackendRequestTimeoutSeconds = 30;
    public const int PausePresetShortSeconds = 900;
    public const int PausePresetLongSeconds = 3600;
    public const int PauseNextDayStartHour = 5;
    public const int AppStopPollIntervalMilliseconds = 250;
    public const int LaunchWatchPollIntervalMilliseconds = 2000;
    public const int LaunchLockNotifyDebounceMilliseconds = 10000;
    public const int HeartbeatIntervalSeconds = 5;
}
