import XCTest
@testable import ConnectorCore

final class LineFramerTests: XCTestCase {
    func testFramesLines() {
        var framer = LineFramer()
        XCTAssertEqual(framer.push(Data("one\ntwo\n".utf8)).map { String(decoding: $0, as: UTF8.self) }, ["one", "two"])
    }

    func testFramesByteByByte() {
        var framer = LineFramer()
        let input = Array("one\ntwo\n".utf8)
        var lines: [String] = []
        for byte in input {
            for line in framer.push(Data([byte])) {
                let decoded = String(data: line, encoding: .utf8) ?? ""
                lines.append(contentsOf: decoded.split(separator: "\n").map(String.init))
            }
        }
        XCTAssertEqual(lines, ["one", "two"])
    }

    func testFramesChunkedData() {
        var framer = LineFramer()
        let first = framer.push(Data("one\ntw".utf8))
        let second = framer.push(Data("o\n".utf8))
        XCTAssertEqual(first.map { String(decoding: $0, as: UTF8.self) }, ["one"])
        XCTAssertEqual(second.map { String(decoding: $0, as: UTF8.self) }, ["two"])
    }

    func testFramesOversizedLineAndRecovers() {
        var framer = LineFramer()
        let tooLong = String(repeating: "x", count: Constants.frontendMaxLineBytes + 1) + "\n"
        _ = framer.push(Data(tooLong.utf8))
        let recovered = framer.push(Data("ok\n".utf8))
        XCTAssertEqual(recovered.map { String(decoding: $0, as: UTF8.self) }, ["ok"])
    }
}
