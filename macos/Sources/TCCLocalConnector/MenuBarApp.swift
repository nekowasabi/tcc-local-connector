import SwiftUI
import ConnectorCore

@main struct TCCLocalConnectorApp: App {
    var body: some Scene {
        MenuBarExtra("TCC Local Connector", systemImage: "checklist") {
            Text("現在のタスク: 未取得")
            Text("状態: starting")
            Text("最終取得: 未取得")
            Divider()
            Button("今すぐ再取得") {}
            Button("設定を再読込") {}
            Button("設定ファイルを開く") {}
            Button("ログを開く") {}
            Divider()
            Button("15分間一時停止") {}
            Button("1時間一時停止") {}
            Button("明日の開始時刻まで一時停止") {}
            Button("一時停止を解除") {}
            Divider()
            Button("完全終了…") { NSApplication.shared.terminate(nil) }
        }
    }
}
