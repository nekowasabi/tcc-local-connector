using System.Diagnostics;

namespace ConnectorCore;

public interface ILockNotifier
{
    void NotifyLocked(string processName);
}

public interface ICloseableProcess : IDisposable
{
    bool CloseMainWindow();
    void Kill();
}

public static class LaunchLockNotification
{
    public const string Title = "Locked";

    public static string Body(string processName) =>
        $"{processName} is locked while a matching task is running";
}

/// <summary>Closes enforce-stop apps that get launched during a task.</summary>
public sealed class LaunchWatcher
{
    private readonly object _lock = new();
    private readonly ILockNotifier _notifier;
    private readonly Func<string, IReadOnlyList<ICloseableProcess>> _processes;
    private readonly Func<DateTime> _utcNow;
    private HashSet<string> _names = new();
    private readonly Dictionary<string, DateTime> _lastNotified = new(StringComparer.OrdinalIgnoreCase);
    private System.Threading.Timer? _timer;

    public LaunchWatcher(ILockNotifier notifier)
        : this(notifier, DefaultProcesses, () => DateTime.UtcNow)
    {
    }

    public LaunchWatcher(
        ILockNotifier notifier,
        Func<string, IReadOnlyList<ICloseableProcess>> processes,
        Func<DateTime> utcNow)
    {
        _notifier = notifier;
        _processes = processes;
        _utcNow = utcNow;
    }

    // Why: polling replaces macOS didLaunchApplicationNotification; WMI process events pull in a heavy dependency.
    public void Update(HashSet<string> enforceStopBundleIds)
    {
        lock (_lock)
        {
            _names = new HashSet<string>(enforceStopBundleIds.Select(ProcessName), StringComparer.OrdinalIgnoreCase);
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
            _lastNotified.Clear();
        }
    }

    public void Poll()
    {
        string[] names;
        lock (_lock) names = _names.ToArray();
        foreach (var name in names)
        {
            var running = _processes(name);
            if (running.Count == 0) continue;
            foreach (var process in running)
            {
                // Why: CloseMainWindow returns false when there is no main window (tray apps), so fall back to Kill.
                try { if (!process.CloseMainWindow()) process.Kill(); } catch (Exception) { }
                process.Dispose();
            }
            NotifyIfDue(name);
        }
    }

    private static string ProcessName(string bundleId) => Path.GetFileNameWithoutExtension(bundleId);

    private void NotifyIfDue(string name)
    {
        var now = _utcNow();
        lock (_lock)
        {
            if (_lastNotified.TryGetValue(name, out var previous)
                && now - previous < TimeSpan.FromMilliseconds(Constants.LaunchLockNotifyDebounceMilliseconds))
            {
                return;
            }
            _lastNotified[name] = now;
        }
        _notifier.NotifyLocked(name);
    }

    private static IReadOnlyList<ICloseableProcess> DefaultProcesses(string name)
    {
        var list = new List<ICloseableProcess>();
        foreach (var process in Process.GetProcessesByName(name))
            list.Add(new ProcessAdapter(process));
        return list;
    }

    private sealed class ProcessAdapter : ICloseableProcess
    {
        private readonly Process _process;
        public ProcessAdapter(Process process) => _process = process;
        public bool CloseMainWindow() => _process.CloseMainWindow();
        public void Kill() => _process.Kill();
        public void Dispose() => _process.Dispose();
    }
}
