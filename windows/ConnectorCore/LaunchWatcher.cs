using System.Diagnostics;

namespace ConnectorCore;

public interface ILockNotifier
{
    void NotifyLocked(string processName, string? title, string? message);
    void WarnNotStopped(string processName);
}

public interface ICloseableProcess : IDisposable
{
    bool CloseMainWindow();
    void Kill();
}

public static class LaunchLockNotification
{
    public const string Title = "ロック中";

    public static string Body(string processName) =>
        $"{processName} は今のタスクのルールで止めています";

    public static string NotStopped(string processName) =>
        $"{processName} を終了できません";
}

/// <summary>Closes enforce-stop apps and posts one lock notice per launch (formal/lock-notice).</summary>
public sealed class LaunchWatcher
{
    private readonly object _lock = new();
    private readonly ILockNotifier _notifier;
    private readonly Func<string, IReadOnlyList<ICloseableProcess>> _processes;
    private HashSet<string> _names = new(StringComparer.OrdinalIgnoreCase);
    private Dictionary<string, PlanAction> _notices = new(StringComparer.OrdinalIgnoreCase);
    // Why: cleared only when a poll sees the app gone, so one launch gets one notice however long it takes to die.
    private readonly HashSet<string> _notified = new(StringComparer.OrdinalIgnoreCase);
    private readonly HashSet<string> _claimed = new(StringComparer.OrdinalIgnoreCase);
    private System.Threading.Timer? _timer;

    public LaunchWatcher(ILockNotifier notifier)
        : this(notifier, DefaultProcesses)
    {
    }

    public LaunchWatcher(ILockNotifier notifier, Func<string, IReadOnlyList<ICloseableProcess>> processes)
    {
        _notifier = notifier;
        _processes = processes;
    }

    // Why: polling replaces macOS didLaunchApplicationNotification; WMI process events pull in a heavy dependency.
    public void Update(PlanPayload plan)
    {
        var notices = new Dictionary<string, PlanAction>(StringComparer.OrdinalIgnoreCase);
        foreach (var action in plan.Actions)
        {
            if (action.Type == "app.stop" && !string.IsNullOrEmpty(action.BundleId))
                notices.TryAdd(ProcessName(action.BundleId), action);
        }
        lock (_lock)
        {
            _names = plan.DryRun
                ? new HashSet<string>(StringComparer.OrdinalIgnoreCase)
                : new HashSet<string>(plan.EnforceStopBundleIds.Select(ProcessName), StringComparer.OrdinalIgnoreCase);
            _notices = notices;
            _timer ??= new System.Threading.Timer(_ => Poll(), null, Constants.LaunchWatchPollIntervalMilliseconds, Constants.LaunchWatchPollIntervalMilliseconds);
        }
    }

    /// <summary>WindowsPlanExecutor.StopApp owns the app until Release; polls skip it meanwhile.</summary>
    public void Claim(string bundleId)
    {
        lock (_lock) _claimed.Add(ProcessName(bundleId));
    }

    public void Release(string bundleId)
    {
        lock (_lock) _claimed.Remove(ProcessName(bundleId));
    }

    /// <summary>Called by StopApp after it actually ended the app; shares the once-per-launch notice with Poll.</summary>
    public void ReportStopped(string bundleId) => NotifyOnce(ProcessName(bundleId));

    public void Stop()
    {
        lock (_lock)
        {
            _timer?.Dispose();
            _timer = null;
            _names = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
            _notified.Clear();
            _claimed.Clear();
        }
    }

    public void Poll()
    {
        string[] names;
        lock (_lock) names = _names.Where(name => !_claimed.Contains(name)).ToArray();
        foreach (var name in names)
        {
            var running = _processes(name);
            if (running.Count == 0)
            {
                lock (_lock)
                {
                    if (!_claimed.Contains(name)) _notified.Remove(name);
                }
                continue;
            }
            var stopped = true;
            foreach (var process in running)
            {
                // Why: CloseMainWindow returns false when there is no main window (tray apps), so fall back to Kill.
                try { if (!process.CloseMainWindow()) process.Kill(); }
                catch (InvalidOperationException) { } // exited between the snapshot and the close
                catch (Exception) { stopped = false; }
                finally { process.Dispose(); }
            }
            if (stopped) NotifyOnce(name); else _notifier.WarnNotStopped(name);
        }
    }

    private static string ProcessName(string bundleId) => Path.GetFileNameWithoutExtension(bundleId);

    private void NotifyOnce(string name)
    {
        PlanAction? notice;
        lock (_lock)
        {
            if (!_notified.Add(name)) return;
            _notices.TryGetValue(name, out notice);
        }
        _notifier.NotifyLocked(name, notice?.Title, notice?.Message);
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
