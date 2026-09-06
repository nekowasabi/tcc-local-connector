using ConnectorCore;
using Microsoft.Win32;

namespace TCCLocalConnector;

internal static class NativeMessagingInstaller
{
    /// <summary>Writes the Firefox manifest and points HKCU at it. Returns true when registered or skipped.</summary>
    public static bool Register()
    {
        var hostPath = Path.Combine(AppContext.BaseDirectory, NativeMessagingManifest.HostExecutableName);
        if (!File.Exists(hostPath)) return true;
        try
        {
            var manifestPath = NativeMessagingManifest.ManifestPath();
            var directory = Path.GetDirectoryName(manifestPath)!;
            Directory.CreateDirectory(directory);
            var temporary = Path.Combine(directory, $".{NativeMessagingManifest.HostName}.{Guid.NewGuid():N}.tmp");
            File.WriteAllText(temporary, NativeMessagingManifest.Build(hostPath));
            // Why: Firefox must never observe a half-written manifest; move is atomic on NTFS.
            File.Move(temporary, manifestPath, overwrite: true);
            using var key = Registry.CurrentUser.CreateSubKey(NativeMessagingManifest.RegistryKeyPath);
            key.SetValue(null, manifestPath);
            return true;
        }
        catch (Exception)
        {
            return false;
        }
    }
}
