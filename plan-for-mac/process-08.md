# Process 08: Swift パッケージ骨格・.app バンドル・BackendClient

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-08.md` を起動した際の自己完結ブリーフ。

- **背景**: バックエンド（Go）との通信規約（NDJSON・RPC 12 メソッド・イベント4種）は既に決定済みであり、Swift 側はこの契約の**消費側**として実装できる。Go 側の完成を待たずに独立して進行できるよう、本 Process では SwiftPM パッケージの骨格・`.app` バンドル生成・バックエンドプロセスのライフサイクル管理（ハンドシェイク・再起動・シャットダウン）を先に固める。
- **目的**: SwiftPM 構成（ConnectorCore + TCCLocalConnector + テストターゲット）、`Info.plist`（`LSUIElement`）、`make-app-bundle.sh`（backend 同梱・ad-hoc 署名）、NDJSON フレーマ、ready ハンドシェイク、指数バックオフ再起動、孤児回収を実装する。
- **変更範囲**: `macos/Package.swift`, `macos/Resources/Info.plist`, `macos/Sources/ConnectorCore/{Protocol,LineFramer,BackendClient,PlanExecutor,AppStore,Constants}.swift`, `macos/Tests/ConnectorCoreTests/{LineFramerTests,BackendClientTests}.swift`, `scripts/make-app-bundle.sh`, `scripts/dev-run.sh` の新規作成。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| supportedProtocolVersion | 1 | - | `ready.data.protocol_version` との一致判定。不一致は再起動せず `backendIncompatible` |
| requiredCapabilities | status, reload_config, pause, resume, refresh_now, config_paths, report_actions | set (7) | `ready.data.capabilities` に全て含まれるかの判定 |
| backendReadyTimeoutSeconds | 10 | 秒 | 起動後 ready を待つ上限 |
| backendRequestTimeoutSeconds | 30 | 秒 | 通常リクエストのタイムアウト |
| backendRestartMaxAttempts | 5 | 回 | 6回目は起動せず `backendDownPermanent` |
| backendRestartBaseDelaySeconds | 1 | 秒 | 待機列の初項 |
| backendRestartDelayFactor | 2 | 倍 | 待機列の公比（1,2,4,8,16） |
| backendRestartMaxDelaySeconds | 30 | 秒 | 待機列の上限（未到達だが定義上の天井） |
| backendRestartWindowSeconds | 600 | 秒 | 再起動試行回数をカウントする窓 |
| backendDownReleaseGraceSeconds | 180 | 秒 | 経過で直近 plan を破棄し OS 操作を停止（`DefaultFailureGraceSeconds` と同値） |
| backendShutdownStdinGraceSeconds | 5 | 秒 | stdin close 後、SIGTERM 送信までの猶予 |
| backendShutdownTermGraceSeconds | 3 | 秒 | SIGTERM 送信後、hardKill までの猶予 |
| frontendMaxLineBytes | 262144 | バイト | LineFramer の受信1行上限（256 KiB。送信上限64 KiBとは別値） |
| bundleIdentifier | "jp.takets.tcc-local-connector" | - | `Info.plist` の `CFBundleIdentifier` |
| backendResourceName | "tcc-local-connector-backend" | - | `Bundle.main.url(forResource:withExtension:)` の解決名 |

- **禁止事項**:
  - D-01（強制終了禁止）: `rg -n 'forceTerminate\(|SIGKILL|kill -9' macos/Sources` → 期待1件（`BackendClient.hardKill` のみ）
  - D-02（`runningApplications()` 全列挙禁止）: `rg -n 'runningApplications\(\)' macos/Sources` → 期待0件（本 Process では NSWorkspace を扱わないため構造的に0件）
  - D-05（Windows/WSL 向けコード禁止）: `rg -n 'wsl\.exe|GOOS=windows|go:build windows|NotifyIcon|PowerShell|wslpath' macos/Sources scripts/make-app-bundle.sh scripts/dev-run.sh` → 期待0件
  - D-07（バンドルID・絶対パス直書き禁止）: `rg -n 'com\.tinyspeck|com\.amazon\.Lassen|/opt/homebrew|/Users/takets' macos/Sources scripts` → 期待0件
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' macos/Sources scripts/make-app-bundle.sh scripts/dev-run.sh` → 期待0件
  - D-14（`.xcodeproj`/`xcodebuild`/XCUITest 禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' macos/` → 期待0件

