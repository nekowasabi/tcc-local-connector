using System.Globalization;

namespace ConnectorCore;

/// <summary>Keeps the Firefox native host's owner-heartbeat file fresh while the backend is healthy.</summary>
public sealed class BrowserPolicyHeartbeat : IDisposable
{
    public const string DirectoryRelativePath = "TCCLocalConnector/BrowserPolicy";
    public const string FileName = "firefox-owner-heartbeat.json";
    public const int PolicyVersion = 1;

    private readonly object _lock = new();
    private readonly Func<DateTime> _now;
    private Timer? _timer;
    private bool _active;

    public string HeartbeatPath { get; }

    public BrowserPolicyHeartbeat(string? stateDir = null, Func<DateTime>? now = null)
    {
        // Why: Go's os.UserConfigDir is %APPDATA% on Windows; the native host reads from the same root.
        stateDir ??= Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), DirectoryRelativePath);
        HeartbeatPath = Path.Combine(stateDir, FileName);
        _now = now ?? (() => DateTime.UtcNow);
    }

    public bool StartOrRefresh()
    {
        lock (_lock)
        {
            _timer?.Dispose();
            _timer = null;
            _active = true;
            if (!Write())
            {
                Stop();
                return false;
            }
            var interval = TimeSpan.FromSeconds(Constants.HeartbeatIntervalSeconds);
            _timer = new Timer(_ => Refresh(), null, interval, interval);
            return true;
        }
    }

    public void Stop()
    {
        lock (_lock)
        {
            _active = false;
            _timer?.Dispose();
            _timer = null;
            try { File.Delete(HeartbeatPath); } catch (IOException) { } catch (UnauthorizedAccessException) { }
        }
    }

    public void Dispose() => Stop();

    private void Refresh()
    {
        lock (_lock)
        {
            if (!_active) return;
            if (!Write()) Stop();
        }
    }

    private bool Write()
    {
        var directory = Path.GetDirectoryName(HeartbeatPath)!;
        var temporary = Path.Combine(directory, $".heartbeat-{Guid.NewGuid():N}.tmp");
        try
        {
            Directory.CreateDirectory(directory);
            File.WriteAllText(temporary, $"{{\"version\":{PolicyVersion},\"updated_at\":\"{Rfc3339Nano(_now())}\"}}");
            // Why: move-with-overwrite is atomic on NTFS, so the native host never reads a partial file.
            File.Move(temporary, HeartbeatPath, overwrite: true);
            return true;
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            try { File.Delete(temporary); } catch (IOException) { } catch (UnauthorizedAccessException) { }
            try { File.Delete(HeartbeatPath); } catch (IOException) { } catch (UnauthorizedAccessException) { }
            return false;
        }
    }

    /// <summary>Go's time.RFC3339Nano needs 9 fractional digits; .NET has 7, so pad two zeros.</summary>
    public static string Rfc3339Nano(DateTime utc) =>
        utc.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss.fffffff", CultureInfo.InvariantCulture) + "00Z";
}
