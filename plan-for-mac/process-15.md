# Process 15: エンジンテスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-15.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/engine`（P06）は60秒周期のポーリング・状態機械・ルール評価を束ねるオーケストレータであり、猶予境界（`DefaultFailureGraceSeconds`）・single-flight（サイクル実行中の重複起動防止）・スリープ検知・`config_error` 時の完全無操作は、SC-03（取得失敗が猶予継続で released へ遷移し以後 plan.actions が空配列になる）と SC-09（1万サイクル後の goroutine/FD 増加0＝リーク無し）に直結する。この安全性を専用のテスト強化 Process で固定する。
- **目的**: `internal/engine/engine_test.go` に猶予境界・single-flight・cycle_id 不一致破棄・`config_error` 時の完全無操作・1万サイクル後のリーク検証を追加する。
- **変更範囲**: `internal/engine/engine_test.go` の拡張のみ。`internal/engine/engine.go` / `cycle.go` / `status.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `DefaultFailureGraceSeconds` | 180 | 秒 | `TestGraceBoundary` で `grace-1s` / `grace+1s` の境界値を作る基準 |
| `WakeReevaluateThresholdFactor` | 2 | 倍 | スリープ検知テストで tick 遅延の境界値を作る基準 |

- **禁止事項**:
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/engine --glob '!*_test.go'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/engine/engine_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/engine/engine.go` / `cycle.go` / `status.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P06）の全 `BEH-06-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **performance**: `TestTenThousandCycles` は `runtime.NumGoroutine()` とファイルディスクリプタ数の増分を出力に含めることで、観測可能な形でリーク無しを証明する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/engine` は取得（`internal/tcc2`）・状態機械（`internal/state`）・ルール評価（`internal/rules`）・台帳（`internal/ledger`）を60秒周期で束ねるオーケストレータであり、単一障害点になりやすい。本 Process は猶予境界の ±1秒の精度、サイクル実行中の `refresh_now` が `busy` になる single-flight 制御、`cycle_id` 不一致の `report_actions` が静かに無視されエラーにならないこと、`config_error` 時にポーリングもアクションも一切行われないこと、そして1万サイクルを回した後に goroutine/ファイルディスクリプタの増分が0であること（リーク無し）を固定する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/engine/engine_test.go` | 既存末尾に追記 | 猶予境界・single-flight・cycle_id 不一致・config_error 無操作・スリープ検知・parse_ok=false の保持・1万サイクルリーク検証のテスト |

