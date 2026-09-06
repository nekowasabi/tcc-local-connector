using ConnectorCore;
using Xunit;

public class PauseDeadlineTests
{
    [Fact]
    public void NextDayStartIsTomorrowAtFive()
    {
        var deadline = PauseDeadline.NextDayStart(new DateTime(2026, 9, 6, 23, 30, 0, DateTimeKind.Local));
        Assert.Equal(new DateTime(2026, 9, 7, 5, 0, 0), deadline);
        Assert.Equal(DateTimeKind.Local, deadline.Kind);
        Assert.Matches(@"^2026-09-07T05:00:00[+-]\d{2}:\d{2}$", PauseDeadline.ToRfc3339(deadline));
    }
}
