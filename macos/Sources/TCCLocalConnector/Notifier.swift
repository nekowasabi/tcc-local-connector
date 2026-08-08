import Foundation
import ConnectorCore

final class Notifier {
    private(set) var recent: [String] = []

    func post(_ message: String) {
        recent.append(message)
        if recent.count > Constants.notifierRecentCapacity {
            recent.removeFirst(recent.count - Constants.notifierRecentCapacity)
        }
    }

    func recordRecent(_ message: String) {
        post(message)
    }
}
