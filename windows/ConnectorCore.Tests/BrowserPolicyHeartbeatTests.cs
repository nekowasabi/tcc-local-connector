using System.Text.Json;
using ConnectorCore;
using Xunit;

public class BrowserPolicyHeartbeatTests
{
    [Fact]
    public void WritesRfc3339NanoAndStopDeletes()
    {
        var dir = Path.Combine(Path.GetTempPath(), "tcc-heartbeat-" + Guid.NewGuid().ToString("N"));
        var fixedNow = new DateTime(2026, 9, 6, 1, 2, 3, DateTimeKind.Utc).AddTicks(1234567);
        using var heartbeat = new BrowserPolicyHeartbeat(dir, () => fixedNow);
        try
        {
            Assert.True(heartbeat.StartOrRefresh());
            using var doc = JsonDocument.Parse(File.ReadAllText(heartbeat.HeartbeatPath));
            Assert.Equal(1, doc.RootElement.GetProperty("version").GetInt32());
            Assert.Equal("2026-09-06T01:02:03.123456700Z", doc.RootElement.GetProperty("updated_at").GetString());
            heartbeat.Stop();
            Assert.False(File.Exists(heartbeat.HeartbeatPath));
        }
        finally
        {
            Directory.Delete(dir, recursive: true);
        }
    }
}
