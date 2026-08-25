import Foundation

public enum Constants {
    public static let supportedProtocolVersion = 1
    public static let backendDownReleaseGraceSeconds = 180
    public static let pauseNextDayStartHour = 5
    public static let requiredCapabilities: Set<String> = ["status", "reload_config", "pause", "resume", "refresh_now", "config_paths", "report_actions"]
    public static let frontendMaxLineBytes = 262144
	public static let menuRefreshIntervalSeconds = 5
	public static let backendRequestTimeoutSeconds = 30
	public static let pausePresetShortSeconds = 900
	public static let pausePresetLongSeconds = 3600
	public static let appStopPollIntervalMilliseconds = 250
	public static let notifierRecentCapacity = 20
}
