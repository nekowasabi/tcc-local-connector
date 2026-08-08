# Process 51: dry_run の全経路貫通

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-51.md` を起動した際の自己完結ブリーフ。

- **背景**: `safety.dry_run` は本番投入前の最後の関門であり、利用者が「このルールセットで実際に何が起きるか」を副作用なしで確認する唯一の手段である。しかし P06（バックエンド側アクション実行）と P09（Swift 側アクション実行）はそれぞれ独立に `command.run`/`process.*`/`app.start`/`app.stop` を実行する経路を持っており、`dry_run` フラグが片方の経路にしか伝播しない、あるいは伝播していても片方が実プロセス生成・シグナル送信・`NSWorkspace` 呼び出しを行ってしまう抜け穴が構造的に生まれ得る。
- **目的**: `plan.dry_run` を Go 側（`Engine.executeBackendActions`）と Swift 側（`MacPlanExecutor`）の両方の実行直前フックへ貫通させ、`dry_run:true` のときは `exec`/`os/signal`/`NSWorkspace`/`NSRunningApplication` のいずれも呼ばれないことを保証する。`notify` は dry_run でも通常通り発行し、状態機械の遷移（`on_enter`/`on_exit`）も抑制しない。
- **変更範囲**: `internal/engine/cycle.go`（`Engine.executeBackendActions` への dry_run 分岐追加）、`internal/ledger/manage.go`（`Manager.Start`/`Manager.Stop` への dry_run 分岐追加）、`macos/Sources/TCCLocalConnector/MacPlanExecutor.swift`（dry_run 時に副作用 API を呼ばず `skipped`/`already_satisfied` を返す分岐追加）。対応するテストファイル一式への追記。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | `DefaultActionTimeoutSeconds` | 30 | seconds | `command.run` の既定タイムアウト（dry_run では未使用だが分岐の前後で参照される既存値） |
  | `CommandRunMaxOutputBytes` | 65536 | bytes | `command.run` の stdout/stderr 上限（dry_run では出力そのものが発生しないため 0 バイトで記録） |
  | `DefaultProcessStopGraceSeconds` | 10 | seconds | `process.stop` の通常終了猶予（dry_run では未使用） |
  | `DefaultAppStopGraceSeconds` | 10 | seconds | `app.stop` の通常終了猶予（Swift 側。dry_run では未使用） |

- **禁止事項**: 該当する Don'ts のみ抜粋（本 Process のスコープに限定）。
  - D-01 強制終了の禁止 — `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal/engine/cycle.go internal/ledger/manage.go macos/Sources/TCCLocalConnector/MacPlanExecutor.swift` → **期待 0 件**
  - D-03 シェル文字列連結の禁止 — `rg -n '"/bin/sh"|"-c"|bash -c|zsh -c|sh -c' internal/engine/cycle.go internal/ledger/manage.go` → **期待 0 件**（本 Process のスコープに `allow_shell` ガードの実体はなく P06 側に既存）
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/engine/cycle.go internal/ledger/manage.go` → **期待 0 件**（`*_test.go` は対象外）
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/engine/cycle.go internal/ledger/manage.go macos/Sources/TCCLocalConnector/MacPlanExecutor.swift` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: dry_run は安全側の既定を確認する機能であるため、dry_run 判定の抜け漏れは D-01/D-02 の禁止事項と同等の重大度で扱う。
  - **error**: dry_run による skip は失敗ではないため、`ActionResult` のステータスは `skipped`（Go 側）/`already_satisfied`（Swift 側、既存の「既に満たされている」ステータスを流用）とし、新規エラーコードを追加しない。
  - **互換性**: `internal/protocol` の RPC スキーマは変更しない。`plan.dry_run` フィールド自体は P05（ルール評価と plan 生成）が既に定義済みであり、本 Process はその値を実行直前フックへ配線するのみ。
  - **並行実行制約**: 本 Process は `internal/engine/cycle.go` を P50・P52 と共有するため、**W07 内で P50→P51→P52 の直列実行が必須**（Conflict Matrix 参照）。P50 の Logger 注入が完了した後に着手する。
  - **該当 Don'ts**: D-01/D-03/D-09/D-12（上記）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

`plan.dry_run` は P05 が生成する `Plan` 構造体に既に存在するフィールドであり、`safety.dry_run:true` の設定から機械的に伝播する。本 Process はこの値を「実行直前」の2箇所のフックへ接続する。

Go 側では `Engine.executeBackendActions` がバックエンド側アクション（`command.run`/`process.start`/`process.stop`）を実行する直前に `plan.dry_run` を判定し、true であれば `exec.Command`/`os.Process.Signal` を一切呼ばず、`ActionResult` を `skipped` として記録する。`process.start`/`process.stop` の実体である `internal/ledger.Manager.Start`/`Manager.Stop` も同様に dry_run 判定を持ち、台帳への書き込み（`Entry` の追加・削除）自体は行わない（**dry_run は「何も実世界に影響を与えない」ことが目的であり、台帳という内部状態の変更も実世界への影響の一種とみなす**）。

Swift 側では `MacPlanExecutor` が `app.start`/`app.stop` を実行する直前に同じ判定を行い、`NSWorkspace.shared.openApplication`/`NSRunningApplication.terminate()` などの副作用 API を一切呼ばず、`report_actions` へは `skipped`/`already_satisfied` を返す。

**`notify` は dry_run でも通常通り発行する**。dry_run の目的は「何が起きる予定かを利用者に見せる」ことであり、通知はその手段そのものだからである。

**状態機械（`internal/state.Machine`）の遷移は dry_run の影響を受けない**。`on_enter`/`on_exit` は通常通り1回だけ実行され、`active_rule_ids` の更新も通常通り行われる。dry_run が止めるのはあくまで「実世界への副作用を伴うアクションの実行」であり、内部の状態遷移ロジックそのものを止めると、dry_run モードで段階投入の検証（ルール切替の正しさ）ができなくなってしまう。

```
// Why: dry_run で状態遷移まで止める設計も検討したが、それでは「新しいルールセットに
// 切り替えたときに on_enter/on_exit が意図通り1回だけ発火するか」を dry_run のまま
// 検証できなくなる。dry_run は「本番前の最後の関門」であって恒久的なデバッグモードでは
// ないため、遷移は通常通り走らせ、実世界への副作用のみを止める設計にした。
```

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/engine/cycle.go` | 既存ファイルへの追記（`patch_only: true`） | `Engine.executeBackendActions` 冒頭に `plan.dry_run` 判定を追加し、true のとき `exec.Command` を呼ばず `skipped` を記録する分岐を追加 |
| `internal/ledger/manage.go` | 既存ファイルへの追記（`patch_only: true`） | `Manager.Start`/`Manager.Stop` に `dryRun bool` 引数（または同等のフィールド）を追加し、true のとき実プロセス生成・シグナル送信・台帳書き込みを行わない分岐を追加 |
| `macos/Sources/TCCLocalConnector/MacPlanExecutor.swift` | 既存ファイルへの追記（`patch_only: true`） | `MacPlanExecutor` のアクション実行直前に `plan.dry_run` 判定を追加し、true のとき副作用 API を呼ばず `skipped`/`already_satisfied` を返す分岐を追加 |
| `internal/engine/engine_test.go` | 追記 | `TestDryRun_*` 一式 |
| `macos/Tests/ConnectorCoreTests/PlanExecutorTests.swift` | 追記 | `testDryRun_*` 一式 |

