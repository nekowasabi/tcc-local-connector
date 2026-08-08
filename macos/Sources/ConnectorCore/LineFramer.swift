import Foundation

public struct LineFramer: Sendable {
    private var buffer = Data()
    public init() {}
    public mutating func push(_ data: Data) -> [Data] {
        buffer.append(data)
        var lines: [Data] = []
        while let index = buffer.firstIndex(of: 10) {
            let line = buffer.prefix(upTo: index)
            buffer.removeSubrange(...index)
            if line.count <= Constants.frontendMaxLineBytes { lines.append(Data(line)) }
        }
        if buffer.count > Constants.frontendMaxLineBytes { buffer.removeAll(keepingCapacity: true) }
        return lines
    }
}
