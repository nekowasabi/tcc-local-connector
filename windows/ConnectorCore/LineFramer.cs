namespace ConnectorCore;

/// <summary>Splits a byte stream into '\n'-terminated lines; oversized lines are dropped.</summary>
public sealed class LineFramer
{
    private readonly MemoryStream _buffer = new();

    public List<byte[]> Push(ReadOnlySpan<byte> data)
    {
        _buffer.Write(data);
        var lines = new List<byte[]>();
        var bytes = _buffer.GetBuffer().AsSpan(0, (int)_buffer.Length);
        var start = 0;
        int newline;
        while ((newline = bytes[start..].IndexOf((byte)'\n')) >= 0)
        {
            var line = bytes.Slice(start, newline);
            if (line.Length <= Constants.FrontendMaxLineBytes) lines.Add(line.ToArray());
            start += newline + 1;
        }
        var rest = bytes[start..].ToArray();
        _buffer.SetLength(0);
        // Why: a line without a terminator beyond the cap can never become valid; drop it like the Swift framer.
        if (rest.Length <= Constants.FrontendMaxLineBytes) _buffer.Write(rest);
        return lines;
    }
}
