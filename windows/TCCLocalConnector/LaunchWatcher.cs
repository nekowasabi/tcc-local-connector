using System.Diagnostics;
using ConnectorCore;

namespace TCCLocalConnector;

/// <summary>Closes enforce-stop apps that get launched during a task.</summary>
internal sealed class LaunchWatcher
{
    private readonly object _lock = new();
    private HashSet<string> _names = new();
    private System.Threading.Timer? _timer;

    // Why: polling replaces macOS didLaunchApplicationNotification; WMI process events pull in a heavy dependency.
    public void Update(HashSet<string> enforceStopBundleIds)
    {
        lock (_lock)
        {
            _names = new HashSet<string>(enforceStopBundleIds.Select(WindowsPlanExecutor.ProcessName), StringComparer.OrdinalIgnoreCase);
            _timer ??= new System.Threading.Timer(_ => Poll(), null, Constants.LaunchWatchPollIntervalMilliseconds, Constants.LaunchWatchPollIntervalMilliseconds);
        }
    }

    public void Stop()
    {
        lock (_lock)
        {
            _timer?.Dispose();
            _timer = null;
            _names = new HashSet<string>();
        }
    }

    private void Poll()
    {
        string[] names;
        lock (_lock) names = _names.ToArray();
        foreach (var name in names)
        {
            foreach (var process in Process.GetProcessesByName(name))
            {
                // Why: CloseMainWindow returns false when there is no main window (tray apps), so fall back to Kill.
                try { if (!process.CloseMainWindow()) process.Kill(); } catch (Exception) { }
                process.Dispose();
            }
        }
    }
}
