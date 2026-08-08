# Process 12: 一時停止・スリープテスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-12.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/state`（P03）は7状態の状態機械と `pause.json` による期限付き一時停止の永続化を実装する。一時停止はスリープや強制終了を跨いで絶対時刻で判定する必要があり（D-10: 単調時計による判定禁止）、`until` の解釈（過去/未来/不正値/version不一致）と、tick 遅延によるスリープ復帰検知（`WakeReevaluateThresholdFactor`）は、実装を誤ると「一時停止したつもりが解除されている」「スリープ復帰に気づかず古い状態のまま動き続ける」といった静かな誤動作を招く。この安全性を専用のテスト強化 Process で固定する。
- **目的**: `internal/state/machine_test.go`, `internal/state/pause_test.go` に境界値・異常系・アトミック性のテストを追加する。
- **変更範囲**: `internal/state/machine_test.go`, `internal/state/pause_test.go` の拡張のみ。`internal/state/machine.go` / `pause.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `PauseStateVersion` | 1 | - | `TestLoadPause_VersionMismatch` で不一致値を投入する基準 |
| `WakeReevaluateThresholdFactor` | 2 | 倍 | `TestWakeDetection_TickDelay` で `interval × 2` 超過の境界値を作る基準 |
| `StateFileMode` | 0600 | ファイルモード | `SavePause` のアトミック書き込みテストで生成ファイルのモードを確認する |

- **禁止事項**:
  - D-10（単調時計による一時停止期限判定の禁止）: `rg -n 'time\.Since|monotonic|remainingSeconds|elapsedSeconds|time\.Tick' internal/state --glob '!*_test.go'` → 期待ヒット数 0（本 Process はテストファイルしか変更しないため非テストファイルへの新規ヒットは生じない）
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/state --glob '!*_test.go'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/state/machine_test.go internal/state/pause_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/state/machine.go` / `pause.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P03）の全 `BEH-03-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **error**: 不正 JSON・version 不一致は「ファイル破棄して一時停止していない扱い」という単一の安全側フォールバックに収束することをテストで固定する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/state` は絶対時刻ベースの一時停止判定（スリープを跨いでも正しく機能する）と、7状態の状態機械の遷移規則を管理する。本 Process はその境界条件——`until` が過去・未来・不正値・version不一致のケース、時計が巻き戻った場合の継続判定、tick 遅延によるスリープ復帰検知、`pause.json` のアトミック書き込みが書き込み中断に耐えること——を個別に固定し、加えて不正な状態遷移（例: `config_error` から `active` への直接遷移）が拒否されることを確認する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/state/machine_test.go` | 既存末尾に追記 | 状態遷移の全分岐テスト、不正遷移拒否テスト |
| `internal/state/pause_test.go` | 既存末尾に追記 | `until` の境界値・異常系テスト、アトミック書き込みテスト、スリープ検知テスト |