- **適用される横断方針（インライン展開）**:
  - **security**: Automation / アクセシビリティ権限を要求しない設計制約。使ってよい API = `NSWorkspace.openApplication(at:configuration:)` / `NSRunningApplication.terminate()` / `NSWorkspace.shared.runningApplications`（プロパティ参照のみ。**引数なしの `runningApplications()` 全列挙は禁止**）/ `NSWorkspace.didLaunchApplicationNotification` / `Foundation.Process`。使ってはいけない = `NSAppleScript` / `osascript` / System Events / `AXUIElement` / `CGEvent` 合成 / 画面収録を伴う API。本 Process 自体は `Foundation.Process`（BackendClient のプロセス起動）のみを使う。
  - **error**: バックエンドからのエラーは表示するが機密情報を含めない。通知失敗を例外にせずメニュー内「最近の警告」へ降格する（本 Process では `AppStore` が保持する状態として設計するのみで、通知降格の実装自体は P09）。
  - **PATH の罠**: `.app` から起動されたプロセスの PATH は launchd 由来で `/opt/homebrew/bin` を含まない。`tcc2` などの外部実行ファイルは検証済み絶対パスを設定から渡す（`--tcc2-executable`）。本 Process では `.app` バンドル自体にバックエンド実行ファイルを同梱し `Bundle.main.url(forResource:withExtension:)` で解決するため、バックエンド自身の起動には PATH 探索が不要になる。
  - **stderr の drain**: フロントが `Process.standardError` を読まずに放置すると**バッファが埋まってバックエンドが書き込みでブロックし停止したように見える**。`BackendClient` は stdout（NDJSON 用）と stderr（診断ログ用）を両方とも継続的に drain する。
  - **該当 Don'ts**: 上記「禁止事項」を参照。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`macos/` に SwiftPM パッケージを新設する。`ConnectorCore` ライブラリターゲットが NDJSON プロトコル型（`Protocol.swift`）、改行分割器（`LineFramer`）、バックエンドプロセスのライフサイクル管理（`BackendClient`）、plan 実行の汎用ディスパッチ（`PlanExecutor`）、アプリ状態保持（`AppStore`）、ローカル定数（`Constants`）を持つ。`BackendClient` は actor として実装し、Swift 6 の strict concurrency でデータ競合をコンパイル時に検出させる。バックエンド起動後 `backendReadyTimeoutSeconds` 以内に `ready` イベントを受け取れなければ失敗とし、`protocol_version` 不一致または `requiredCapabilities` 欠落の場合は**再起動せず** `backendIncompatible` へ遷移する（無限再起動を避けるため）。それ以外の理由での断絶は 1,2,4,8,16 秒の指数バックオフで最大5回まで再起動を試み、6回目は起動しない。シャットダウンは stdin close → 猶予 → SIGTERM → 猶予 → `hardKill`（SIGKILL）の順で段階的に行う。`scripts/make-app-bundle.sh` は Go バックエンドのビルド成果物を `.app` に同梱し ad-hoc 署名する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `macos/Package.swift` | 新規全体 | SwiftPM パッケージ定義（`ConnectorCore` ライブラリ、`TCCLocalConnector` 実行可能ターゲット、`ConnectorCoreTests` テストターゲット） |
