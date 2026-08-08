# Process 14: ルール評価テスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-14.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/rules`（P05）はルールの `match`（contains OR / not_contains AND-NOT）評価と、優先度に基づく plan 生成（`BuildPlan`）を実装する。同一優先度のルールが矛盾する指示を出した場合の競合解決、`on_enter`/`on_exit` の一回性、`ensure` の冪等性は SC-02（ルール切替時に `on_enter`/`on_exit` が各1回だけ実行され、連続5サイクルで追加発火0件）に直結する。この安全性を専用のテスト強化 Process で固定する。
- **目的**: `internal/rules/evaluate_test.go`, `internal/rules/plan_test.go` に優先度・競合・冪等性・実行中0件の意味論を網羅するテストを追加する。
- **変更範囲**: `internal/rules/evaluate_test.go`, `internal/rules/plan_test.go` の拡張のみ。`internal/rules/evaluate.go` / `plan.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:
  本 Process はテスト専用であり定数を直接参照しない。優先度値・ランダムケース件数（100）はテストデータとして直接記述し、D-09 の検査対象外（`*_test.go`）として扱う。

- **禁止事項**:
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/rules --glob '!*_test.go'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/rules/evaluate_test.go internal/rules/plan_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/rules/evaluate.go` / `plan.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P05）の全 `BEH-05-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **スコープ制約**: `internal/rules` は `process.start`/`process.stop`/`command.run` を plan に含めない設計（PLAN-for-mac.md Scope 対象外「外部プロセスの一般的終了・強制終了」参照）。本 Process のテストもこの制約が守られていることを確認する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/rules` はタスク名に対する `contains`/`not_contains` 条件の評価と、優先度に基づく plan 生成を担う。本 Process は、異なる優先度での矛盾する指示は高い方が採用されること、同一優先度での矛盾はその対象だけ操作せず `Conflict` を返すこと、`on_enter` がルール遷移サイクルでのみ1回だけ発火すること、`ensure` が同じ入力に対して常に同じ plan を返す冪等性（property test 100ケース）を固定する。実行中タスクが0件のときの `contains`/`not_contains` の非対称性（`contains` は偽・`not_contains` は真）も明示的に仕様として固定する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/rules/evaluate_test.go` | 既存末尾に追記 | `match` の OR/AND-NOT 評価、実行中0件の意味論、大文字小文字の非区別のテスト |
| `internal/rules/plan_test.go` | 既存末尾に追記 | 優先度・競合解決・`on_enter`/`on_exit` 一回性・`ensure` 冪等性（property test）のテスト |

