import XCTest
@testable import ConnectorCore

final class StatusIconTests: XCTestCase {
    func testStatusIconDescribeIsUniqueForAllStates() {
        let states: [BackendState] = [.starting, .running, .backendDown, .backendIncompatible, .terminated]
        let labels = states.map { StatusIcon.describe($0) }
        let uniqueCount = Set(labels).count
        XCTAssertEqual(uniqueCount, labels.count)
    }
}