| `macos/Resources/Info.plist` | 新規全体 | `CFBundleIdentifier`, `LSUIElement=true`, `CFBundleExecutable`, `CFBundleName`, `CFBundleVersion` |
| `macos/Sources/ConnectorCore/Protocol.swift` | 新規全体 | `BackendRequest`, `BackendResponse`, `BackendEvent`, `RPCError`, `PlanPayload`, `PlanAction`, `StatusPayload`, `BackendState`, `NotifyPayload`, `StateChangedPayload` |
| `macos/Sources/ConnectorCore/LineFramer.swift` | 新規全体 | `LineFramer`, `LineFramer.push` |
| `macos/Sources/ConnectorCore/BackendClient.swift` | 新規全体 | `BackendClient`（actor）, `BackendClient.start`, `.send`, `.shutdown`, `.hardKill`, `BackendClientDelegate`, `RestartPolicy`, `BackendLifecycleState` |
| `macos/Sources/ConnectorCore/PlanExecutor.swift` | 新規全体 | `PlanExecutor`, `ActionOutcome`（macOS 固有アクション実装は P09 が担う汎用ディスパッチの型） |
| `macos/Sources/ConnectorCore/AppStore.swift` | 新規全体 | `AppStore`（状態スナップショット保持） |
| `macos/Sources/ConnectorCore/Constants.swift` | 新規全体 | 本 core のローカル定数15個 |
| `macos/Tests/ConnectorCoreTests/LineFramerTests.swift` | 新規全体 | 1バイトずつ／一括／分断／上限超過の全分岐テスト |
| `macos/Tests/ConnectorCoreTests/BackendClientTests.swift` | 新規全体 | ハンドシェイク成功・版不一致・capability欠落・再起動バックオフ・シャットダウン段階のテスト |
| `scripts/make-app-bundle.sh` | 新規全体 | `.app` バンドル生成（backend 同梱・ad-hoc 署名） |
| `scripts/dev-run.sh` | 新規全体 | build → bundle → `open dist/TCCLocalConnector.app` の開発用スクリプト |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: macos/Package.swift
symbols:
  - name: package-manifest
    kind: manifest
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
  - cmd_exists:xcrun
  - cmd_exists:plutil
---
file: macos/Resources/Info.plist
symbols:
  - name: LSUIElement
    kind: plist-key
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - cmd_exists:plutil
---
file: macos/Sources/ConnectorCore/Protocol.swift
symbols:
  - name: BackendRequest
    kind: type
    line_hint: top
  - name: BackendResponse
    kind: type
    line_hint: top
  - name: BackendEvent
    kind: type
    line_hint: top
  - name: RPCError
    kind: type
    line_hint: top
  - name: PlanPayload
    kind: type
    line_hint: middle
  - name: PlanAction
    kind: type
    line_hint: middle
  - name: StatusPayload
    kind: type
    line_hint: middle
  - name: BackendState
    kind: type
    line_hint: middle
  - name: NotifyPayload
    kind: type
    line_hint: bottom
  - name: StateChangedPayload
    kind: type
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/ConnectorCore/LineFramer.swift
symbols:
  - name: LineFramer
    kind: type
    line_hint: top
  - name: LineFramer.push
    kind: method
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/ConnectorCore/BackendClient.swift
symbols:
  - name: BackendClient
    kind: actor
    line_hint: top
  - name: BackendClient.start
    kind: method
    line_hint: middle
  - name: BackendClient.send
    kind: method
    line_hint: middle
  - name: BackendClient.shutdown
    kind: method
    line_hint: middle
  - name: BackendClient.hardKill
    kind: method
    line_hint: bottom
  - name: BackendClientDelegate
    kind: protocol
    line_hint: top
  - name: RestartPolicy
    kind: type
    line_hint: middle
  - name: BackendLifecycleState
    kind: type
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
  - cmd_exists:xcrun
---
file: macos/Sources/ConnectorCore/PlanExecutor.swift
symbols:
  - name: PlanExecutor
    kind: type
    line_hint: top
  - name: ActionOutcome
    kind: type
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/ConnectorCore/AppStore.swift
symbols:
  - name: AppStore
    kind: type
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: macos/Sources/ConnectorCore/Constants.swift
symbols:
  - name: supportedProtocolVersion
    kind: constant
    line_hint: top
  - name: requiredCapabilities
    kind: constant
    line_hint: top
  - name: backendReadyTimeoutSeconds
    kind: constant
    line_hint: top
  - name: backendRestartMaxAttempts
    kind: constant
    line_hint: middle
  - name: backendDownReleaseGraceSeconds
    kind: constant
    line_hint: middle
  - name: frontendMaxLineBytes
    kind: constant
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - swift_build_ok
---
file: scripts/make-app-bundle.sh
symbols:
  - name: make-app-bundle
    kind: script
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - cmd_exists:xcrun
  - cmd_exists:plutil
