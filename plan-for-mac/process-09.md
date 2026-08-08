# Process 09: macOS アクション実行と MenuBarExtra UI

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-09.md` を起動した際の自己完結ブリーフ。

- **背景**: P08 が確立したバックエンド通信の土台の上に、macOS 固有のアクション実行（GUIアプリの起動・通常終了・通知）とメニューバー UI を実装する。security 方針により Automation/アクセシビリティ権限を要求しない API のみを使う制約があり、`app.stop` は「猶予後も終了していなければ必ず `refused` を返し、再試行しない」という安全側の設計が要となる。
- **目的**: `NSWorkspace.openApplication` / `NSRunningApplication.terminate()`、`didLaunchApplicationNotification` による即時是正、MenuBarExtra メニュー、一時停止操作、SMAppService（既定 OFF）、通知のメニュー降格を実装する。
- **変更範囲**: `macos/Sources/TCCLocalConnector/{MacPlanExecutor,LaunchWatcher,MenuBarApp,LoginItem,Notifier}.swift`, `macos/Sources/ConnectorCore/StatusIcon.swift`（新規。配置理由は Symbol Targets の Notes を参照）, `macos/Tests/ConnectorCoreTests/{PlanExecutorTests,AppStoreTests,StatusIconTests}.swift` の新規作成。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| menuRefreshIntervalSeconds | 5 | 秒 | メニュー描画用の `status` ポーリング間隔 |
| appStopPollIntervalMilliseconds | 250 | ミリ秒 | `app.stop` の `isTerminated` 確認間隔 |
| pausePresetsSeconds | [900, 3600] | 秒 | 一時停止プリセット（15分・1時間） |
| pauseNextDayStartHour | 5 | 時（ローカル） | 「明日の開始時刻まで一時停止」の動的計算基準 |
| notifierRecentCapacity | 20 | 件 | `Notifier.recent` のリングバッファ容量 |
| backendRequestTimeoutSeconds | 30 | 秒 | `app.start` completion の打ち切り |
| NotifyTitleMaxRunes | 200 | ルーン | 通知タイトルの上限 |
| NotifyMessageMaxRunes | 500 | ルーン | 通知本文の上限 |
| MinGraceSeconds | 1 | 秒 | `grace_seconds` の下限 |
| DefaultAppStopGraceSeconds | 10 | 秒 | `app.stop` の既定猶予 |
| MaxGraceSeconds | 120 | 秒 | `grace_seconds` の上限 |
| VISUAL_GATE_EVIDENCE_DIR | "docs/screenshots/" | パス | 視覚検証の証跡保存先 |

- **禁止事項**:
  - D-01（強制終了禁止）: `rg -n 'forceTerminate\(|SIGKILL|kill -9' macos/Sources/TCCLocalConnector` → 期待0件（`MacPlanExecutor` は `terminate()` のみを使う）
  - D-02（`runningApplications()` 全列挙禁止・`forceTerminate` 禁止）: `rg -n 'forceTerminate|runningApplications\(\)' macos/Sources` → 期待0件（P09-VG-02 そのもの）
  - D-05（Windows/WSL 向けコード禁止）: `rg -n 'wsl\.exe|GOOS=windows|go:build windows|NotifyIcon|PowerShell|wslpath' macos/Sources` → 期待0件
  - D-07（バンドルID・絶対パス直書き禁止）: `rg -n 'com\.tinyspeck|com\.amazon\.Lassen|/opt/homebrew|/Users/takets' macos/Sources --glob '!**/testdata/**'` → 期待0件
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' macos/Sources/TCCLocalConnector` → 期待0件
  - D-14（`.xcodeproj`/`xcodebuild`/XCUITest 禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' macos/` → 期待0件