## Symbol Targets

```yaml
file: internal/engine/cycle.go
symbols:
  - {name: Engine.executeBackendActions, kind: method, body_start_line: 1, body_end_line: 55, line_hint: 65}
patch_only: true
disjoint_guarantee: false
disjoint_guarantee_evidence: "internal/engine/cycle.go は P50（Logger 注入）・P52（安全ゲート再確認）と同一ファイルを触る。Conflict Matrix（PLAN-for-mac.md）が本 Process を disjoint:false と記録。W07 は P50→P51→P52 の直列実行が必須であり、本 Process は P50 完了後に着手する。"
pre_flight_checks: [git_clean, go_test_ok]
---
file: internal/ledger/manage.go
symbols:
  - {name: Manager.Start, kind: method, body_start_line: 1, body_end_line: 30, line_hint: 1}
  - {name: Manager.Stop, kind: method, body_start_line: 1, body_end_line: 35, line_hint: 35}
patch_only: true
disjoint_guarantee: false
pre_flight_checks: [git_clean, go_test_ok]
---
file: macos/Sources/TCCLocalConnector/MacPlanExecutor.swift
symbols:
  - {name: MacPlanExecutor, kind: struct, body_start_line: 1, body_end_line: 60, line_hint: 1}
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "macos/Sources/TCCLocalConnector/ は Go 側の Conflict Matrix 行（P50/P52）と交差しない。同一 Wave 内で本ファイルを触る他 Process はない。"
pre_flight_checks: [git_clean, swift_build_ok]
```

