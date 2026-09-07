using ConnectorCore;

namespace TCCLocalConnector;

internal sealed class BalloonLockNotifier : ILockNotifier
{
    private readonly NotifyIcon _icon;

    public BalloonLockNotifier(NotifyIcon icon) => _icon = icon;

    public void NotifyLocked(string processName)
    {
        _icon.ShowBalloonTip(
            5000,
            LaunchLockNotification.Title,
            LaunchLockNotification.Body(processName),
            ToolTipIcon.Info);
    }
}
