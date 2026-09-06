using ConnectorCore;
using Xunit;

public class ActionResultMapperTests
{
    [Fact]
    public void MapsEveryOutcome()
    {
        Assert.Equal(("accepted", null), ActionResultMapper.Map(ActionOutcome.Accepted));
        Assert.Equal(("skipped", null), ActionResultMapper.Map(ActionOutcome.Skipped));
        Assert.Equal(("refused", "quit_refused"), ActionResultMapper.Map(ActionOutcome.Rejected("quit_refused")));
        Assert.Equal(("timeout", "timeout"), ActionResultMapper.Map(ActionOutcome.Rejected("timeout")));
        Assert.Equal(("failed", "app_not_found"), ActionResultMapper.Map(ActionOutcome.Rejected("app_not_found")));
    }
}