## Symbol Targets
```yaml
file: internal/state/machine_test.go
symbols:
  - name: TestMachine_AllValidTransitions
    kind: func
    line_hint: bottom
  - name: TestMachine_RejectsInvalidTransition
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/state/*_test.go のみを変更する。W05 内の他 Process は internal/state に触れない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/state/pause.go:LoadPause"
---
file: internal/state/pause_test.go
symbols:
  - name: TestLoadPause_UntilFuture
    kind: func
    line_hint: bottom
  - name: TestLoadPause_UntilPast
    kind: func
    line_hint: bottom
  - name: TestLoadPause_InvalidJSON
    kind: func
    line_hint: bottom
  - name: TestLoadPause_VersionMismatch
    kind: func
    line_hint: bottom
  - name: TestPause_ClockRewind_StaysPaused
    kind: func
    line_hint: bottom
  - name: TestWakeDetection_TickDelay
    kind: func
    line_hint: bottom
  - name: TestSavePause_AtomicWrite_NoTmpLeftover
    kind: func
    line_hint: bottom
  - name: TestSavePause_WriteFailure_NoMemoryStateChange
    kind: func
    line_hint: bottom
  - name: TestClearPause_RemovesFile
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/state/pause.go:LoadPause"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P12-VG-01 | green | test | agent | true | `go test ./internal/state -race -count=10` | P12 task_delta | exit==0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-05 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 12
- gate_ids: [P12-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（`PauseStateVersion`, `WakeReevaluateThresholdFactor`, `StateFileMode`）
- regeneration_required_when: P03 の状態機械の遷移規則・`Pause`/`LoadPause`/`SavePause` のシグネチャ・Verification Gates のいずれかが変更されたとき
- appendix: process-12.appendix.md（実行時に Read しない）

## Implementation Notes
- **`until` の境界値テスト**: 未来 → `Expired() == false` / 過去 → `Expired() == true`。境界の等号側（`until == now`）は「未来として扱う」か「過去として扱う」かを実装のデフォルト挙動としてそのまま検証し、テスト側で新たな仕様を追加しない。
- **`TestLoadPause_InvalidJSON`**: 壊れた JSON を読み込ませたとき、エラーを伝播させるのではなく「ファイルを破棄して一時停止していない扱い」にすることを確認する。
  // Why: 一時停止ファイルの破損によってアプリ全体が起動不能になるのは過剰な安全側であり、「一時停止していない」に倒すほうが全体としては安全（制御を継続できる）。
- **`TestLoadPause_VersionMismatch`**: `PauseStateVersion` と異なる `version` フィールドを持つファイルも同様に「一時停止していない扱い」になることを確認する。
- **`TestPause_ClockRewind_StaysPaused`**: システム時計が `until` より前の時刻に巻き戻った場合でも、一時停止状態が継続すること（誤って解除方向に倒れないこと）を確認する。
- **`TestWakeDetection_TickDelay`**: tick 間隔が `interval × WakeReevaluateThresholdFactor` を超えて遅延した場合、スリープ復帰として検知されることを確認する。
- **`TestSavePause_AtomicWrite_NoTmpLeftover`**: `SavePause` が tmp ファイル書き込み + rename によるアトミック書き込みを行い、正常終了後に tmp ファイルが残らないことを確認する。
- **`TestSavePause_WriteFailure_NoMemoryStateChange`**: 書き込み自体が失敗した場合（例: 書き込み不可なパス）、メモリ上の一時停止状態も変更されない（部分的な状態不整合が起きない）ことを確認する。
- **`TestClearPause_RemovesFile`**: `ClearPause` 呼び出し後に `pause.json` が削除されていることを確認する。
- **`TestMachine_AllValidTransitions`**: 7状態（`Starting`/`Fetching`/`Active`/`Degraded`/`Released`/`Paused`/`ConfigError`）の全遷移条件の妥当性を確認する。
- **`TestMachine_RejectsInvalidTransition`**: 不正な遷移（例: `config_error` → `active` の直接遷移）が拒否されることを確認する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- `until` が未来なら `Expired() == false`、過去なら `Expired() == true`。
- 不正 JSON または `version` 不一致のとき、`LoadPause` はファイルを破棄し「一時停止していない」扱いになる（エラーを上位に伝播させない）。
- システム時計が `until` より前に巻き戻っても、一時停止状態は継続する（解除方向に誤って倒れない）。
- tick 遅延が `interval × WakeReevaluateThresholdFactor` を超えたとき、スリープ復帰として検知される。
- `SavePause` は tmp + rename によるアトミック書き込みを行い、正常終了後に tmp ファイルが残らない。書き込み失敗時はメモリ上の状態も変更されない。
- 不正な状態遷移（例: `config_error` → `active`）は拒否される。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- テスト内で時刻を注入する方法（固定 `time.Time` の直接指定か、テスト用クロックインターフェースか）
- 書き込み失敗を再現する具体的な手段（読み取り専用ディレクトリ、権限操作など）
- テーブル駆動テストの遷移表の型名

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P03 の green 状態であるため、テスト追加前に該当テスト名を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P12-VG-01）と共有する。
- [x] `internal/state/pause_test.go` に9個のテスト関数を追加する
- [x] `internal/state/machine_test.go` に2個のテスト関数（全遷移網羅・不正遷移拒否）を追加する

## Green Phase
- [x] `go test ./internal/state -race -count=10` を実行し、追加した全テストケースが10回連続で PASS することを確認する（`-count=10` は時刻境界のフレーク検出を兼ねる）
- [x] Behavior Specification の Correctness Criteria 全項目に対応するテストが存在し PASS することを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P12-VG-01 / status / command_or_action: `go test ./internal/state -race -count=10` / exit_code / expected: 0 / observed / attempt）

## Refactor Phase
- [x] D-10（単調時計による判定禁止）・D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `internal/state/machine.go` / `pause.go` に差分が生じていないことを `git diff --stat` で確認する
- [x] 専用の Verification Gate はここには定義しない。D-10/D-09/D-12 の grep 結果は P12-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。実機でのスリープ跨ぎ確認は VM-3（P09 の Manual Verification）が担う。

## Dependencies
- Requires: P03（`internal/state` の実装が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
