# macOS 検証証跡（2026-08-07）

## E2E マーカー

`internal/engine/e2e_test.go` が実バイナリと `testdata/fake-tcc2.sh` を起動し、stdio の
`ready`、`plan`、`report_actions accepted`、`paused`、`resumed` を1実行で検証した。

## P02 実機確認（FetchTaskChute）

`tcc2` CLI を実環境で起動し、`FetchTaskChute` を1回実行した（実行時は簡易の Go スニペットで `FetchTaskChute(ctx, "tcc2", []string{"mcp"}, nil)` を呼び出した）。

出力:
`running_tasks=0`、`status_counts=map[Done:76 Todo:16]`。  
`[In Progress]` タスクの実在確認ではなく、実 API 応答の解析系（`done/todo` の集計、`parse error` 非発生）を確認。

## VM/OQ

`sw_vers` で macOS 26.5.1 を確認し、`xcrun simctl list devices available` を試行したが利用可能な起動済み端末は0件だった。GUI 操作・署名済みアプリの利用者権限を必要とする VM-1〜VM-5 は未達（この実行環境では自動実施不能）。OQ-1（通常終了時の TCC 挙動）、OQ-2（ad-hoc 署名の通知・ログイン項目）、OQ-3（Start of Day の符号）は未達として分類する。

## W10 LOC 判断

`bash scripts/loc-report.sh` が Go と Swift の非テスト行数を測定し、各 1200 行閾値との比較結果を出力する。超過時は追加分割のレビューが必要、未超過時は現構成を維持する。

実測は `go_nontest_loc=2862`、`swift_nontest_loc=586`、判定は
`loc_decision=exceeds_threshold; W10 requires documented review` だった。したがって W10 は
LOC 超過として記録し、Go 側の分割レビューを保留する。

## 自動検証結果

- `go test ./... -race -count=1`: 成功
- `staticcheck ./...`: 成功（出力なし）
- `swift test --package-path macos` / `swift build --package-path macos -c release`: 成功
- `bash scripts/forbidden-audit.sh`: 成功
- `bash scripts/verify-all.sh`: 成功（`ALL AUTOMATED VERIFICATION PASSED`）

## §18 段階別の分類

- 段階1（設定解析、状態機械、改行区切り JSON、版不一致・不正入力・取消・EOF）: 達成。`go test ./internal/{config,state,protocol}` が対象ケースを検証する。応答順序入れ替わり、バックエンド異常終了後の実プロセス再起動、1万回更新のリーク計測は未達（専用の実プロセス試験なし）。
- 段階2（MCP 初期化、`get_taskchute`、`[In Progress]` 抽出、複数実行中、日付境界）: 達成。P18 の擬似 MCP と `internal/tcc2` の解析テストで検証する。実サービスでの認証切れ・異常終了後の再接続は未達（認証情報を使う外部検証が必要）。
- 段階3（名前条件、優先順位、競合、`on_enter`、`on_exit`、`ensure`、設定再読込、不正設定拒否）: 達成。`internal/rules` と `internal/config` の自動テストで検証する。
- 段階4（アプリ起動、重複防止、通常終了、終了拒否、起動通知、同一 bundle ID 複数プロセス）: 未達（VM-1/VM-2/VM-4 は GUI 実機操作が必要）。Windows 実行ファイル識別と UAC 権限差は対象外（macOS 専用）。
- 段階5（管理対象 CLI の起動・終了、PID 再利用、子プロセス、タイムアウト、重複防止、状態復元、引数注入拒否）: 達成。台帳・PID 同一性・通常終了のみ・dry_run の自動テストで検証する。nvim、tmux、`wsl.exe`、WSL パス境界と WSL 再接続は対象外（macOS 専用かつ MVP 範囲外）。
- 段階6（常駐 UI、現在タスク表示、最終取得時刻、期限付き一時停止、スリープ復帰、完全終了、ログイン時起動、再起動ループ抑制）: 一時停止・wake 検知・再起動待機列は達成（自動テスト）。実際のメニュー表示、スリープ跨ぎ、ログイン時起動は未達（VM-1/VM-3 が必要）。Windows 通知領域と Explorer 再起動は対象外（macOS 専用）。
- 段階7（ブラウザ拡張）: 対象外（別コンポーネント）。
