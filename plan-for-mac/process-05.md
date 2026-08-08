# Process 05: ルール評価と plan 生成

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-05.md` を起動した際の自己完結ブリーフ。

- **背景**: バックエンドはユーザー定義ルール（`task_name_contains`/`task_name_not_contains` 等のマッチ条件と `priority`）を実行中タスク一覧に適用し、フロントが実行すべき `app.start`/`app.stop`/`notify` の plan を生成する必要がある。同一対象への矛盾する指示、ルールの有効/無効変化、ensure の再計算といった非対称な仕様が多く、純関数として厳密にテストできる形に切り出す価値が高い。
- **目的**: `internal/rules` パッケージに `Evaluate`（マッチ意味論の適用）と `BuildPlan`（priority 競合解決・重複排除・一回性/冪等性を反映した plan 生成）を実装する。
- **変更範囲**: `internal/rules/evaluate.go`, `internal/rules/plan.go` の新規作成。対応するテストファイル一式。P01/P02 が定義する `Config`/`RunningTask` 等の型は import のみで使用し、当該ファイルは編集しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| ActionIDFormat | "%d-%d" | フォーマット文字列 | `action_id` を `(cycle_id, seq)` から生成する際のフォーマット |
| MaxRules | 100 | 件 | 評価対象ルール数の上限（バリデーション） |
| MaxActionsPerRule | 20 | 件 | 1ルールが生成できるアクション数の上限（バリデーション） |
| NotifyTitleMaxRunes | 200 | rune数 | `notify` アクションのタイトル長上限 |
| NotifyMessageMaxRunes | 500 | rune数 | `notify` アクションのメッセージ長上限 |
| DefaultAppStopGraceSeconds | 10 | 秒 | `app.stop` アクションの既定猶予秒数 |
| MinGraceSeconds | 1 | 秒 | grace_seconds の下限バリデーション |
| MaxGraceSeconds | 120 | 秒 | grace_seconds の上限バリデーション |

- **禁止事項**:
  - D-09（マジックナンバー直書き禁止）: `NotifyTitleMaxRunes` 等の上限値・猶予秒数は上表の定数を参照する
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/rules` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **security**: plan の `actions[]` にタスク名を含めない（マッチ判定にのみ使用し、出力には rule_id 等の識別子のみを載せる）。
  - **error**: `Conflict` や `reason` は小文字スネークケースの enum（`ensure`/`on_enter`/`on_exit`、`rule_conflict` 等）で統一する。
  - **命名規約**: 本 Process が導入する定数はすべて `internal/constants/constants.go` に集約する。
  - **stdout/stderr**: 本 Process のコードは純関数であり、stdout/stderr への直接出力を行わない。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) core に列挙した更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/rules` パッケージは `(Config, []RunningTask, 前回Evaluation) → (Plan, []Conflict)` の純関数として実装する。マッチ意味論（`task_name_contains`/`task_name_not_contains` の OR/AND-NOT 非対称仕様含む）、priority による競合解決、同一 priority 競合時の skip、`on_enter`/`on_exit` の一回性、`ensure` の毎サイクル再計算による冪等性を扱う。`kind` は macOS 実装においてフロント担当の `app.start`/`app.stop`/`notify` の3種類のみで、`process.start`/`process.stop`/`command.run` は plan に一切現れない（バックエンド自身が同一実行ホストで実行するため）。

## Affected Files（パス・行番号・変更内容）
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/rules/evaluate.go` | 新規全体 | `Evaluation` 構造体、`Evaluate` 関数、`matchRule`, `containsAnyFold` |
| `internal/rules/plan.go` | 新規全体 | `Plan` 構造体、`PlannedAction` 構造体、`Conflict` 構造体、`BuildPlan`, `targetKey`, `dedupe` |
| `internal/rules/evaluate_test.go` | 新規全体 | マッチ意味論の正常系・異常系（実行中タスク0件時の非対称性含む）テスト |
| `internal/rules/plan_test.go` | 新規全体 | 競合解決・一回性・冪等性の全分岐テスト |

