using System.Diagnostics;
using System.Text.Json;
using ConnectorCore;

namespace TCCLocalConnector;

/// <summary>Tray-only WinForms context; a port of the macOS MenuController + MenuBarApp.</summary>
internal sealed class TrayApplicationContext : ApplicationContext
{
    private const string BackendExecutableName = "tcc-local-connector-backend.exe";

    private readonly NotifyIcon _icon;
    private readonly BackendClient _backend = new();
    private readonly WindowsPlanExecutor _executor;
    private readonly LaunchWatcher _watcher;
    private readonly BrowserPolicyHeartbeat _heartbeat = new();
    private readonly System.Windows.Forms.Timer _timer = new() { Interval = Constants.MenuRefreshIntervalSeconds * 1000 };
    private readonly ToolStripMenuItem _stateItem = new();
    private readonly ToolStripMenuItem _warningItem = new();
    private readonly ToolStripMenuItem _taskItem = new();
    private readonly ToolStripMenuItem _updatedItem = new();

    private BackendState _state = BackendState.Starting;
    private StatusPayload? _status;
    private DateTime? _lastUpdated;
    private string _recentWarning = "";
    private Dictionary<string, string> _paths = new();

    public TrayApplicationContext()
    {
        var menu = new ContextMenuStrip();
        _stateItem.Click += (_, _) => Synchronize();
        _warningItem.Click += (_, _) => Synchronize();
        _taskItem.Enabled = false;
        _updatedItem.Enabled = false;
        menu.Items.AddRange(new ToolStripItem[] { _stateItem, _warningItem, _taskItem, _updatedItem, new ToolStripSeparator() });
        menu.Items.Add("今すぐ再取得", null, (_, _) => Send("refresh_now"));
        menu.Items.Add("設定を再読込", null, (_, _) => Send("reload_config"));
        menu.Items.Add("設定ファイルを開く", null, (_, _) => OpenPath("config"));
        menu.Items.Add("ログを開く", null, (_, _) => OpenPath("log"));
        menu.Items.Add(new ToolStripSeparator());
        menu.Items.Add("15分間一時停止", null, (_, _) => Pause(Constants.PausePresetShortSeconds));
        menu.Items.Add("1時間一時停止", null, (_, _) => Pause(Constants.PausePresetLongSeconds));
        menu.Items.Add("明日の開始時刻まで一時停止", null, (_, _) => PauseUntilNextDayStart());
        menu.Items.Add("一時停止を解除", null, (_, _) => Send("resume"));
        menu.Items.Add(new ToolStripSeparator());
        menu.Items.Add("完全終了…", null, (_, _) => Quit());
        menu.Opening += (_, _) => RefreshMenuText();
        RefreshMenuText();

        _icon = new NotifyIcon
        {
            Text = "TCC Local Connector",
            Icon = SystemIcons.Application,
            Visible = true,
            ContextMenuStrip = menu,
        };
        _executor = new WindowsPlanExecutor(_icon);
        _watcher = new LaunchWatcher(new BalloonLockNotifier(_icon));
        _timer.Tick += (_, _) => Synchronize();
        Start();
    }

    private void RefreshMenuText()
    {
        _stateItem.Text = $"状態表示: {StatusIcon.Describe(_state)}";
        _warningItem.Text = $"警告表示: {(_recentWarning.Length == 0 ? "なし" : _recentWarning)}";
        _taskItem.Text = $"現在のタスク: {_status?.RunningTasks.FirstOrDefault()?.Name ?? "未取得"}";
        _updatedItem.Text = $"最終取得: {_lastUpdated?.ToString("HH:mm:ss") ?? "未取得"}";
    }

    private void Start()
    {
        // Why: register the host from the running app so the manifest path follows wherever the exe lives.
        if (!NativeMessagingInstaller.Register()) _recentWarning = "ブラウザ拡張の Host を登録できません";
        var executable = Path.Combine(AppContext.BaseDirectory, BackendExecutableName);
        if (!File.Exists(executable))
        {
            _heartbeat.Stop();
            _state = BackendState.BackendDown;
            _recentWarning = "同梱バックエンドが見つかりません";
            return;
        }
        try
        {
            // Why: a config.yml beside the exe wins over %USERPROFILE%\.config so a portable folder works as-is.
            var localConfig = Path.Combine(AppContext.BaseDirectory, "config.yml");
            var arguments = File.Exists(localConfig) ? new[] { "serve", "--stdio", "--config", localConfig } : new[] { "serve", "--stdio" };
            _backend.Launch(executable, arguments);
            Synchronize();
            _timer.Start();
        }
        catch (Exception)
        {
            _heartbeat.Stop();
            _state = BackendState.BackendDown;
            _recentWarning = "バックエンドを起動できません";
        }
    }