---
file: scripts/dev-run.sh
symbols:
  - name: dev-run
    kind: script
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
```

**Notes**:
- `macos/` と `scripts/` は既存 Go ツリー（`internal/`, `cmd/`）と完全にディレクトリ分離しており symbol / import 依存を一切持たない（`disjoint_guarantee: true`）。
- `BackendClient` を actor にする判断は Swift 6 strict concurrency 前提であり、`Sendable` でない値をまたいで共有すればコンパイルエラーになる。これにより競合状態をコンパイル時に検出する。
- Event/Response の識別規則は **`event` キーの有無**であり `id` の有無に依存しない点を `Protocol.swift` のデコード実装で厳守する（バックエンドの `writeError` は `id:""` を omitempty で落とすため）。

## Verification Gates（P08）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P08-VG-01 | red | test | agent | true | `swift test --package-path macos` | P08 task_delta | exit != 0 | GoalEvidence | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08 | TEST-RED,TEST-GREEN,SCOPE-01,DONT-01 |
| P08-VG-02 | green | test | agent | true | `swift test --package-path macos` | P08 task_delta | exit == 0 | GoalEvidence | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08 | TEST-RED,TEST-GREEN,SCOPE-01,DONT-01 |
| P08-VG-03 | green | test | agent | true | `swift build --package-path macos -c release` | P08 task_delta | exit == 0 かつ stderr に `warning:` が0件 | GoalEvidence | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08 | TEST-RED,TEST-GREEN,SCOPE-01,DONT-01 |
| P08-VG-04 | green | conformance | agent | true | `bash scripts/make-app-bundle.sh && plutil -extract LSUIElement raw dist/TCCLocalConnector.app/Contents/Info.plist` | P08 task_delta | exit == 0 かつ標準出力が `true` | GoalEvidence | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08 | TEST-RED,TEST-GREEN,SCOPE-01,DONT-01 |
| P08-VG-05 | green | conformance | agent | true | `test -x dist/TCCLocalConnector.app/Contents/Resources/tcc-local-connector-backend` | P08 task_delta | exit == 0 | GoalEvidence | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08 | TEST-RED,TEST-GREEN,SCOPE-01,DONT-01 |
| P08-VG-06 | green | test | agent | true | `swift test --package-path macos --filter BackendClientTests/testRestartBackoffSequence` | P08 task_delta | exit == 0（待機列 1,2,4,8,16 と6回目非起動を注入クロックで照合） | GoalEvidence | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08 | TEST-RED,TEST-GREEN,SCOPE-01,DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 08
- gate_ids: [P08-VG-01, P08-VG-02, P08-VG-03, P08-VG-04, P08-VG-05, P08-VG-06]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-08.appendix.md（実行時に Read しない）

## Implementation Notes
- **Go バックエンドを `.app` へ同梱する**: `scripts/make-app-bundle.sh` が `go build` の成果物を `Contents/Resources/` にコピーする。
  // Why: 版の組合せが構造的に固定され「通信規約の版不一致」がほぼ起きなくなる。PATH 探索も不要になる。
- **孤児回収は stdin EOF 検出に依存する**: シャットダウンの第一段階は stdin を閉じることであり、バックエンドは EOF で shutdown する（Go 側で実装済み）。
  // Why: macOS に Job Object も `prctl(PR_SET_PDEATHSIG)` も無い。バックエンド側が親の死を検出する手段が stdin EOF であり、これは既に実装済み。これが stdio 方式のもう一つの利点。
- **D-01 の適用範囲**: D-01（強制終了禁止）は「管理対象の外部プロセス」の話であって、自作のバックエンド自身には適用しない。
  // Why: `hardKill` が macOS/Sources 全体で唯一の SIGKILL 経路であることを grep で担保することで、この区別を実装の物理構造として固定する。
- **ハンドシェイク不一致時は再起動しない**: `protocol_version` 不一致または `requiredCapabilities` 欠落は `backendIncompatible` へ即座に遷移し、再起動をスケジュールしない。
  // Why: 版・能力の不一致は再起動しても解決しない。無限再起動を避けるため即座に諦めて表示のみにする。
- **`tcc2` 実行ファイルは絶対パスで受け取る**（本 Process 自体は `tcc2` を起動しないが、`BackendClient` が起動するバックエンドの引数構築規約として踏襲）。
  // Why: `.app` から起動されたプロセスの PATH は launchd 由来で `/opt/homebrew/bin` を含まない。
- **stderr を継続的に drain する**: `BackendClient` は stdout（NDJSON）と stderr（診断ログ）の両方を読み取り続ける。
  // Why: 読み取りを怠るとバッファが埋まってバックエンドが書き込みでブロックし、フリーズしたように見える。

## Behavior Specification
System Type: reactive

### Reactive（`BackendClient` のライフサイクル状態機械）
| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-08-01 | starting（プロセス起動直後） | `ready` イベント受信 | `protocol_version == supportedProtocolVersion` かつ `requiredCapabilities` が全て含まれる | running | plan 実行可能。再起動カウンタをリセット | testHandshakeSuccess |
| BEH-08-02 | starting | `backendReadyTimeoutSeconds`(10) 経過しても `ready` 未受信 | - | backendDown（再起動試行1回目を予約） | プロセスは終了処理へ | testHandshakeTimeout_TriggersRestart |
| BEH-08-03 | starting | `ready` イベント受信 | `protocol_version != supportedProtocolVersion` | backendIncompatible | 再起動をスケジュールしない。OS 操作を一切行わない | testProtocolVersionMismatch_NoRestart |
| BEH-08-04 | starting | `ready` イベント受信 | `requiredCapabilities` に1つでも欠落 | backendIncompatible | 同上。再起動カウンタをリセットしない | testMissingCapability_NoRestart |
| BEH-08-05 | backendDown（試行回数 N、N<5） | 再起動タイマ発火 | 待機列は `backendRestartBaseDelaySeconds`(1) から `backendRestartDelayFactor`(2) 倍で 1,2,4,8,16 秒 | starting（再起動試行） | 注入クロックで待機列を照合（実時間で待たない） | testRestartBackoffSequence |
| BEH-08-06 | backendDown（試行回数 5） | 再起動タイマ発火条件 | `backendRestartWindowSeconds`(600) 内で試行回数 >= `backendRestartMaxAttempts`(5) | backendDownPermanent | 6回目は起動しない | testRestartBackoffSequence（6回目非起動を含む） |
| BEH-08-07 | backendDownPermanent | `backendDownReleaseGraceSeconds`(180) 経過 | - | backendDownPermanent（変化なし。plan のみ破棄） | 直近 plan を破棄し、以後 OS 操作を行わない | testReleaseGraceDiscardsPlan |
| BEH-08-08 | running | `shutdown()` 呼び出し | - | terminated | ①stdin close ②`backendShutdownStdinGraceSeconds`(5) 待機 ③残存なら SIGTERM ④`backendShutdownTermGraceSeconds`(3) 待機 ⑤残存なら `hardKill` の順で段階的に進む | testShutdownEscalation |
| BEH-08-09 | 任意（受信バイトストリーム処理中） | `LineFramer.push` に1行が `frontendMaxLineBytes`(262144) 超過で到達 | - | 変化なし（エラー計上のみ） | 該当行は破棄され例外を投げずに次の行から復帰する | testLineFramer_OversizedLineDiscardedAndRecovers |

### Correctness Criteria（観測可能・固定する）
- `hardKill` の呼び出しは `macos/Sources` 全体で `BackendClient.shutdown` のエスカレーション経路（BEH-08-08 の⑤）以外から発生しない。
- `backendIncompatible` に到達した場合、再起動タイマは二度と発火しない（BEH-08-03/04 到達後、`RestartPolicy` の試行回数は増加しない）。
- 再起動の待機列は常に `1, 2, 4, 8, 16` 秒の順で、6回目の試行が `backendRestartWindowSeconds` 内で発生しない。
- `LineFramer` は1バイトずつ届く場合・一括で届く場合・行の途中で分断される場合のいずれでも同一の行集合を復元する。
- Event と Response の判別は `event` キーの有無のみで行い、`id` の有無や値には依存しない。

### Left to Implementation（内部ヘルパー名・小さな関数分割・ローカル変数名のみ）
- `RestartPolicy` 内部でのタイマー実装（`Task.sleep` を使うか `DispatchSourceTimer` を使うか）
- `BackendClient` actor 内部での state 変数名・private ヘルパーメソッドの分割粒度
- `LineFramer` の内部バッファ実装（`[UInt8]` か `Data` か）

## Frontend Constraints（frontend_scope:true のため必須）
- **AbortController 相当**: Swift では `Task` のキャンセルと `Process.terminate()` で表現する。URLSession は使わない（ネットワーク通信なし）。
- **cleanup 必須対象**: `Process.terminationHandler` の循環参照回避（`[weak self]`）、stdout/stderr 読み取り Task のキャンセル、`readabilityHandler` の nil 代入。
- **state リセット条件**: バックエンド再起動時に `cycle_id` / 直近 plan / `requiredCapabilities` 確認結果をリセットする。`backendIncompatible` 到達時は再起動カウンタをリセットしない。
- **エラー時の表示**: 既存の `StatusPayload` を**維持する**（クリアしない）。理由: バックエンド断中も「最後に分かっていた状態」を見せる方が利用者の判断材料になる。ただし `state` は `backendDown` 系に上書きして鮮度を明示する。
- **Swift 6 strict concurrency**: `BackendClient` は actor とし、`Sendable` 違反をコンパイラに検出させる。

## Red Phase
- [x] `LineFramerTests` に1バイトずつ届く場合／一括で届く場合／行の途中で分断される場合／`frontendMaxLineBytes` 超過の全分岐を実装し、実装がないため失敗することを確認する
- [x] `BackendClientTests` に `testHandshakeSuccess` / `testHandshakeTimeout_TriggersRestart` / `testProtocolVersionMismatch_NoRestart` / `testMissingCapability_NoRestart` / `testRestartBackoffSequence`（6回目非起動を含む）/ `testShutdownEscalation` を実装する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P08-VG-01 / status / command_or_action: `swift test --package-path macos` / exit_code / expected: != 0 / observed / attempt）

## Green Phase
- [x] `Protocol.swift`, `LineFramer.swift`, `BackendClient.swift`, `PlanExecutor.swift`, `AppStore.swift`, `Constants.swift` を実装し、Behavior Specification 表の全行に test_ref のテストが存在し PASS することを確認する
- [x] `scripts/make-app-bundle.sh`, `scripts/dev-run.sh` を実装する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P08-VG-02 / status / command_or_action: `swift test --package-path macos` / exit_code / expected: 0 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P08-VG-03 / status / command_or_action: `swift build --package-path macos -c release` / exit_code / expected: 0 かつ warning 0件 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P08-VG-04 / status / command_or_action: `bash scripts/make-app-bundle.sh && plutil -extract LSUIElement raw dist/TCCLocalConnector.app/Contents/Info.plist` / exit_code / expected: 0 かつ標準出力 true / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P08-VG-05 / status / command_or_action: `test -x dist/TCCLocalConnector.app/Contents/Resources/tcc-local-connector-backend` / exit_code / expected: 0 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P08-VG-06 / status / command_or_action: `swift test --package-path macos --filter BackendClientTests/testRestartBackoffSequence` / exit_code / expected: 0 / observed / attempt）

## Refactor Phase
- [x] D-01（`rg -n 'forceTerminate\(|SIGKILL|kill -9' macos/Sources`）が期待1件（`BackendClient.hardKill` のみ）であることを確認する
- [x] D-02/D-05/D-07/D-12/D-14 の grep が期待ヒット数（いずれも0件）と一致することを確認する
- [x] `hardKill` が `BackendClient.shutdown` のエスカレーション経路以外から呼ばれていないことをコードレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（禁止事項 D-01/D-02/D-05/D-07/D-12/D-14 の grep を Refactor Phase の証跡として使う）/ status / command_or_action: 禁止事項節記載の各 grep コマンド / exit_code / expected: 期待ヒット数と一致 / observed / attempt）

## Manual Verification（最大3件）
1. **孤児回収の確認（executor: human）**: 操作=`bash scripts/dev-run.sh` で `.app` を起動し、`ps -ef | grep tcc-local-connector-backend` でバックエンドの PID を確認したのち、メニューバーの「完全終了…」ではなく Activity Monitor から `TCCLocalConnector` を強制終了する → 期待=`backendShutdownStdinGraceSeconds` + `backendShutdownTermGraceSeconds` 以内にバックエンドプロセスも終了する → データ状態の確認方法=`ps -ef | grep tcc-local-connector-backend` が数秒後にヒット0になること
2. **再起動バックオフの実地確認（executor: human）**: 操作=`.app` バンドル内の `tcc-local-connector-backend` を一時的に実行不能な内容に置き換えてから `.app` を起動する → 期待=メニューバーの状態表示が最終的に `backendDownPermanent` 相当に到達する → データ状態の確認方法=診断ログ（stderr 経由）に再起動試行が5回記録され、6回目の試行ログが存在しないこと

## Dependencies
- Requires: なし
- Blocks: P09, P17
