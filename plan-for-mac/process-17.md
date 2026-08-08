# Process 17: Swift ユニットテスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-17.md` を起動した際の自己完結ブリーフ。

- **背景**: Swift 側（P08: `BackendClient`・LineFramer・再起動制御、P09: `MenuBarExtra`・アクション実行）は Go バックエンドとの通信断・起動失敗・非互換プロトコルを検知して安全側に倒す責務を持つ。特にバックオフ列（`backendRestartMaxAttempts`=5、待機列 1,2,4,8,16秒）と `requiredCapabilities` 不足時の非再起動判定は SC-07（バックエンド再起動が待機列1,2,4,8,16で最大5回、6回目は起動しない）・SC-08（version 不一致時に再起動せず OS 操作を一切行わない）に直結する。この安全性を専用のテスト強化 Process で固定する。
- **目的**: `macos/Tests/ConnectorCoreTests/*.swift` に LineFramer の分割耐性・ready タイムアウト・capabilities 不足時の非再起動・バックオフ列（注入クロック）・`StatusIcon` の一意性・dry_run の副作用ゼロ・`report_actions` の件数整合性を検証するテストを追加する。
- **変更範囲**: `macos/Tests/ConnectorCoreTests/` 配下の新規テストファイル群のみ。`macos/Sources/ConnectorCore/*` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `frontendMaxLineBytes` | 262144 | bytes | LineFramer の上限超過テストで境界値を作る基準 |
| `backendReadyTimeoutSeconds` | 10 | 秒 | ready タイムアウトテストの判定基準 |
| `requiredCapabilities` | status, reload_config, pause, resume, refresh_now, config_paths, report_actions | set (7) | capabilities 不足時の非再起動テストの判定基準 |
| `backendRestartMaxAttempts` / `backendRestartBaseDelaySeconds` / `backendRestartDelayFactor` / `backendRestartMaxDelaySeconds` | 5 / 1 / 2 / 30 | count / seconds | バックオフ列（1,2,4,8,16）と6回目非起動の判定基準 |

- **禁止事項**:
  - D-14（`.xcodeproj`/`xcodebuild`/XCUITest の導入禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' macos/Tests` → 期待ヒット数 0
  - D-15（未導入 linter の記載禁止）: `rg -n 'golangci-lint|swiftlint|swift-format' macos/Tests` → 期待ヒット数 0
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go`/Swift のテストターゲットは D-09 の検査対象外）: 該当なし（Swift テストファイルはテストターゲット扱いのため grep 対象外）
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' macos/Tests/ConnectorCoreTests` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`macos/Sources/ConnectorCore/*` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P08, P09）の全 `BEH-08-n`/`BEH-09-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **並行性**: Swift 6 の strict concurrency に準拠したテストを書く。実時間で待機する `sleep`/`Task.sleep` によるバックオフ検証は行わず、**注入クロック**を用いる。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
Swift 側は Go バックエンドとの stdio 通信のフレーミング（`LineFramer`）、起動待ち・再起動制御（`BackendClient`）、UI 表示（`StatusIcon`）、アクション実行（`MacPlanExecutor`）を担う。本 Process は、1バイトずつの分割受信でも行を正しく組み立てられること、行の途中での分断やサイズ上限超過に対する耐性、`protocol_version` 不一致や `requiredCapabilities` 不足時には**再起動せず** OS 操作も一切行わないこと、再起動のバックオフ列が実時間に依存せず注入クロックで検証できること、10状態すべてで一意なアイコンラベルが得られること、`dry_run` で副作用 API が一切呼ばれないことを固定する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `macos/Tests/ConnectorCoreTests/LineFramerTests.swift` | 新規 | 1バイト分割/一括/行途中分断/上限超過のテスト |
| `macos/Tests/ConnectorCoreTests/BackendReadyTests.swift` | 新規 | ready タイムアウト、`protocol_version` 不一致・`requiredCapabilities` 不足時の非再起動テスト |
| `macos/Tests/ConnectorCoreTests/RestartBackoffTests.swift` | 新規 | 注入クロックによるバックオフ列（1,2,4,8,16）と6回目非起動のテスト |
| `macos/Tests/ConnectorCoreTests/StatusIconTests.swift` | 新規 | 10状態組すべてで一意なラベルを返すことのテスト |
| `macos/Tests/ConnectorCoreTests/DryRunPlanExecutorTests.swift` | 新規 | dry_run で副作用APIを呼ばず全アクションを skipped/already_satisfied にするテスト、`report_actions` の件数整合性テスト |

## Symbol Targets
```yaml
file: macos/Tests/ConnectorCoreTests/LineFramerTests.swift
symbols:
  - name: testPushSingleByteAtATime
    kind: func
    line_hint: top
  - name: testPushBulkData
    kind: func
    line_hint: top
  - name: testLineSplitAcrossPushes
    kind: func
    line_hint: middle
  - name: testExceedsMaxLineBytes_DiscardedAsErrorAndRecovers
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "macos/Tests/ConnectorCoreTests/ 配下の新規ファイルのみを追加する。W05 内の他 Process は macos/Tests に触れない。"
pre_flight_checks:
  - swift_build_ok
