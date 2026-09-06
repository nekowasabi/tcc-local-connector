using System.Text.Json;
using ConnectorCore;
using Xunit;

public class NativeMessagingManifestTests
{
    [Fact]
    public void BuildsFirefoxManifest()
    {
        using var doc = JsonDocument.Parse(NativeMessagingManifest.Build(@"C:\app\tcc-firefox-native-host.exe"));
        var root = doc.RootElement;
        Assert.Equal("jp.takets.tcc_local_connector.firefox", root.GetProperty("name").GetString());
        Assert.Equal(@"C:\app\tcc-firefox-native-host.exe", root.GetProperty("path").GetString());
        Assert.Equal("stdio", root.GetProperty("type").GetString());
        Assert.Equal("firefox-domain-blocker@tcc-local-connector.takets.jp", root.GetProperty("allowed_extensions")[0].GetString());
    }

    [Fact]
    public void ManifestPathIsUnderAppData()
    {
        var path = NativeMessagingManifest.ManifestPath("/appdata");
        Assert.Equal(Path.Combine("/appdata", "TCCLocalConnector", "NativeMessagingHosts", "jp.takets.tcc_local_connector.firefox.json"), path);
    }
}
