using ConnectorCore;
using Xunit;

public sealed class LaunchWatcherTests
{
    [Fact]
    public void Poll_MatchingProcess_ClosesAndNotifiesLocked()
    {
        var process = new FakeProcess();
        var notifier = new FakeLockNotifier();
        using var watcher = new WatcherScope(notifier, name => name == "slack" ? new[] { process } : Array.Empty<ICloseableProcess>());

        watcher.Inner.Update(new HashSet<string> { "slack.exe" });
        watcher.Inner.Poll();

        Assert.True(process.Closed || process.Killed);
        Assert.True(process.Disposed);
        var posted = Assert.Single(notifier.Names);
        Assert.Equal("slack", posted);
        Assert.Equal("Locked", LaunchLockNotification.Title);
        Assert.Contains("locked", LaunchLockNotification.Body("slack"), StringComparison.OrdinalIgnoreCase);
    }

    [Fact]
    public void Poll_NonMatchingName_DoesNotCloseOrNotify()
    {
        var process = new FakeProcess();
        var notifier = new FakeLockNotifier();
        using var watcher = new WatcherScope(notifier, name => name == "slack" ? new[] { process } : Array.Empty<ICloseableProcess>());

        watcher.Inner.Update(new HashSet<string> { "other.exe" });
        watcher.Inner.Poll();

        Assert.False(process.Closed);
        Assert.False(process.Killed);
        Assert.Empty(notifier.Names);
    }

    private sealed class FakeLockNotifier : ILockNotifier
    {
        public List<string> Names { get; } = new();
        public void NotifyLocked(string processName) => Names.Add(processName);
    }

    private sealed class FakeProcess : ICloseableProcess
    {
        public bool Closed { get; private set; }
        public bool Killed { get; private set; }
        public bool Disposed { get; private set; }
        public bool CloseMainWindow() { Closed = true; return true; }
        public void Kill() => Killed = true;
        public void Dispose() => Disposed = true;
    }

    private sealed class WatcherScope : IDisposable
    {
        public LaunchWatcher Inner { get; }

        public WatcherScope(ILockNotifier notifier, Func<string, IReadOnlyList<ICloseableProcess>> processes)
        {
            Inner = new LaunchWatcher(notifier, processes, () => DateTime.UtcNow);
        }

        public void Dispose() => Inner.Stop();
    }
}
