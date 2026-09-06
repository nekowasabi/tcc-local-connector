using System.Diagnostics;
using ConnectorCore;

namespace TCCLocalConnector;

/// <summary>Executes plan actions; bundle_id is interpreted as a Windows executable name (e.g. "slack.exe").</summary>
internal sealed class WindowsPlanExecutor
{
    private readonly NotifyIcon _icon;

    public WindowsPlanExecutor(NotifyIcon icon) => _icon = icon;

    public List<ActionOutcome> Execute(IReadOnlyList<PlanAction> actions, bool dryRun)
    {
        var outcomes = new List<ActionOutcome>(actions.Count);
        foreach (var action in actions)
        {
            if (dryRun)
            {
                outcomes.Add(ActionOutcome.Skipped);
                continue;
            }
            outcomes.Add(action.Type switch
            {
                "app.start" => StartApp(action),
                // Why: keep going after quit_refused so later app.stop entries still run.
                "app.stop" => StopApp(action),
                "notify" => Notify(action),
                _ => ActionOutcome.Rejected("unsupported_action"),
            });
        }
        return outcomes;
    }

    internal static string ProcessName(string bundleId) => Path.GetFileNameWithoutExtension(bundleId);

    private static Process[] Running(string bundleId) => Process.GetProcessesByName(ProcessName(bundleId));

    private static bool IsRunning(string bundleId)
    {
        var processes = Running(bundleId);
        foreach (var process in processes) process.Dispose();
        return processes.Length > 0;
    }

    private static ActionOutcome StartApp(PlanAction action)
    {
        if (string.IsNullOrEmpty(action.BundleId)) return ActionOutcome.Rejected("invalid_action");
        if (IsRunning(action.BundleId)) return ActionOutcome.Skipped;
        try
        {
            // Why: ShellExecute resolves the App Paths registry, so "slack.exe" starts even when not on PATH.
            Process.Start(new ProcessStartInfo(action.BundleId) { UseShellExecute = true });
            return ActionOutcome.Accepted;
        }
        catch (Exception)
        {
            return ActionOutcome.Rejected("app_not_found");
        }
    }

    private static ActionOutcome StopApp(PlanAction action)
    {
        if (string.IsNullOrEmpty(action.BundleId)) return ActionOutcome.Rejected("invalid_action");
        var processes = Running(action.BundleId);
        if (processes.Length == 0) return ActionOutcome.Skipped;
        foreach (var process in processes)
        {
            try { process.CloseMainWindow(); } catch (Exception) { }
            process.Dispose();
        }
        var grace = Math.Max(1, action.GraceSeconds ?? 10);
        var deadline = DateTime.UtcNow.AddSeconds(grace);
        while (DateTime.UtcNow < deadline)
        {
            if (!IsRunning(action.BundleId)) return ActionOutcome.Accepted;
            Thread.Sleep(Constants.AppStopPollIntervalMilliseconds);
        }
        // Why: Windows has no Quit AppleEvent; WM_CLOSE is ignored by tray-resident apps, so escalate to Kill after grace.
        foreach (var process in Running(action.BundleId))
        {
            try { process.Kill(); } catch (Exception) { }
            process.Dispose();
        }
        Thread.Sleep(Constants.AppStopPollIntervalMilliseconds);
        return IsRunning(action.BundleId) ? ActionOutcome.Rejected("quit_refused") : ActionOutcome.Accepted;
    }

    private ActionOutcome Notify(PlanAction action)
    {
        var title = string.IsNullOrWhiteSpace(action.Title) ? "notify" : action.Title;
        var message = string.IsNullOrWhiteSpace(action.Message) ? "notify" : action.Message;
        _icon.ShowBalloonTip(5000, title, message, ToolTipIcon.Info);
        return ActionOutcome.Accepted;
    }
}