## Symbol Targets
```yaml
file: internal/rules/evaluate.go
symbols:
  - name: Evaluation
    kind: type
    line_hint: top
  - name: Evaluate
    kind: func
    line_hint: middle
  - name: matchRule
    kind: func
    line_hint: middle
  - name: containsAnyFold
    kind: func
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
# 根拠: internal/rules は新規パッケージ。P01/P02 が定義する Config/RunningTask 等の型は import のみで参照し、
# それらの定義ファイル自体は編集しない。internal/state（P03）, internal/ledger（P04）とも symbol 依存なし。
pre_flight_checks:
  - git_clean
  - go_build_ok
---
file: internal/rules/plan.go
symbols:
  - name: Plan
    kind: type
    line_hint: top
  - name: PlannedAction
    kind: type
    line_hint: top
  - name: Conflict
    kind: type
    line_hint: top
  - name: BuildPlan
    kind: func
    line_hint: middle
  - name: targetKey
    kind: func
    line_hint: middle
  - name: dedupe
    kind: func
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P05-VG-01 | red | test | agent | true | `go test ./internal/rules -count=1` | P05 task_delta | exit != 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-02 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P05-VG-02 | green | test | agent | true | `go test ./internal/rules -race -count=1` | P05 task_delta | exit == 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-02 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P05-VG-03 | green | conformance | agent | true | `go test ./internal/rules -run 'TestBuildPlan_(Priority\|SamePriorityConflict\|EnterOnce\|EnsureIdempotent)' -v` | P05 task_delta | exit == 0 かつ4ケースすべて PASS | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-02 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 05
- gate_ids: [P05-VG-01, P05-VG-02, P05-VG-03]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-05.appendix.md（実行時に Read しない）

## Implementation Notes
- **実行中タスク0件時の非対称性を仕様として固定する理由**: `contains` は「含むタスクが存在しない」ため常に偽、`not_contains` は「どのタスクも含まない」ため常に真、という非対称な結果になる。これは直感に反しやすいため、Behavior Specification に明示的なテストケースを置いて仕様として固定する。
  // Why: 曖昧に実装すると「タスクが1件もない朝はどのルールも一切発火しない」という直感的な誤り、あるいは逆に「タスクが1件もないのに contains ルールが発火する」という誤りのどちらかに倒れやすい。集合論的に厳密な定義（空集合に対する存在量化・全称量化）で固定する。
- **`ensure` が毎サイクル期待状態を再計算する理由**:
  // Why: on_exit だけに依存せず ensure が毎回期待状態を計算する。アプリ異常終了や設定変更時に取り消し処理が実行されない事故を防ぐ。
- **正規表現を使わない理由**: `containsAnyFold` は単純な大文字小文字非依存の部分文字列比較のみを行う。正規表現は特殊文字のエスケープやユーザー入力によるパフォーマンス劣化（catastrophic backtracking）のリスクがあり、MVP の要件（部分文字列一致のみ）には過剰な複雑さになるため採用しない。
- **`reload` で消滅したルールの on_exit**: ルールが設定リロードで削除された場合も、直前に有効だった場合は on_exit を1回だけ発火させる。`Evaluation` が前回の `active_rule_ids` を保持することでこれを実現する。

## Behavior Specification
System Type: transformation

`(Config, []RunningTask, 前回Evaluation) → (Plan, []Conflict)` の純関数。

| behavior_id | 入力 | 出力 | pre_state | post_state | invariants | test_ref |
|---|---|---|---|---|---|---|
| BEH-05-01 | ルールの `match.task_name_contains=["会議"]`、実行中タスクのいずれかの名前が "会議" を含む | ルールがマッチと判定される | - | - | いずれかの実行中タスク × いずれかの部分文字列、の OR×OR | TestEvaluate_ContainsSemantics_OR |
| BEH-05-02 | ルールの `match.task_name_not_contains=["休憩"]`、どの実行中タスク名も "休憩" を含まない | ルールがマッチと判定される | - | - | どのタスク × どの部分文字列も含まない、の AND-NOT | TestEvaluate_NotContainsSemantics_ANDNOT |
| BEH-05-03 | `match` フィールドが空/省略 | ルールが常にマッチと判定される | - | - | - | TestEvaluate_EmptyMatch_AlwaysTrue |
| BEH-05-04 | 実行中タスクが0件、ルールが `task_name_contains` を持つ | ルールが偽と判定される（マッチしない） | - | - | 空集合に対する存在量化は常に偽 | TestEvaluate_ZeroTasks_ContainsIsFalse |
| BEH-05-05 | 実行中タスクが0件、ルールが `task_name_not_contains` を持つ | ルールが真と判定される（マッチする） | - | - | 空集合に対する全称量化は常に真。BEH-05-04 との非対称性を固定する | TestEvaluate_ZeroTasks_NotContainsIsTrue |
| BEH-05-06 | 実行中タスク名 "MEETING"、ルールの部分文字列 "meeting"（大文字小文字違い） | ルールがマッチと判定される | - | - | 大文字小文字を区別しない | TestEvaluate_CaseInsensitiveMatch |
| BEH-05-07 | 異なる priority のルール2件が同一 `bundle_id` に対し矛盾する `app.start`/`app.stop` を指示 | 高 priority 側の指示のみが plan の `actions[]` に反映される | - | 低 priority 側の指示は破棄される。`Conflict` は返らない | - | TestBuildPlan_Priority |
| BEH-05-08 | 同一 priority のルール2件が同一 `bundle_id` に対し矛盾する `app.start`/`app.stop` を指示 | 対象 `bundle_id` に対する actions は生成されず、`Conflict` が1件返る | - | 呼び出し側が `notify{rule_conflict}` を出す前提の `Conflict` が返る | 対象外の他ルールは通常通り actions に反映される | TestBuildPlan_SamePriorityConflict |
| BEH-05-09 | 同一 `bundle_id` に対し同一種の指示（例: `app.start` が複数）が複数ルールから発行される | `actions[]` に1件のみ残る（重複排除済み） | - | - | dedupe は1件に畳む | TestBuildPlan_Dedupe |
| BEH-05-10 | 前回 Evaluation でルール R が無効、今回 R が有効に変化 | R について `reason:"on_enter"` の action が生成される | 前回 `active_rule_ids` に R を含まない | 今回 `active_rule_ids` に R を含む | on_enter は「無効→有効」に変化したサイクルでのみ1回 | TestBuildPlan_EnterOnce |
| BEH-05-11 | 前回 Evaluation でルール R が有効、今回 R が無効に変化 | R について `reason:"on_exit"` の action が生成される | 前回 `active_rule_ids` に R を含む | 今回 `active_rule_ids` に R を含まない | on_exit は「有効→無効」に変化したサイクルでのみ1回 | TestBuildPlan_ExitOnce |
| BEH-05-12 | 前回有効だったルール R が今回の Config から削除（reload で消滅） | R について `reason:"on_exit"` の action が1回だけ生成される | 前回 `active_rule_ids` に R を含む | 今回 Config に R が存在しない | reload で消滅したルールも on_exit を1回だけ発火する | TestBuildPlan_ExitOnce_RuleRemovedByReload |
| BEH-05-13 | ルール R が `ensure` タイプで、前回・今回とも同じ期待状態 | 前回と同じ内容の `reason:"ensure"` action が今回も生成される | - | - | ensure は毎サイクル期待状態を再計算し、同じ結果なら同じ actions を生成してよい（冪等） | TestBuildPlan_EnsureIdempotent |
| BEH-05-14 | 有効な `Plan` を生成した場合の `enforce_stop_bundle_ids` | `actions[]` 内の `kind=="app.stop"` の bundle_id 集合と厳密一致する | - | - | `enforce_stop_bundle_ids` は `app.stop` の bundle_id 集合の派生値であり独立に生成されない | TestBuildPlan_EnforceStopBundleIdsMatchesActions |
| BEH-05-15 | ルールが `process.start`/`process.stop`/`command.run` に相当する意図を持つ設定（誤設定または将来拡張の入力） | plan の `actions[]` に `process.*`/`command.run` の kind は一切現れない | - | - | macOS 実装では `kind` は `app.start`/`app.stop`/`notify` の3種のみ | TestBuildPlan_NeverEmitsProcessOrCommandKinds |

### Correctness Criteria（観測可能・固定する）
- `containsAnyFold` は正規表現を使わず、単純な大文字小文字非依存の部分文字列比較のみで実装される。
- `action_id` は `ActionIDFormat`(`"%d-%d"`) を用いて `(cycle_id, seq)` から一意に生成される。
- `Plan.enforce_stop_bundle_ids` は常に `actions[]` 内の `kind=="app.stop"` の bundle_id 集合と厳密一致する（生成後に別途手動で追加・削除されることがない）。
- 同一 priority での矛盾は必ず `Conflict` として返り、その対象 `bundle_id`/`process_id` には actions が一切生成されない（部分的に片方だけ採用されることはない）。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `targetKey` の内部表現（文字列連結か構造体キーか）
- `Evaluation` 内部で `active_rule_ids` をどのデータ構造（map か slice）で保持するか
- `dedupe` のソート順（決定的な出力順序を保証する内部実装）

## Red Phase
- [x] `internal/rules/evaluate_test.go` に BEH-05-01〜06 のテストケースを実装し、実装がないため失敗することを確認する
- [x] `internal/rules/plan_test.go` に BEH-05-07〜15 のテストケースを実装する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P05-VG-01 / status / command_or_action: `go test ./internal/rules -count=1` / exit_code / expected: != 0 / observed / attempt）

## Green Phase
- [x] `internal/rules/evaluate.go`, `plan.go` を実装し、Behavior Specification 表の全行に test_ref のテストが存在し PASS することを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P05-VG-02 / status / command_or_action: `go test ./internal/rules -race -count=1` / exit_code / expected: 0 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P05-VG-03 / status / command_or_action: `go test ./internal/rules -run 'TestBuildPlan_(Priority|SamePriorityConflict|EnterOnce|EnsureIdempotent)' -v` / exit_code / expected: 0、4ケース全PASS / observed / attempt）

## Refactor Phase
- [x] D-12（TODO/FIXME）がゼロヒットであることを確認する
- [x] マジックナンバー（grace秒数・rune上限等）が `internal/constants` の定数経由になっていることをレビューする
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P05-VG-03 / status / command_or_action: レビュー完了確認 / exit_code / expected: N/A / observed / attempt）

## Manual Verification
1. **executor: agent** — 操作: 実行中タスクが0件の状態で `task_name_contains` ルールと `task_name_not_contains` ルールを両方持つ Config を `Evaluate` に渡す → 期待される出力: `contains` ルールは非マッチ、`not_contains` ルールはマッチ → 確認方法: `Evaluation.active_rule_ids` に後者の rule_id のみが含まれること
2. **executor: agent** — 操作: 同一 priority・同一 bundle_id に矛盾する2ルールを含む Config を `BuildPlan` に渡す → 期待される出力: 当該 bundle_id への actions が生成されず `Conflict` が1件返る → 確認方法: 戻り値の `[]Conflict` に対象 bundle_id が含まれ、`Plan.actions` に対象 bundle_id への `app.start`/`app.stop` が存在しないこと

## Dependencies
- Requires: P01, P02
- Blocks: P06, P14
