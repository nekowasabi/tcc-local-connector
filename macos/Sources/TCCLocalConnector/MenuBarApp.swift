import SwiftUI
import ConnectorCore

@main struct TCCLocalConnectorApp: App {
    @StateObject private var controller = MenuController()

    var body: some Scene {
        MenuBarExtra("TCC Local Connector", systemImage: StatusIcon.symbolName(controller.state)) {
            MenuContent(controller: controller)
                .task { controller.start() }
        }
    }
}

private struct MenuContent: View {
    @ObservedObject var controller: MenuController

    var body: some View {
        Button("状態表示: \(StatusIcon.describe(controller.state))") { controller.perform(.showStatus) }
        Button("警告表示: \(controller.recentWarning.isEmpty ? "なし" : controller.recentWarning)") { controller.perform(.showWarning) }
        Text("現在のタスク: \(controller.status?.runningTasks.first?.name ?? "未取得")")
        Text("最終取得: \(controller.lastUpdated?.formatted(date: .omitted, time: .standard) ?? "未取得")")
        Divider()
        Button("今すぐ再取得") { controller.perform(.refresh) }
        Button("設定を再読込") { controller.perform(.reloadConfig) }
        Button("設定ファイルを開く") { controller.perform(.openConfig) }
        Button("ログを開く") { controller.perform(.openLog) }
        Divider()
        Button("15分間一時停止") { controller.perform(.pause(Constants.pausePresetShortSeconds)) }
        Button("1時間一時停止") { controller.perform(.pause(Constants.pausePresetLongSeconds)) }
        Button("明日の開始時刻まで一時停止") { controller.perform(.pauseUntilNextDayStart) }
        Button("一時停止を解除") { controller.perform(.resume) }
        Divider()
        Button("完全終了…") { controller.perform(.quit) }
    }
}
