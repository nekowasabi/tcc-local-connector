using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace ConnectorCore;

/// <summary>Owns the backend child process and the NDJSON stdio protocol.</summary>
public sealed class BackendClient
{
    private readonly object _lock = new();
    private readonly LineFramer _framer = new();
    private readonly List<PlanPayload> _plans = new();
    private readonly List<NotifyPayload> _notifications = new();
    private readonly List<BackendResponse> _responses = new();
    private Process? _process;
    private Stream? _stdin;
    private int _nextRequestSequence;

    public BackendState State { get; private set; } = BackendState.Terminated;

    public void Launch(string executablePath, IEnumerable<string> arguments)
    {
        Shutdown();
        var info = new ProcessStartInfo(executablePath)
        {
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardInput = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
        };
        foreach (var argument in arguments) info.ArgumentList.Add(argument);
        var process = Process.Start(info) ?? throw new InvalidOperationException("backend process did not start");
        lock (_lock)
        {
            _process = process;
            _stdin = process.StandardInput.BaseStream;
            State = BackendState.Starting;
        }
        // Why: drain stderr so the child never blocks on a full pipe; its content is not surfaced.
        process.ErrorDataReceived += (_, _) => { };
        process.BeginErrorReadLine();
        _ = Task.Run(() => PumpStdout(process.StandardOutput.BaseStream));
    }

    private void PumpStdout(Stream stdout)
    {
        var buffer = new byte[8192];
        try
        {
            int read;
            while ((read = stdout.Read(buffer, 0, buffer.Length)) > 0) ConsumeStdout(buffer.AsSpan(0, read));
        }
        catch (IOException)
        {
        }
        catch (ObjectDisposedException)
        {
        }
    }

    public void Send(string method, object? @params = null)
    {
        lock (_lock)
        {
            if (_stdin is null) throw new InvalidOperationException("backend is not running");
            _nextRequestSequence++;
            var request = new BackendRequest { Id = $"menu-{_nextRequestSequence}", Method = method, Params = @params };
            var payload = Encoding.UTF8.GetBytes(JsonSerializer.Serialize(request) + "\n");
            _stdin.Write(payload);
            _stdin.Flush();
        }
    }

    public List<PlanPayload> TakePlans() => Take(_plans);
    public List<BackendResponse> TakeResponses() => Take(_responses);
    public List<NotifyPayload> TakeNotifications() => Take(_notifications);

    private List<T> Take<T>(List<T> source)
    {
        lock (_lock)
        {
            var taken = new List<T>(source);
            source.Clear();
            return taken;
        }
    }

    /// <summary>Parses raw stdout bytes; callable without a process for tests.</summary>
    public void ConsumeStdout(ReadOnlySpan<byte> data)
    {
        List<byte[]> lines;
        lock (_lock) lines = _framer.Push(data);
        foreach (var line in lines)
        {
            if (line.Length == 0) continue;
            try
            {
                using var document = JsonDocument.Parse(line);
                if (document.RootElement.ValueKind != JsonValueKind.Object) continue;
                if (document.RootElement.TryGetProperty("event", out _))
                {
                    var evt = document.RootElement.Deserialize<BackendEvent>();
                    if (evt is not null) Handle(evt);
                }
                else
                {
                    var response = document.RootElement.Deserialize<BackendResponse>();
                    if (response is not null) lock (_lock) _responses.Add(response);
                }
            }
            catch (JsonException)
            {
            }
        }
    }

    private void Handle(BackendEvent evt)
    {
        if (evt.Data is not { } data) return;
        switch (evt.Event)
        {
            case "ready":
                if (data.TryGetProperty("protocol_version", out var version) && version.TryGetInt32(out var protocolVersion))
                {
                    var capabilities = new HashSet<string>();
                    if (data.TryGetProperty("capabilities", out var list) && list.ValueKind == JsonValueKind.Array)
                        foreach (var item in list.EnumerateArray())
                            if (item.TryGetProperty("name", out var name) && name.GetString() is { } value) capabilities.Add(value);
                    AcceptReady(protocolVersion, capabilities);
                }
                break;
            case "event.plan":
                if (data.Deserialize<PlanPayload>() is { } plan) lock (_lock) _plans.Add(plan);
                break;
            case "event.notify":
                if (data.Deserialize<NotifyPayload>() is { } notification) lock (_lock) _notifications.Add(notification);
                break;
        }
    }

    public void AcceptReady(int protocolVersion, IReadOnlySet<string> capabilities)
    {
        lock (_lock)
        {
            State = protocolVersion == Constants.SupportedProtocolVersion && Constants.RequiredCapabilities.IsSubsetOf(capabilities)
                ? BackendState.Running
                : BackendState.BackendIncompatible;
        }
    }

    public void Shutdown()
    {
        Process? process;
        lock (_lock)
        {
            process = _process;
            _process = null;
            try { _stdin?.Close(); } catch (IOException) { }
            _stdin = null;
            State = BackendState.Terminated;
        }
        if (process is null) return;
        try
        {
            // Why: closing stdin is the graceful stop signal; kill only if the backend ignores it.
            if (!process.WaitForExit(3000)) process.Kill(entireProcessTree: true);
        }
        catch (InvalidOperationException)
        {
        }
        process.Dispose();
    }
}
