import Foundation
public enum ActionOutcome: Sendable, Equatable { case accepted; case skipped; case rejected(String) }