## Symbol Targets
```yaml
file: internal/rules/evaluate_test.go
symbols:
  - name: TestEvaluate_ContainsOR
    kind: func
    line_hint: bottom
  - name: TestEvaluate_NotContainsAndNot
    kind: func
    line_hint: bottom
  - name: TestEvaluate_NoRunningTasks_Asymmetry
    kind: func
    line_hint: bottom
  - name: TestEvaluate_EmptyMatchAlwaysTrue
    kind: func
    line_hint: bottom
  - name: TestEvaluate_CaseInsensitive
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/rules/*_test.go のみを変更する。W05 内の他 Process は internal/rules に触れない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/rules/plan.go:BuildPlan"
---
file: internal/rules/plan_test.go
symbols:
  - name: TestBuildPlan_Priority
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_SamePriorityConflict
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_EnterOnce
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_ExitOnRuleRemovedAfterReload
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_EnsureIdempotent
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_Property
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_EnforceStopBundleIDsMatchActions
    kind: func
    line_hint: bottom
  - name: TestBuildPlan_NoProcessOrCommandActions
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/rules/plan.go:BuildPlan"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P14-VG-01 | green | test | agent | true | `go test ./internal/rules -race -count=1 -cover` | P14 task_delta | exit==0 かつ coverage >= 90.0% | GoalEvidence（exit_code・coverage 数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-02 | TEST-GREEN, SCOPE-01, QUALITY-01 |
| P14-VG-02 | green | quality | agent | true | `go test ./internal/rules -run TestBuildPlan_Property -count=1` | P14 task_delta | exit==0（100 ランダムケースで plan が入力の関数であることを確認） | GoalEvidence（PASS 行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-02 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 14
- gate_ids: [P14-VG-01, P14-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（本 Process は参照のみ、転記なし）
- regeneration_required_when: P05 の `Evaluate`/`BuildPlan` のシグネチャ・優先度解決規則・Verification Gates のいずれかが変更されたとき
- appendix: process-14.appendix.md（実行時に Read しない）

## Implementation Notes
- **`TestBuildPlan_Priority`**: 異なる priority で矛盾する指示（例: 高優先度ルールが `app.start`、低優先度ルールが同一アプリの `app.stop`）を与え、高い方の指示が採用されることを確認する。
- **`TestBuildPlan_SamePriorityConflict`**: 同一 priority のルールが矛盾する指示を出したとき、その対象（bundle_id 等）だけ操作せず `Conflict` を plan の付随情報として返すことを確認する。他の対象への操作は正常に含まれることも確認する。
- **`TestBuildPlan_EnterOnce`**: `on_enter` がルール遷移サイクル（非該当→該当）でのみ1回発火し、該当が継続する間は再発火しないことを確認する。
- **`TestBuildPlan_ExitOnRuleRemovedAfterReload`**: reload によってルールが設定から消滅したとき、直前まで該当していたルールの `on_exit` が1回だけ発火することを確認する。
- **`TestBuildPlan_EnsureIdempotent`**: 同じ入力（実行中タスク・現在の状態）を2回 `BuildPlan` に流し、同じ plan が返ることを確認する（`ensure` アクションの冪等性）。
- **`TestBuildPlan_Property`**: 100 ランダムケース（実行中タスクの組み合わせ・複数ルールの優先度をランダム生成）で、同一入力に対して常に同一 plan が得られること（plan が入力の関数であること）を property test で確認する。
  // Why: 手動で書いたテストケースだけでは競合解決ロジックの分岐を網羅しきれない。ランダム生成による property test で「入力が同じなら出力も同じ」という参照透過性を広くカバーする。
- **`TestEvaluate_ContainsOR`**: `contains` 条件が複数指定されたとき OR で評価されることを確認する。
- **`TestEvaluate_NotContainsAndNot`**: `not_contains` 条件が複数指定されたとき AND-NOT（すべての条件を満たさない場合にのみ真）で評価されることを確認する。
- **`TestEvaluate_NoRunningTasks_Asymmetry`**: 実行中タスクが0件のとき、`contains` は常に偽・`not_contains` は常に真になる非対称性を仕様として固定する。
  // Why: この非対称性は直感に反しやすく、実装者が「0件なら両方とも判定不能」のように誤って実装しがちな箇所。テストで明示的に固定しないと将来のリファクタリングで壊れる。
- **`TestEvaluate_EmptyMatchAlwaysTrue`**: `match` が空のとき常に一致することを確認する。
- **`TestEvaluate_CaseInsensitive`**: タスク名とキーワードの比較が大文字小文字を区別しないことを確認する。
- **`TestBuildPlan_EnforceStopBundleIDsMatchActions`**: `enforce_stop_bundle_ids` が actions 内の `app.stop` の bundle_id 集合と厳密一致することを確認する。
- **`TestBuildPlan_NoProcessOrCommandActions`**: 生成された plan に `process.start` / `process.stop` / `command.run` が一切現れないことを確認する（`internal/rules` のスコープ制約の裏付け）。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- 異なる priority で矛盾する指示があるとき、`BuildPlan` は高い priority の指示を採用する。
- 同一 priority で矛盾する指示があるとき、対象への操作は行われず `Conflict` が返る（他の対象への操作は継続される）。
- `on_enter` はルール遷移サイクルでのみ1回発火し、連続する同一状態では再発火しない。reload によるルール消滅時は `on_exit` が1回だけ発火する。
- 同一入力に対して `BuildPlan` は常に同一の plan を返す（冪等性・参照透過性。100 ランダムケースで検証）。
- 実行中タスクが0件のとき、`contains` は偽、`not_contains` は真になる。`match` が空のとき常に一致する。比較は大文字小文字を区別しない。
- `enforce_stop_bundle_ids` は actions 内の `app.stop` の bundle_id 集合と厳密一致する。
- plan に `process.start` / `process.stop` / `command.run` は一切現れない。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- property test のランダム生成戦略（`math/rand` の使い方、シード固定の有無）
- テーブル駆動テストの要素型名
- `Conflict` の構造体フィールド名（対象識別子・関与したルールID等）

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P05 の green 状態であるため、テスト追加前に該当テスト名を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P14-VG-01/02）と共有する。
- [x] `internal/rules/evaluate_test.go` に5個のテスト関数を追加する
- [x] `internal/rules/plan_test.go` に8個のテスト関数（`TestBuildPlan_Property` を含む）を追加する

## Green Phase
- [x] `go test ./internal/rules -race -count=1 -cover` を実行し、追加した全テストケースが PASS することを確認する
- [x] `go test ./internal/rules -run TestBuildPlan_Property -count=1` を実行し、100 ランダムケースが PASS することを確認する
- [x] Behavior Specification の Correctness Criteria 全項目に対応するテストが存在し PASS することを確認する
- [x] coverage が 90.0% 以上であることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P14-VG-01 / status / command_or_action: `go test ./internal/rules -race -count=1 -cover` / exit_code / expected: 0 かつ coverage>=90.0% / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P14-VG-02 / status / command_or_action: `go test ./internal/rules -run TestBuildPlan_Property -count=1` / exit_code / expected: 0 / observed / attempt）

## Refactor Phase
- [x] D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `internal/rules/evaluate.go` / `plan.go` に差分が生じていないことを `git diff --stat` で確認する
- [x] 専用の Verification Gate はここには定義しない。D-09/D-12 の grep 結果は P14-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P05（`internal/rules` の実装が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
