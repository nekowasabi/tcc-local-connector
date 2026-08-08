public enum StatusIcon {
    public static func describe(_ state: BackendState) -> String {
        switch state {
        case .starting: return "時計"
        case .running: return "チェック"
        case .backendDown: return "警告"
        case .backendDownPermanent: return "停止"
        case .backendIncompatible: return "互換性なし"
        case .terminated: return "終了"
        }
    }
}
