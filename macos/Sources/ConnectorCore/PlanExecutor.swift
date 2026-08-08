import Foundation
public enum ActionOutcome: Sendable, Equatable { case accepted; case skipped; case rejected(String) }
public protocol PlanExecutor {
    func execute(_ actions: [PlanAction], dryRun: Bool) async -> [ActionOutcome]
}

public extension PlanExecutor {
    func execute(_ plan: PlanPayload) async -> [ActionOutcome] {
        await execute(plan.actions, dryRun: plan.dryRun)
    }
}