- **適用される横断方針（インライン展開）**:
  - **security**: Automation / アクセシビリティ権限を要求しない設計制約。使ってよい API = `NSWorkspace.openApplication(at:configuration:)` / `NSRunningApplication.terminate()` / `NSWorkspace.shared.runningApplications`（**プロパティ参照。引数なしの `runningApplications()` 全列挙メソッドは禁止**）/ `NSWorkspace.didLaunchApplicationNotification` / `Foundation.Process`。使ってはいけない = `NSAppleScript` / `osascript` / System Events / `AXUIElement` / `CGEvent` 合成 / 画面収録を伴う API。
  - **error**: バックエンドからのエラーは表示するが機密情報を含めない。通知失敗を例外にせずメニュー内「最近の警告」（`Notifier.recent`）へ降格する。plan 実行は通知失敗があっても継続する。
  - **PATH の罠**: 本 Process は外部実行ファイルを直接起動しない（`NSWorkspace` 経由のみ）ため該当なし。
  - **stderr の drain**: 本 Process は P08 の `BackendClient` が確立した drain の上で動作する。追加の drain 責務は発生しない。
  - **該当 Don'ts**: 上記「禁止事項」を参照。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`macos/Sources/TCCLocalConnector/`（実行可能ターゲット）に、plan の3種のアクション（`app.start` / `app.stop` / `notify`）を実行する `MacPlanExecutor`、起動検出による即時是正を行う `LaunchWatcher`、11項目のメニューを持つ `MenuBarApp`、ログイン時起動を管理する `LoginItem`、通知のベストエフォート送出とメニュー降格を担う `Notifier` を実装する。`app.stop` は `grace_seconds` 経過後も `isTerminated == false` なら**必ず** `refused/quit_refused` を返し、再試行しない設計にすることで、TCC 権限不足による無言失敗を報告経路上で検出可能にする（OQ-1 の吸収）。通知は最初からベストエフォート実装とし、失敗しても `Notifier.recent` に積んでメニューへ表示することで、通知が使えない環境でも情報が届く（OQ-2 の吸収）。`StatusIcon.describe` は純関数として `ConnectorCore` に置き、MenuBarExtra 自体は自動テストできない代わりに表示ロジックの正しさだけは自動検証する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `macos/Sources/TCCLocalConnector/MacPlanExecutor.swift` | 新規全体 | `MacPlanExecutor`, `.startApp`, `.stopApp`, `.notify` |
| `macos/Sources/TCCLocalConnector/LaunchWatcher.swift` | 新規全体 | `LaunchWatcher`, `LaunchWatcher.update` |
| `macos/Sources/TCCLocalConnector/MenuBarApp.swift` | 新規全体 | `TCCLocalConnectorApp`, `MenuContent`（`ConnectorCore.StatusIcon` を import して描画に使用） |
| `macos/Sources/TCCLocalConnector/LoginItem.swift` | 新規全体 | `LoginItem`, `.isEnabled`, `.setEnabled` |
| `macos/Sources/TCCLocalConnector/Notifier.swift` | 新規全体 | `Notifier`, `.post`, `.recent` |
| `macos/Sources/ConnectorCore/StatusIcon.swift` | 新規全体 | `StatusIcon`, `StatusIcon.describe`（配置理由は Notes を参照） |
| `macos/Tests/ConnectorCoreTests/PlanExecutorTests.swift` | 新規全体 | `MacPlanExecutor` の3アクション全分岐テスト |
| `macos/Tests/ConnectorCoreTests/AppStoreTests.swift` | 新規全体 | `AppStore` と `LaunchWatcher` の連携（`enforce_stop_bundle_ids` 更新）テスト |
| `macos/Tests/ConnectorCoreTests/StatusIconTests.swift` | 新規全体 | `StatusIcon.describe` の10状態組の一意性テスト |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: macos/Sources/TCCLocalConnector/MacPlanExecutor.swift
symbols:
  - name: MacPlanExecutor
    kind: type
    line_hint: top
  - name: MacPlanExecutor.startApp
    kind: method
    line_hint: middle
  - name: MacPlanExecutor.stopApp
    kind: method
    line_hint: middle
  - name: MacPlanExecutor.notify
    kind: method
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
  - file_exists:macos/Resources/Info.plist
  - symbol_exists:macos/Sources/ConnectorCore/BackendClient.swift:BackendClient
