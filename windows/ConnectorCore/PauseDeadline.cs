using System.Globalization;

namespace ConnectorCore;

public static class PauseDeadline
{
    public static DateTime NextDayStart(DateTime now) =>
        DateTime.SpecifyKind(now.Date.AddDays(1).AddHours(Constants.PauseNextDayStartHour), DateTimeKind.Local);

    /// <summary>RFC3339 with numeric offset, e.g. 2026-09-07T05:00:00+09:00.</summary>
    public static string ToRfc3339(DateTime local) =>
        local.ToString("yyyy-MM-dd'T'HH:mm:sszzz", CultureInfo.InvariantCulture);
}
