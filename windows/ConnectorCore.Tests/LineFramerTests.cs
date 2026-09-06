using System.Text;
using ConnectorCore;
using Xunit;

public class LineFramerTests
{
    [Fact]
    public void SplitsOnNewlineAndKeepsPartialTail()
    {
        var framer = new LineFramer();
        var first = framer.Push(Encoding.UTF8.GetBytes("a\nbc\nd"));
        Assert.Equal(new[] { "a", "bc" }, first.Select(Encoding.UTF8.GetString));
        var second = framer.Push(Encoding.UTF8.GetBytes("e\n"));
        Assert.Equal(new[] { "de" }, second.Select(Encoding.UTF8.GetString));
    }

    [Fact]
    public void DropsLinesOverTheCap()
    {
        var framer = new LineFramer();
        var big = new byte[Constants.FrontendMaxLineBytes + 1];
        Array.Fill(big, (byte)'x');
        Assert.Empty(framer.Push(big));
        // Why: the dropped tail must not leak into the next line; the bare '\n' yields an empty line like Swift.
        var lines = framer.Push(Encoding.UTF8.GetBytes("\nok\n"));
        Assert.Equal(new[] { "", "ok" }, lines.Select(Encoding.UTF8.GetString));
    }
}
