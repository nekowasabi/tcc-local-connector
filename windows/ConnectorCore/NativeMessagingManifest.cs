using System.Text.Json;

namespace ConnectorCore;

public static class NativeMessagingManifest
{
    public const string HostName = "jp.takets.tcc_local_connector.firefox";
    public const string HostExecutableName = "tcc-firefox-native-host.exe";
    public const string RegistryKeyPath = @"Software\Mozilla\NativeMessagingHosts\" + HostName;
    private const string Description = "TCC Local Connector Firefox native messaging host";
    private static readonly string[] AllowedExtensions = { "firefox-domain-blocker@tcc-local-connector.takets.jp" };

    public static string Build(string hostPath) => JsonSerializer.Serialize(new
    {
        name = HostName,
        description = Description,
        path = hostPath,
        type = "stdio",
        allowed_extensions = AllowedExtensions,
    }, new JsonSerializerOptions { WriteIndented = true });

    public static string ManifestPath(string? appData = null) => Path.Combine(
        appData ?? Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
        "TCCLocalConnector", "NativeMessagingHosts", HostName + ".json");
}
