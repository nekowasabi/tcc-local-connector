using ConnectorCore;

namespace TCCLocalConnector;

internal sealed class BalloonLockNotifier : ILockNotifier
{
    private readonly NotifyIcon _icon;
    private readonly Action<string> _warn;

    public BalloonLockNotifier(NotifyIcon icon, Action<string> warn)
    {
        _icon = icon;
        _warn = warn;
    }

    public void NotifyLocked(string processName, string? title, string? message)
    {
        // Why: a balloon has no subtitle line (macOS shows the bundle ID there), so the process name goes under the rule text.
        var body = string.IsNullOrWhiteSpace(message) ? LaunchLockNotification.Body(processName) : $"{message}\n{processName}";
        _icon.ShowBalloonTip(5000, string.IsNullOrWhiteSpace(title) ? LaunchLockNotification.Title : title, body, ToolTipIcon.Info);
    }

    public void WarnNotStopped(string processName) => _warn(LaunchLockNotification.NotStopped(processName));
}