---
file: macos/Tests/ConnectorCoreTests/BackendReadyTests.swift
symbols:
  - name: testReadyNotReceivedWithinTimeout_Fails
    kind: func
    line_hint: top
  - name: testProtocolVersionMismatch_NoRestart_Incompatible
    kind: func
    line_hint: middle
  - name: testMissingRequiredCapability_NoRestart_Incompatible
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - swift_build_ok
---
file: macos/Tests/ConnectorCoreTests/RestartBackoffTests.swift
symbols:
  - name: testRestartBackoffSequence
    kind: func
    line_hint: top
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - swift_build_ok
---
file: macos/Tests/ConnectorCoreTests/StatusIconTests.swift
symbols:
  - name: testDescribeReturnsUniqueLabelForAllTenStates
    kind: func
    line_hint: top
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - swift_build_ok
---
file: macos/Tests/ConnectorCoreTests/DryRunPlanExecutorTests.swift
symbols:
  - name: testDryRun_NoSideEffectAPICalls_AllSkippedOrAlreadySatisfied
    kind: func
    line_hint: top
  - name: testReportActionsCountMatchesPlanKindCount
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - swift_build_ok
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P17-VG-01 | green | test | agent | true | `swift test --package-path macos` | P17 task_delta | exit==0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07, SC-08, SC-09 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 17
- gate_ids: [P17-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（`frontendMaxLineBytes`, `backendReadyTimeoutSeconds`, `requiredCapabilities`, `backendRestartMaxAttempts` 系4定数）
- regeneration_required_when: P08/P09 の `LineFramer`/`BackendClient`/`StatusIcon`/`MacPlanExecutor` の公開 API・バックオフ定数・Verification Gates のいずれかが変更されたとき
- appendix: process-17.appendix.md（実行時に Read しない）

## Implementation Notes
- **LineFramer に1バイトずつ push**: 受信データを1バイトずつ与えても、改行区切りで正しく1行ずつ組み立てられることを確認する。
- **一括 push**: 複数行を含むデータを一括で与えても正しく分割されることを確認する。
- **行の途中で分断**: 1行の途中でデータが分断され、後続の push で続きが来るケースでも正しく組み立てられることを確認する。
- **`frontendMaxLineBytes` 超過**: 上限を超える行はエラーとして破棄され、以後の行の受信は継続する（`LineFramer` 全体が壊れない）ことを確認する。
- **ready タイムアウト**: `backendReadyTimeoutSeconds` 以内に `ready` が届かない場合、失敗扱いになることを確認する。
- **`protocol_version` 不一致**: 再起動せず `backendIncompatible` になることを確認する（OS 操作を伴う API が一切呼ばれないことも合わせて確認する）。
- **`requiredCapabilities` 不足**: 7つのうち1つでも欠けたら同様に `backendIncompatible` になり、再起動しないことを確認する。
- **`testRestartBackoffSequence`**: **注入クロック**を用いて、待機列が 1, 2, 4, 8, 16 秒の順であること、6回目の再起動は試行されないことを確認する。
  // Why: 実時間で `sleep` するテストは合計31秒以上かかり、CI 実行時間を圧迫するうえフレークしやすい。注入可能なクロックインターフェースを用いることで、瞬時かつ決定的にバックオフ列を検証する。
- **`StatusIcon.describe`**: 10状態組すべてで一意なラベルを返すことを確認する（重複ラベルがあると、ユーザーがメニューバーアイコンから状態を正しく読み取れない）。
- **dry_run**: `dry_run:true` のとき副作用を持つ API（`NSWorkspace` 起動・プロセス起動・シグナル送信等）が一切呼ばれず、全アクションが `skipped` または `already_satisfied` になることを確認する。
- **`report_actions` の件数整合性**: `report_actions` に含まれる要素数が、plan の該当 kind（対象アクション種別）の件数と厳密に一致することを確認する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- `LineFramer` は1バイトずつの分割受信・一括受信・行途中の分断のいずれでも正しく行を組み立てる。
- `frontendMaxLineBytes` を超える行はエラーとして破棄され、以後の受信は継続する。
- `backendReadyTimeoutSeconds` 以内に `ready` が届かない場合は失敗扱いになる。
- `protocol_version` 不一致または `requiredCapabilities` の1つ以上の欠落があるとき、再起動せず `backendIncompatible` になり、OS 操作を伴う API は一切呼ばれない。
- 再起動のバックオフ列は 1, 2, 4, 8, 16 秒の順であり、6回目の再起動は試行されない（注入クロックで決定的に検証する）。
- `StatusIcon.describe` は10状態組すべてで一意なラベルを返す。
- `dry_run:true` のとき副作用 API は一切呼ばれず、全アクションが `skipped`/`already_satisfied` になる。
- `report_actions` の要素数は plan の該当 kind の件数と厳密に一致する。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- 注入クロックのプロトコル名・モック実装名
- LineFramer テストでのバイト列の具体的な組み立て方
- dry_run テストでの副作用 API 呼び出し検出方法（プロトコル抽象化されたインターフェースのモック呼び出し回数カウント）

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P08/P09 の green 状態であるため、テスト追加前に該当テスト名を指定して実行しても該当テストが存在せずビルドが通らない（あるいはテストが存在しないためスキップされる）だけで、意図的な失敗の観測にはならない。Red Phase の完了は Green Phase の gate（P17-VG-01）と共有する。
- [x] `macos/Tests/ConnectorCoreTests/` に5個の新規テストファイルを作成する

## Green Phase
- [x] `swift test --package-path macos` を実行し、追加した全テストケースが PASS することを確認する
- [x] `testRestartBackoffSequence` が実時間で待機せず（注入クロックで）瞬時に完了することを確認する
- [x] Behavior Specification の Correctness Criteria 全項目に対応するテストが存在し PASS することを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P17-VG-01 / status / command_or_action: `swift test --package-path macos` / exit_code / expected: 0 / observed / attempt）

## Refactor Phase
- [x] D-14（`.xcodeproj`/`xcodebuild`/XCUITest 禁止）・D-15（未導入 linter 記載禁止）・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `macos/Sources/ConnectorCore/*` に差分が生じていないことを `git diff --stat` で確認する
- [x] 専用の Verification Gate はここには定義しない。上記の grep 結果は P17-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P08（`ConnectorCore` の `LineFramer`/`BackendClient` が green であること）, P09（`MacPlanExecutor`/`StatusIcon` が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