---
file: macos/Sources/TCCLocalConnector/LaunchWatcher.swift
symbols:
  - name: LaunchWatcher
    kind: type
    line_hint: top
  - name: LaunchWatcher.update
    kind: method
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/TCCLocalConnector/MenuBarApp.swift
symbols:
  - name: TCCLocalConnectorApp
    kind: type
    line_hint: top
  - name: MenuContent
    kind: type
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/TCCLocalConnector/LoginItem.swift
symbols:
  - name: LoginItem
    kind: type
    line_hint: top
  - name: LoginItem.isEnabled
    kind: property
    line_hint: middle
  - name: LoginItem.setEnabled
    kind: method
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/TCCLocalConnector/Notifier.swift
symbols:
  - name: Notifier
    kind: type
    line_hint: top
  - name: Notifier.post
    kind: method
    line_hint: middle
  - name: Notifier.recent
    kind: property
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/ConnectorCore/StatusIcon.swift
symbols:
  - name: StatusIcon
    kind: type
    line_hint: top
  - name: StatusIcon.describe
    kind: func
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
```

**Notes**:
- **`StatusIcon` の配置について**: symbol の役割上は MenuBarExtra の描画（`MenuBarApp.swift`）に近いが、SwiftPM の実行可能ターゲット（`TCCLocalConnector`）はテストターゲットから import できない。`swift test --filter StatusIconTests` で自動検証するという要求（P09-VG-01）を満たすには、`StatusIcon.describe` をライブラリターゲットである `ConnectorCore` に置く必要がある。`MenuBarApp.swift` 側は `import ConnectorCore` して `StatusIcon.describe` を呼び出すのみとする。（**Noticed but not fixing 該当なし** — これは技術的制約に基づく実装配置の決定であり、外部挙動には影響しない。）
- `macos/Sources/TCCLocalConnector/` は P08 が実装する `macos/Sources/ConnectorCore/` とディレクトリが完全に分離しており、`disjoint_guarantee: true` が成立する。
- `LaunchWatcher` は `MacPlanExecutor` を介して `terminate()` を要求する（直接 API を呼ばない）ことで、D-01/D-02 の禁止事項をコードパス上でも一箇所に集約する。

## Verification Gates（P09）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P09-VG-01 | green | test | agent | true | `swift test --package-path macos --filter StatusIconTests` | P09 task_delta | exit == 0（10状態組の写像が一意） | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-09 | TEST-GREEN,SCOPE-01,DONT-01 |
| P09-VG-02 | green | grep | agent | true | `rg -n 'forceTerminate\|runningApplications\(\)' macos/Sources` | P09 task_delta | ヒット == 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-09 | TEST-GREEN,SCOPE-01,DONT-01 |
| P09-VG-03 | green | visual | human | true | `bash scripts/dev-run.sh` → メニューバーをクリック → `screencapture -x /tmp/p09-menu.png` | P09 task_delta | 人が `/tmp/p09-menu.png` を開き、11項目がすべて表示されていることを確認して `docs/macos-verification-<date>.md` に記録。合格条件: 記録ファイルに11項目のチェックがすべて `[x]` | GoalEvidence | failure_class:visual, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-09 | TEST-GREEN,SCOPE-01,DONT-01 |
| P09-VG-04 | green | manual | human | true | VM-2（終了拒否）を実施 | P09 task_delta | 対象アプリが `grace_seconds` 経過後も `pgrep -f <app>` でヒット >= 1、かつメニューに `action_refused` が1件 | GoalEvidence | failure_class:manual, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-09 | TEST-GREEN,SCOPE-01,DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 09
- gate_ids: [P09-VG-01, P09-VG-02, P09-VG-03, P09-VG-04]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-09.appendix.md（実行時に Read しない）

## Implementation Notes
- **`grace_seconds` 経過後の `isTerminated == false` は必ず `refused` にする**。
  // Why: TCC 権限が必要で無言失敗した場合でも「終了したつもりで終了していない」状態を報告経路で検出できる（OQ-1 の吸収）。再試行はしない — 機械的な再試行の繰り返しは強制終了への圧力になりやすく D-01 の精神に反する。
- **通知はベストエフォート実装にし、失敗をメニュー降格で最初から吸収する**。
  // Why: ad-hoc 署名下で `UNUserNotificationCenter` が使えるか未検証だが（OQ-2）、使えなくても `Notifier.recent` と状態アイコンで情報は届く。plan 実行を止める理由にはならない。
- **`LaunchWatcher` の observer は明示的に解除する**。
  // Why: `NSWorkspace.shared.notificationCenter` の subscription を放置すると、`MacPlanExecutor` や `AppStore` への強参照がリークし続ける。cleanup を Frontend Constraints に明記する。
- **`SMAppService.mainApp` を選ぶ**。
  // Why: ①plist の自前管理と残骸が消える ②システム設定 > ログイン項目 から無効化できるので「完全終了時にログイン時起動を無効化する」が OS の UI で実現される ③KeepAlive を持たないので「終了してもすぐ再起動される」矛盾と再起動ループが構造的に起きない。
- **plan の重なりは中断せず完了させる**。
  // Why: 実行中の plan N を中断すると、途中まで実行したアクションの状態が不明瞭になる。plan N+1 は N の完了後に実行し、N の結果は `cycle_id=N` のまま `report_actions` する（バックエンド側が `ignored` として捨てる設計のため、フロント側で古い結果を握り潰す判断をしなくてよい）。

## Behavior Specification
System Type: reactive

### Reactive（`MacPlanExecutor` / `LaunchWatcher` の plan 実行状態機械）
| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-09-01 | idle | `app.start` action 受信 | 対象 bundle_id のアプリが未起動 | pending | `openApplication(at:configuration:)` を1回呼ぶ | testStartApp_LaunchesWhenNotRunning |
| BEH-09-02 | idle | `app.start` action 受信 | 対象 bundle_id のアプリが既に起動済み | idle（変化なし） | `openApplication` を呼ばず `skipped/already_satisfied` を返す | testStartApp_SkipsWhenAlreadyRunning |
| BEH-09-03 | idle | `app.start` action 受信 | `urlForApplication(withBundleIdentifier:)` が解決不能 | idle（変化なし） | `failed/app_not_found` を返す | testStartApp_FailedAppNotFound |
| BEH-09-04 | pending（`app.start` completion 待ち） | `backendRequestTimeoutSeconds`(30) 経過 | completion 未到達 | idle | `timeout` を返す | testStartApp_Timeout |
| BEH-09-05 | idle | `app.stop` action 受信 | 対象 bundle_id の `runningApplications` が0件 | idle（変化なし） | `skipped/already_satisfied` を返す | testStopApp_SkipsWhenNotRunning |
| BEH-09-06 | idle | `app.stop` action 受信 | `terminate()` 後、`grace_seconds` 内に `isTerminated == true` を `appStopPollIntervalMilliseconds`(250) 間隔のポーリングで確認 | idle | `stopped`（正常終了）を返す | testStopApp_GracefulTermination |
| BEH-09-07 | idle | `app.stop` action 受信 | `grace_seconds` 経過後も `isTerminated == false` | idle（変化なし。再試行しない） | **必ず** `refused/quit_refused` を返す | testStopApp_RefusedNoRetry |
| BEH-09-08 | idle | `notify` action 受信 | `UNUserNotificationCenter` への投稿が失敗/拒否 | idle | 例外にせず `Notifier.recent`（容量 `notifierRecentCapacity`=20）へ追加し plan 実行は継続する | testNotify_FailureDowngradedToRecent |
| BEH-09-09 | 実行中（plan N 処理中） | plan N+1 が届く | - | 実行中（plan N を継続） | plan N の実行を中断せず完了させてから N+1 を実行する。N の結果は `cycle_id=N` のまま報告する | testPlanOverlap_CompletesCurrentBeforeNext |
| BEH-09-10 | enforce_stop_bundle_ids に対象 bundle_id を含む | `didLaunchApplicationNotification` 受信 | 起動したアプリの bundle_id が集合に含まれる | idle | 60秒のポーリング周期を待たず即座に `terminate()` を要求する | testLaunchWatcher_ImmediateTerminateOnMatch |
| BEH-09-11 | idle | plan 受信（`plan.dry_run == true`） | - | idle | 副作用 API を一切呼ばず、`kind ∈ {app.start, app.stop, notify}` の全アクションを `skipped/already_satisfied` として `report_actions` する | testDryRun_NoSideEffects |

### Correctness Criteria（観測可能・固定する）
- `app.stop` は `grace_seconds` 経過後の `isTerminated` が `false` である限り、必ず `refused/quit_refused` を返す。自動再試行は一切行わない。
- `report_actions` の `results` 要素数は、受信した `plan.actions` のうち `kind ∈ {app.start, app.stop, notify}` の件数と厳密一致する。`action_id` は plan のものをそのまま返す。
- `dry_run == true` のとき、`NSWorkspace` / `NSRunningApplication` / `UNUserNotificationCenter` への呼び出しが1回も発生しない。
- `LaunchWatcher` による是正は、`menuRefreshIntervalSeconds` のポーリング周期を待たず、`didLaunchApplicationNotification` を受けて即座に発火する。
- `StatusIcon.describe` は 10 状態組すべてに対し一意なラベルを返す（同一ラベルの重複が0件）。

### Left to Implementation（内部ヘルパー名・小さな関数分割・ローカル変数名のみ）
- `MacPlanExecutor` 内部での completion ハンドラの管理方法（`withCheckedContinuation` かコールバック保持か）
- `LaunchWatcher` の observer トークンの保持方法（プロパティ名）
- `MenuContent` の SwiftUI View 分割粒度（サブビュー名）
- 「明日の開始時刻まで一時停止」の残り秒数計算の内部関数名

## Frontend Constraints（frontend_scope:true のため必須）
- **AbortController 相当**: `app.stop` の猶予待ちは `Task.sleep` + キャンセルで表現する。plan 切り替え時に前の待機を確実にキャンセルする。
- **cleanup 必須対象**: `NSWorkspace.shared.notificationCenter` の observer 明示解除、`Task` の解除、`NSAlert` の参照保持解除。
- **state リセット条件**: 新しい plan 受信時に `enforce_stop_bundle_ids` を差し替える。バックエンド断時は `enforce_stop_bundle_ids` を**空にする**（制御解除）。
- **エラー時の表示**: 直近の `StatusPayload` を維持し、`Notifier.recent` に警告を積む。既存表示をクリアしない。

## Visual Verification（visual_scope:true）
> **agent-browser は使用不可**（ネイティブ macOS アプリのため）。**XCUITest も使用不可**（`.xcodeproj` / test target を持たない決定のため）。
- **検証ツール**: `screencapture -x` による手動スクリーンショット撮影 + 人による目視確認
- **対象URL / 起動条件**: `bash scripts/dev-run.sh` で `.app` を起動し、メニューバーアイコンをクリック
- **基本フロー**: ①dev-run.sh で起動 ②メニューバーアイコンをクリック ③別ターミナルから `screencapture -x /tmp/p09-menu.png`（または `-T 5` で遅延撮影）④人が画像を開いて確認 ⑤`docs/screenshots/vm1-menu.png` へ保存
- **確認観点**: レイアウト/文言（11項目がこの順で表示されるか）/ 状態表示（現在のタスク名・状態・最終取得の相対時刻が読めるか）/ 一時停止中は残り時間が表示されるか
- **合否基準**（曖昧表現を禁止し観測可能な基準にする）: `docs/macos-verification-<date>.md` のチェックリストで**11項目すべてが `[x]`**。「現在のタスク: <名前>」「状態: <7状態のいずれか>」「最終取得: N秒前」の3行が画像内に読み取れること
- **スクリーンショット保存先**: `docs/screenshots/process-09-01.png`

## Red Phase
- [x] `PlanExecutorTests` に BEH-09-01〜09/11（`MacPlanExecutor` の3アクション）のテストケースを実装し、実装がないため失敗することを確認する
- [x] `AppStoreTests` に BEH-09-10（`LaunchWatcher.update` / 即時是正）のテストケースを実装する
- [x] `StatusIconTests` に10状態組すべての一意性アサーションを実装する
- [x] ✅ **Phase Complete**（GoalEvidence: n/a（P09 の red フェーズは専用ゲートを持たないため `swift test --package-path macos` の非0終了を Red の証跡とする）/ status / command_or_action: `swift test --package-path macos` / exit_code / expected: != 0 / observed / attempt）

## Green Phase
- [x] `MacPlanExecutor.swift`, `LaunchWatcher.swift`, `MenuBarApp.swift`, `LoginItem.swift`, `Notifier.swift`, `StatusIcon.swift` を実装し、Behavior Specification 表の全行に test_ref のテストが存在し PASS することを確認する
- [x] MenuBarExtra メニューが11項目（現在のタスク名 / 状態 / 最終取得 / 今すぐ再取得 / 設定を再読込 / 設定ファイルを開く / ログを開く / 15分間一時停止 / 1時間一時停止 / 明日の開始時刻まで一時停止 / 一時停止を解除 / 完全終了…）をこの順で描画することを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P09-VG-01 / status / command_or_action: `swift test --package-path macos --filter StatusIconTests` / exit_code / expected: 0（10状態組一意） / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P09-VG-02 / status / command_or_action: `rg -n 'forceTerminate\|runningApplications\(\)' macos/Sources` / exit_code / expected: ヒット0 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P09-VG-03 / status / command_or_action: `bash scripts/dev-run.sh` → メニュークリック → `screencapture -x /tmp/p09-menu.png` / exit_code / expected: `docs/macos-verification-<date>.md` に11項目全 `[x]` / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P09-VG-04 / status / command_or_action: VM-2 実施 / exit_code / expected: `pgrep` ヒット>=1 かつ `action_refused` 1件 / observed / attempt）

## Refactor Phase
- [x] D-01（`rg -n 'forceTerminate\(|SIGKILL|kill -9' macos/Sources/TCCLocalConnector`）が期待0件であることを確認する
- [x] D-02/D-05/D-07/D-12/D-14 の grep が期待ヒット数（いずれも0件）と一致することを確認する
- [x] `LaunchWatcher` の observer が deinit または明示的な解除メソッドで確実に unsubscribe されていることをコードレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（禁止事項 D-01/D-02/D-05/D-07/D-12/D-14 の grep を Refactor Phase の証跡として使う）/ status / command_or_action: 禁止事項節記載の各 grep コマンド / exit_code / expected: 期待ヒット数と一致 / observed / attempt）

## Manual Verification（最大3件）
1. **VM-1 メニューバー表示の視覚確認（executor: human）**: 操作=dev-run.sh で起動しメニューをクリック、`screencapture -x /tmp/vm1-menu.png` → 期待=11項目がこの順で表示される → データ状態の確認方法=`/tmp/vm1-menu.png` が存在し、人が開いて11項目を目視確認、結果を `docs/macos-verification-<date>.md` に `[x]` で記録、画像を `docs/screenshots/vm1-menu.png` へ保存
2. **VM-2 GUI アプリの終了拒否（executor: human）**: 操作=対象アプリに未保存の変更を作り、その bundle_id を `app.stop` に持つルールに一致するタスクを開始、最大60秒待つ → 期待=終了確認ダイアログが表示され、`grace_seconds` 経過後もアプリは終了しておらず、メニュー内「最近の警告」に `action_refused` が1件現れ、**強制終了されない** → データ状態の確認方法=`pgrep -f <アプリ実行ファイル名>` が**ヒット >= 1**、`rg -n 'quit_refused' ~/.local/state/tcc-local-connector/backend.log` がヒット >= 1、`report_actions` の当該 action_id の `status == "refused"` かつ `code == "quit_refused"` がログに記録されている
3. **VM-4 起動検出による即時是正（executor: human）**: 操作=`app.stop` 対象アプリが `enforce_stop_bundle_ids` に含まれる状態を作り、`status` で `active_rule_ids` を確認してから対象アプリを Finder/Spotlight から手動起動、経過時間を計測 → 期待=60秒のポーリング周期を待たず**3秒以内**に通常終了要求が飛ぶ（一瞬ウィンドウが見える可能性はある） → データ状態の確認方法=`pgrep -f <アプリ実行ファイル名>` が**ヒット 0**、かつ `status.cycle_id` が手動起動の前後で**増えていない**こと（定期サイクルではなくイベント駆動で処理された証拠）

## Dependencies
- Requires: P08
- Blocks: P17, P51
