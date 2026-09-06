using System.Text;
using ConnectorCore;
using Xunit;

public class BackendClientTests
{
    private const string ReadyAll = """{"version":1,"event":"ready","data":{"protocol_version":1,"capabilities":[{"name":"status"},{"name":"reload_config"},{"name":"pause"},{"name":"resume"},{"name":"refresh_now"},{"name":"config_paths"},{"name":"report_actions"}]}}""";

    private static void Feed(BackendClient client, string text) => client.ConsumeStdout(Encoding.UTF8.GetBytes(text + "\n"));

    [Fact]
    public void ReadyWithRequiredCapabilitiesBecomesRunning()
    {
        var client = new BackendClient();
        Feed(client, ReadyAll);
        Assert.Equal(BackendState.Running, client.State);
    }

    [Fact]
    public void ReadyMissingCapabilityBecomesIncompatible()
    {
        var client = new BackendClient();
        Feed(client, """{"version":1,"event":"ready","data":{"protocol_version":1,"capabilities":[{"name":"status"}]}}""");
        Assert.Equal(BackendState.BackendIncompatible, client.State);
    }

    [Fact]
    public void RoutesPlansNotificationsAndResponses()
    {
        var client = new BackendClient();
        Feed(client, """{"version":1,"event":"event.plan","data":{"cycle_id":7,"dry_run":false,"actions":[{"action_id":"a1","phase":"start","kind":"app.stop","bundle_id":"slack.exe","grace_seconds":3,"reason":"r","priority":1}],"enforce_stop_bundle_ids":["slack.exe"]}}""");
        Feed(client, """{"version":1,"event":"event.notify","data":{"level":"warn","code":"config_error","title":"t","message":"m","at":"2026-09-06T01:02:03.123456789Z"}}""");
        Feed(client, """{"version":1,"id":"menu-1","result":{"state":"active","cycle_id":7,"parse_ok":true,"running_tasks":[{"name":"Task","task_id":"x"}]}}""");
        Feed(client, """{"version":1,"id":"menu-2","error":{"code":"bad","message":"boom"}}""");

        var plan = Assert.Single(client.TakePlans());
        Assert.Equal(7, plan.CycleId);
        Assert.Equal("app.stop", plan.Actions[0].Type);
        Assert.Equal("a1", plan.Actions[0].Id);
        Assert.Equal(3, plan.Actions[0].GraceSeconds);
        Assert.Equal(new[] { "slack.exe" }, plan.EnforceStopBundleIds);
        Assert.Equal("config_error", Assert.Single(client.TakeNotifications()).Code);
        var responses = client.TakeResponses();
        Assert.Equal(2, responses.Count);
        Assert.Equal("active", responses[0].Result!.Value.GetProperty("state").GetString());
        Assert.Equal("boom", responses[1].Error!.Message);
        Assert.Empty(client.TakeResponses());
    }
}
