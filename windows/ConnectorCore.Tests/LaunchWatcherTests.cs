using ConnectorCore;
using Xunit;

public sealed class LaunchWatcherTests
{
    [Fact]
    public void Poll_MatchingProcess_ClosesAndPostsRuleNotice()
    {
        var process = new FakeProcess();
        var notifier = new FakeLockNotifier();
        var watcher = new LaunchWatcher(notifier, name => name == "slack" ? new[] { process } : Array.Empty<ICloseableProcess>());

        watcher.Update(Plan("slack.exe"));
        watcher.Poll();
        watcher.Stop();

        Assert.True(process.Closed);
        Assert.True(process.Disposed);
        Assert.Equal(("slack", "ロック中: sns-gate", "SNS タスク以外は止める"), Assert.Single(notifier.Notices));
        Assert.Equal("ロック中", LaunchLockNotification.Title);
        Assert.Contains("止めています", LaunchLockNotification.Body("slack"));
    }

    [Fact]
    public void Poll_NonMatchingName_DoesNotCloseOrNotify()
    {
        var process = new FakeProcess();
        var notifier = new FakeLockNotifier();
        var watcher = new LaunchWatcher(notifier, name => name == "slack" ? new[] { process } : Array.Empty<ICloseableProcess>());

        watcher.Update(Plan("other.exe"));
        watcher.Poll();
        watcher.Stop();

        Assert.False(process.Closed);
        Assert.Empty(notifier.Notices);
    }

    [Fact]
    public void Poll_DryRun_DoesNotClose()
    {
        var process = new FakeProcess();
        var notifier = new FakeLockNotifier();
        var watcher = new LaunchWatcher(notifier, _ => new[] { process });
        var plan = Plan("slack.exe");

        watcher.Update(new PlanPayload { DryRun = true, EnforceStopBundleIds = plan.EnforceStopBundleIds, Actions = plan.Actions });
        watcher.Poll();
        watcher.Stop();

        Assert.False(process.Closed);
    }

    [Fact]
    public void Poll_KillFails_WarnsInsteadOfNotifying()
    {
        var notifier = new FakeLockNotifier();
        var watcher = new LaunchWatcher(notifier, _ => new[] { new FakeProcess(closeReturns: false, killThrows: true) });

        watcher.Update(Plan("slack.exe"));
        watcher.Poll();
        watcher.Stop();

        Assert.Empty(notifier.Notices);
        Assert.Equal("slack", Assert.Single(notifier.Warnings));
    }

    private static PlanPayload Plan(params string[] bundleIds) => new()
    {
        EnforceStopBundleIds = bundleIds.ToList(),
        Actions = bundleIds.Select(id => new PlanAction { Type = "app.stop", BundleId = id, Title = "ロック中: sns-gate", Message = "SNS タスク以外は止める" }).ToList(),
    };

    private sealed class FakeLockNotifier : ILockNotifier
    {
        public List<(string Name, string? Title, string? Message)> Notices { get; } = new();
        public List<string> Warnings { get; } = new();
        public void NotifyLocked(string processName, string? title, string? message) => Notices.Add((processName, title, message));
        public void WarnNotStopped(string processName) => Warnings.Add(processName);
    }

    private sealed class FakeProcess : ICloseableProcess
    {
        private readonly bool _closeReturns;
        private readonly bool _killThrows;
        public FakeProcess(bool closeReturns = true, bool killThrows = false) { _closeReturns = closeReturns; _killThrows = killThrows; }
        public bool Closed { get; private set; }
        public bool Killed { get; private set; }
        public bool Disposed { get; private set; }
        public bool CloseMainWindow() { Closed = true; return _closeReturns; }
        public void Kill() { if (_killThrows) throw new System.ComponentModel.Win32Exception(5); Killed = true; }
        public void Dispose() => Disposed = true;
    }
}