### Notes
- Go 側2ファイルへの変更は `patch_only: true` に限定し、`Manager.Start`/`Manager.Stop` の既存シグネチャを破壊的に変えない（`dryRun` は追加引数またはレシーバ側の設定値として渡す。呼び出し元の `Engine.executeBackendActions` からの配線のみが本 Process のスコープ）。
- `MacPlanExecutor` は Go 側と disjoint（ディレクトリが分離しており Conflict Matrix 上も他 Process と衝突しない）だが、Go 側2ファイルとは論理的に同じ契約（`plan.dry_run` の意味）を実装するため、テストの期待値（`skipped`/`already_satisfied` の使い分け）を Go 側と揃える。

## Verification Gates（P51）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P51-VG-01 | red | test | agent | true | `go test ./internal/engine -run TestDryRun -v` | P51 の task_delta | exit != 0 | GoalEvidence（exit_code と `FAIL`/未定義シンボルを含む行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-09 | TEST-RED, SCOPE-01, QUALITY-01 |
| P51-VG-02 | green | test | agent | true | `go test ./internal/engine -run TestDryRun -v && swift test --package-path macos --filter PlanExecutorTests/testDryRun` | P51 の task_delta | 両方 exit == 0 | GoalEvidence（両コマンドの exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-09 | TEST-GREEN, SCOPE-01, QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 51
- gate_ids: [P51-VG-01, P51-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-51.appendix.md（実行時に Read しない）

## Implementation Notes

**注意**: 本 Process の差分が `MIN_PARALLEL_DIFF_LINES`(30行) 未満になる場合は P52 へ統合してよい（PLAN-for-mac.md の計画運用定数に基づく）。統合した場合はその旨を報告すること。

**なぜ `notify` を dry_run の対象から除外したか**:
```
// Why: dry_run の目的は「本番前に何が起きるか確認する」ことであり、notify はまさに
// その確認手段そのものである。notify まで抑制すると、利用者は dry_run で何も
// 見えなくなり、確認のしようがなくなる。副作用（実プロセス操作）と情報提示
// （notify）を明確に区別し、後者は常に発行する設計にした。
```

**なぜ台帳（`internal/ledger`）への書き込みも dry_run の対象に含めたか**: `process.start`/`process.stop` の実世界への影響は「プロセスを起動/終了させる」ことだが、台帳への `Entry` 追加・削除も「次回以降のサイクルでの整合判断に使われる永続状態の変更」であり、dry_run が「何も実世界に影響を与えない」ことを謳う以上、この永続状態変更も止める対象に含めるべきと判断した。台帳だけ実際に更新されると、dry_run 実行後に台帳の内容が実際のプロセス起動状態と食い違い、次回の非 dry_run サイクルで誤ったシグナル送信判断（P04 の PID 再利用対策）を招く危険がある。

## Behavior Specification
System Type: transformation

`(Plan.dry_run, Action) → (ActionResult, 実世界への副作用の有無)` の判定。

| behavior_id | 入力 | 出力 | pre_state | post_state | invariants | test_ref |
|---|---|---|---|---|---|---|
| BEH-51-01 | `plan.dry_run==true`、`kind=="command.run"` のアクション | `ActionResult{status:"skipped"}` | - | `exec.Command` が呼ばれていない | dry_run 時は実プロセス生成システムコールが発生しない | TestDryRun_CommandRun_NoExec |
| BEH-51-02 | `plan.dry_run==true`、`kind=="process.start"` のアクション | `ActionResult{status:"skipped"}` | 台帳に対象 `Entry` なし | 台帳に対象 `Entry` が追加されない | dry_run 時は `Manager.Start` が `exec.Command`/台帳書き込みを行わない | TestDryRun_ProcessStart_NoExecNoLedgerWrite |
| BEH-51-03 | `plan.dry_run==true`、`kind=="process.stop"` のアクション | `ActionResult{status:"skipped"}` | 台帳に対象 `Entry` あり | 台帳の `Entry` が削除されない、シグナルが送信されない | dry_run 時は `Manager.Stop` がシグナル送信・台帳削除を行わない | TestDryRun_ProcessStop_NoSignalNoLedgerDelete |
| BEH-51-04 | `plan.dry_run==true`、`kind=="notify"` のアクション | `ActionResult{status:"sent"}`（通常経路と同一） | - | 通知が実際に発行される | dry_run でも `notify` は抑制されない | TestDryRun_NotifyStillEmitted |
| BEH-51-05 | `safety.dry_run:true` を含む設定から生成された `Plan` | `Plan.dry_run==true` | - | - | 設定値がそのまま `Plan.dry_run` へ伝播する | TestDryRun_PropagatesFromConfigToPlan |
| BEH-51-06 | `plan.dry_run==true`、`kind=="app.start"`/`"app.stop"` のアクション（Swift 側） | `report_actions` の結果が `skipped`/`already_satisfied` | - | `NSWorkspace`/`NSRunningApplication` の副作用 API が呼ばれていない | Swift 側も dry_run で副作用 API を一切呼ばない | testDryRun_NoSideEffectAPI |
| BEH-51-07 | `plan.dry_run==true` を含む連続する複数サイクルでのルール切替 | 各サイクルの `on_enter`/`on_exit` が通常通り1回ずつ発火 | - | `active_rule_ids` が通常通り更新される | dry_run は状態機械の遷移を抑制しない | TestDryRun_StateMachineStillTransitions |

### Correctness Criteria（観測可能・固定する）
- `plan.dry_run==true` のとき、`command.run`/`process.start`/`process.stop` のいずれのアクションでも `exec.Command`/`os.Process.Signal` が呼ばれない（Go 側3テストが個別に照合）。
- `plan.dry_run==true` のとき、`process.start`/`process.stop` は台帳（`internal/ledger`）への書き込みを行わない。
- `plan.dry_run==true` のとき、`notify` は通常経路と同一に発行される。
- `plan.dry_run==true` のとき、Swift 側は `NSWorkspace`/`NSRunningApplication` の副作用 API を一切呼ばない。
- `plan.dry_run==true` であっても状態機械の `on_enter`/`on_exit` は抑制されず通常通り1回ずつ発火する。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `Manager.Start`/`Manager.Stop` へ `dryRun` を渡す方法（追加引数かオプション構造体か）。
- `MacPlanExecutor` 内部での dry_run 判定をアクション種別ごとの switch 文の先頭に置くか、共通ヘルパ関数に切り出すかの分割粒度。
- テストダブル（フェイク `exec.Command`）の命名。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（本 Process では上記3点のみが実装者の裁量）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/engine/engine_test.go` に `TestDryRun_CommandRun_NoExec` 等（存在しない dry_run 分岐を前提とする最小ケース）を追記
- [x] `macos/Tests/ConnectorCoreTests/PlanExecutorTests.swift` に `testDryRun_NoSideEffectAPI` を追記
- [x] Behavior Specification 表の全7行に対応する test_ref を用意
- [x] テストを実行して失敗することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P51-VG-01 / status: / command_or_action: `go test ./internal/engine -run TestDryRun -v` / exit_code: / expected: exit!=0 / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/engine/cycle.go` の `Engine.executeBackendActions` に `plan.dry_run` 判定を追加
- [x] `internal/ledger/manage.go` の `Manager.Start`/`Manager.Stop` に dry_run 分岐を追加
- [x] `macos/Sources/TCCLocalConnector/MacPlanExecutor.swift` に dry_run 分岐を追加
- [x] Behavior Specification 表の全7行に対応する test_ref のテストが存在し PASS することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P51-VG-02 / status: / command_or_action: `go test ./internal/engine -run TestDryRun -v && swift test --package-path macos --filter PlanExecutorTests/testDryRun` / exit_code: / expected: 両方 exit==0 / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `Engine.executeBackendActions` 内の dry_run 分岐とアクション種別分岐の重複を整理
- [x] D-01/D-03/D-09/D-12 grep を再実行しゼロヒットを確認
- [x] Go 側と Swift 側で dry_run 時のステータス文字列（`skipped`/`already_satisfied`）の使い分けに矛盾がないことを再確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P51-VG-02 / status: / command_or_action: `go test ./internal/engine -run TestDryRun -v && swift test --package-path macos --filter PlanExecutorTests/testDryRun` / exit_code: / expected: 両方 exit==0（Refactor 後も維持） / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: agent** — 操作: `safety.dry_run:true` の設定で1サイクル実行し `report_actions` の内容を確認する → 期待される出力: 全アクションが `skipped`/`already_satisfied` であり `notify` のみ `sent` になる → 確認方法: `TestDryRun_*` のテスト出力で `ActionResult.status` の一覧を確認
2. **executor: agent** — 操作: `safety.dry_run:true` のままルールセットを切り替えて5サイクル実行する → 期待される出力: `on_enter`/`on_exit` が切替時に各1回だけ発火し、以後の追加発火が0件 → 確認方法: `TestDryRun_StateMachineStillTransitions` のテスト出力でカウントを確認

## Dependencies
- Requires: P06, P09
- Blocks: P52