    private void Synchronize()
    {
        try
        {
            _backend.Send("status");
            _backend.Send("config_paths");
        }
        catch (Exception)
        {
            _heartbeat.Stop();
            _state = BackendState.BackendDown;
            _recentWarning = "バックエンドへ要求を送信できません";
            return;
        }
        var healthy = ConsumeBackendMessages();
        _state = _backend.State;
        if (!healthy || _state != BackendState.Running) _heartbeat.Stop();
    }

    private void Send(string method, object? @params = null)
    {
        try
        {
            _backend.Send(method, @params);
        }
        catch (Exception)
        {
            _heartbeat.Stop();
            _state = BackendState.BackendDown;
            _recentWarning = "バックエンドへ要求を送信できません";
        }
    }

    private bool ConsumeBackendMessages()
    {
        var receivedHealthyStatus = false;
        var responseFailed = false;
        foreach (var notification in _backend.TakeNotifications())
            _recentWarning = notification.Message.Length == 0 ? notification.Code : notification.Message;
        foreach (var response in _backend.TakeResponses())
        {
            if (response.Error is { } error)
            {
                responseFailed = true;
                _recentWarning = error.Message.Length == 0 ? error.Code : error.Message;
                continue;
            }
            if (response.Result is not { ValueKind: JsonValueKind.Object } result)
            {
                responseFailed = true;
                continue;
            }
            if (result.TryGetProperty("running_tasks", out _) && result.Deserialize<StatusPayload>() is { } status)
            {
                _status = status;
                _lastUpdated = DateTime.Now;
                if (IsHealthyStatus(status)) receivedHealthyStatus = true; else responseFailed = true;
            }
            else if (result.TryGetProperty("config", out _) && TryPaths(result) is { } paths)
            {
                _paths = paths;
            }
            else
            {
                responseFailed = true;
            }
        }
        foreach (var plan in _backend.TakePlans())
        {
            _watcher.Update(plan.DryRun ? new HashSet<string>() : new HashSet<string>(plan.EnforceStopBundleIds));
            // ponytail: runs on the UI thread and can block up to app.stop grace seconds; move to Task.Run if the menu stalls.
            var outcomes = _executor.Execute(plan.Actions, plan.DryRun);
            var results = plan.Actions.Select((action, index) =>
            {
                var (status, code) = ActionResultMapper.Map(outcomes[index]);
                return new { action_id = action.Id, status, code };
            }).ToList();
            try
            {
                _backend.Send("report_actions", new { cycle_id = plan.CycleId, results });
            }
            catch (Exception)
            {
                _state = BackendState.BackendDown;
                _recentWarning = "アクション結果を報告できません";
            }
        }
        if (responseFailed)
        {
            _heartbeat.Stop();
            return false;
        }
        return receivedHealthyStatus && _heartbeat.StartOrRefresh();
    }

    private static Dictionary<string, string>? TryPaths(JsonElement result)
    {
        try { return result.Deserialize<Dictionary<string, string>>(); }
        catch (JsonException) { return null; }
    }

    // Why: parse_ok false means a task snapshot was invalid, not that enforcement stopped;
    // dropping the heartbeat there would fail-open Firefox.
    private static bool IsHealthyStatus(StatusPayload status) => status.State is "active" or "fetching" or "degraded";

    private void Pause(int seconds)
    {
        _heartbeat.Stop();
        Send("pause", new { duration_seconds = seconds });
    }

    private void PauseUntilNextDayStart()
    {
        _heartbeat.Stop();
        Send("pause", new { until = PauseDeadline.ToRfc3339(PauseDeadline.NextDayStart(DateTime.Now)) });
    }

    private void OpenPath(string name)
    {
        if (!_paths.TryGetValue(name, out var path))
        {
            _recentWarning = "パスを取得中です";
            Send("config_paths");
            return;
        }
        try { Process.Start(new ProcessStartInfo(path) { UseShellExecute = true }); }
        catch (Exception) { _recentWarning = "ファイルを開けません"; }
    }

    private void Quit()
    {
        _timer.Stop();
        _watcher.Stop();
        _heartbeat.Stop();
        _backend.Shutdown();
        _icon.Visible = false;
        _icon.Dispose();
        Application.Exit();
    }
}
