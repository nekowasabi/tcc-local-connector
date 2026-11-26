using ConnectorCore;
using Xunit;

// formal/lock-notice/LockNotice.tla の design の前提 (WAct・NotifyIfDue・goneReset・watcherYields・sharedNotify) を実コードで確かめる。
public sealed class LockNoticeConformanceTests
{
    [Fact]
    public void WAct_WindowCloses_NoKill_ButNotifies()
    {
        var process = new FakeProcess(closeReturns: true);
        var (watcher, notifier) = Build(() => new[] { process });
        watcher.Poll();
        Assert.False(process.Killed);
        Assert.Single(notifier.Notices);
    }

    [Fact]
    public void WAct_NoMainWindow_Kills()
    {
        var process = new FakeProcess(closeReturns: false);
        var (watcher, _) = Build(() => new[] { process });
        watcher.Poll();
        Assert.True(process.Killed);
    }

    [Fact]
    public void GoneReset_OneNoticePerLaunch_ResetWhenPollSeesZero()
    {
        var state = new Running { Value = true };
        var (watcher, notifier) = Build(() => state.Value ? new[] { new FakeProcess() } : Array.Empty<ICloseableProcess>());
        watcher.Poll();
        watcher.Poll();                     // 同じ起動がまだ残っている
        Assert.Single(notifier.Notices);
        state.Value = false; watcher.Poll(); // 消えたのを見る
        state.Value = true; watcher.Poll();  // 起動し直したアプリ
        Assert.Equal(2, notifier.Notices.Count);
    }

    [Fact]
    public void WatcherYields_ClaimedAppIsSkipped_AndZeroDoesNotReset()
    {
        var state = new Running { Value = true };
        var process = new FakeProcess();
        var (watcher, notifier) = Build(() => state.Value ? new[] { process } : Array.Empty<ICloseableProcess>());
        watcher.Poll();                     // ウォッチャーが通知 (1 回目)
        watcher.Claim("slack.exe");
        state.Value = false; watcher.Poll(); // 所有中なので 0 件を見ても解除しない
        watcher.ReportStopped("slack.exe");  // StopApp の通知は共有の 1 回に吸収される
        watcher.Release("slack.exe");
        Assert.Single(notifier.Notices);
    }

    [Fact]
    public void WatcherYields_ClaimDuringPoll_ZeroDoesNotReset()
    {
        var notifier = new FakeLockNotifier();
        LaunchWatcher? watcher = null;
        // StopApp が所有を始めたのが、Poll が名前を選んだ後・件数を数える前だった場面
        watcher = new LaunchWatcher(notifier, _ => { watcher!.Claim("slack.exe"); return Array.Empty<ICloseableProcess>(); });
        watcher.Update(Plan("slack.exe"));
        watcher.ReportStopped("slack.exe");  // 1 回目の通知
        watcher.Poll();                      // 所有中に 0 件を見ても解除しない
        watcher.ReportStopped("slack.exe");
        Assert.Single(notifier.Notices);
    }

    [Fact]
    public void SharedNotify_StopAppNoticeCountsForTheLaunch()
    {
        var (watcher, notifier) = Build(() => new[] { new FakeProcess() });
        watcher.ReportStopped("slack.exe");
        watcher.Poll();
        Assert.Single(notifier.Notices);
        Assert.Equal("ロック中: sns-gate", notifier.Notices[0].Title);
    }

    private sealed class Running { public bool Value { get; set; } }

    private static (LaunchWatcher, FakeLockNotifier) Build(Func<ICloseableProcess[]> processes)
    {
        var notifier = new FakeLockNotifier();
        var watcher = new LaunchWatcher(notifier, name => name == "slack" ? processes() : Array.Empty<ICloseableProcess>());
        watcher.Update(Plan("slack.exe"));
        return (watcher, notifier);
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