## Symbol Targets
```yaml
file: internal/engine/engine_test.go
symbols:
  - name: TestGraceBoundary
    kind: func
    line_hint: bottom
  - name: TestRefreshNow_SingleFlight_Busy
    kind: func
    line_hint: bottom
  - name: TestReportActions_CycleIDMismatch_Ignored
    kind: func
    line_hint: bottom
  - name: TestConfigError_NoPollingNoActions
    kind: func
    line_hint: bottom
  - name: TestPause_RefreshNow_PausedError
    kind: func
    line_hint: bottom
  - name: TestResume_NotPaused_InvalidState
    kind: func
    line_hint: bottom
  - name: TestWakeDetection_TickDelay_Engine
    kind: func
    line_hint: bottom
  - name: TestParseFailure_RunningTasksRetainsPrevious
    kind: func
    line_hint: bottom
  - name: TestTenThousandCycles
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/engine/engine_test.go のみを変更する。W05 内の他 Process は internal/engine に触れない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/engine/engine.go:Engine"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P15-VG-01 | green | test | agent | true | `go test ./internal/engine -race -count=1 -cover` | P15 task_delta | exit==0 かつ coverage >= 80.0% | GoalEvidence（exit_code・coverage 数値・goroutine/FD 増分を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-04, SC-09 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 15
- gate_ids: [P15-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（`DefaultFailureGraceSeconds`, `WakeReevaluateThresholdFactor`）
- regeneration_required_when: P06 の `Engine` の公開 API・猶予境界の判定条件・Verification Gates のいずれかが変更されたとき
- appendix: process-15.appendix.md（実行時に Read しない）

## Implementation Notes
- **`TestGraceBoundary`**: 取得失敗が `DefaultFailureGraceSeconds - 1s` 継続した時点で `degraded`、`DefaultFailureGraceSeconds + 1s` 継続した時点で `released` が出力に含まれることを確認する（境界の ±1秒精度）。
- **`TestRefreshNow_SingleFlight_Busy`**: サイクル実行中に `refresh_now` を呼び出すと `busy` エラーになることを確認する（single-flight 制御。同時に2つのサイクルが走らないことの裏付け）。
- **`TestReportActions_CycleIDMismatch_Ignored`**: 古い `cycle_id` を持つ `report_actions` がエラーにならず `ignored` にカウントされることを確認する。
- **`TestConfigError_NoPollingNoActions`**: `config_error` 状態のときポーリングもアクションも plan 発行も一切行われないことを確認する（呼び出し回数0件のアサーションで裏付ける）。
- **`TestPause_RefreshNow_PausedError`**: `paused` 状態で `refresh_now` を呼ぶと `paused` エラーになることを確認する。
- **`TestResume_NotPaused_InvalidState`**: 現在 `paused` でないときに `resume` を呼ぶと `invalid_state` になることを確認する。
- **`TestWakeDetection_TickDelay_Engine`**: tick 遅延が `interval × WakeReevaluateThresholdFactor` を超えたとき、スリープ検知として扱われることを確認する（`internal/state` 側のロジック呼び出しがエンジンレベルで正しく駆動されることの統合的な確認）。
- **`TestParseFailure_RunningTasksRetainsPrevious`**: `parse_ok==false` のサイクルで `running_tasks` が前回値を保持することを確認する（SC-04 の直接的な裏付け）。
- **`TestTenThousandCycles`**: 1万サイクルを高速に回した後、`goroutine_delta == 0` かつ `fd_delta == 0` を出力に含めて確認する（SC-09 の直接的な裏付け）。
  // Why: 常駐アプリとして長時間動作し続けるため、わずかなリークでも蓄積して最終的にリソース枯渇を招く。1万サイクルという大きな回数で検証することで、通常のテストでは見えない緩やかなリークを検出する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- 取得失敗が `DefaultFailureGraceSeconds - 1s` の時点では `degraded`、`+1s` の時点では `released` になる。
- サイクル実行中の `refresh_now` 呼び出しは `busy` を返し、新しいサイクルを開始しない。
- 古い `cycle_id` を持つ `report_actions` はエラーにならず `ignored` としてカウントされる。
- `config_error` 状態ではポーリング・アクション実行・plan 発行がいずれも一切行われない。
- `paused` 状態での `refresh_now` は `paused` エラー、`paused` でない状態での `resume` は `invalid_state` エラーになる。
- `parse_ok==false` のサイクルでは `running_tasks` が前回値を保持し、空配列に置換されない。
- 1万サイクル実行後、goroutine 数とファイルディスクリプタ数の増分がいずれも0である。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- テスト内で `Engine` にモック取得元（tcc2 セッション相当）を注入する方法
- 1万サイクルを高速に回すためのポーリング間隔の短縮方法（テスト専用の内部フックか、時刻注入か）
- goroutine/FD カウント取得のヘルパ関数名

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P06 の green 状態であるため、テスト追加前に該当テスト名を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P15-VG-01）と共有する。
- [x] `internal/engine/engine_test.go` に9個のテスト関数（`TestTenThousandCycles` を含む）を追加する

## Green Phase
- [x] `go test ./internal/engine -race -count=1 -cover` を実行し、追加した全テストケースが PASS することを確認する
- [x] `TestTenThousandCycles` の出力に `goroutine_delta == 0` かつ `fd_delta == 0` が含まれることを確認する
- [x] Behavior Specification の Correctness Criteria 全項目に対応するテストが存在し PASS することを確認する
- [x] coverage が 80.0% 以上であることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P15-VG-01 / status / command_or_action: `go test ./internal/engine -race -count=1 -cover` / exit_code / expected: 0 かつ coverage>=80.0% かつ goroutine/FD 増分0 / observed / attempt）

## Refactor Phase
- [x] D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `internal/engine/engine.go` / `cycle.go` / `status.go` に差分が生じていないことを `git diff --stat` で確認する
- [x] 専用の Verification Gate はここには定義しない。D-09/D-12 の grep 結果は P15-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P06（`internal/engine` の実装が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
