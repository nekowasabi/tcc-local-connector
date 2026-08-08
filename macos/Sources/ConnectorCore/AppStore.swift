import Foundation
import Combine
@MainActor public final class AppStore: ObservableObject { @Published public var state: BackendState = .starting; @Published public var status: StatusPayload?; public init() {} }
