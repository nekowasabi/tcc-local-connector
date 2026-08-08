# Process 11: 解析黄金テスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-11.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/tcc2/parse.go`（P02）は `get_taskchute` のテキスト応答から実行中タスクを抽出する唯一の経路であり、リスク R1（TaskChute Cloud 2 の表示形式変更で解析が壊れ「実行中0件」と誤判定 → `not_contains` ルールが全発火し利用者のアプリが意図せず終了させられる）に直結する。実測フォーマット（RESEARCH.md F2〜F8）に基づく黄金 fixture テストと、解析の落とし穴となる分岐（名前中の `(`/`[`、先頭空白、ID欠落、`isError`）を専用のテスト強化 Process として固定する。
- **目的**: `internal/tcc2/parse_test.go` に実測フォーマットの黄金 fixture テストと16個の異常系・境界値テストを追加する。
- **変更範囲**: `internal/tcc2/parse_test.go` の拡張、および匿名化済み fixture 3ファイル（`testdata/get_taskchute_sample.txt`, `testdata/get_user_sample.txt`, `testdata/get_taskchute_error.json`）の新規作成。`internal/tcc2/parse.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `MaxRunningTasks` | 32 | count | `TestParse_TooManyRunningTasks` で33件を投入し `too_many_running_tasks` を確認する境界値 |
| `TaskIDPattern` | `^task_[0-9a-f]{32}$` | regexp | `TestParse_TaskIDFormatMismatch` で不一致パターンを判定する基準 |
| `IDBlockKeys` | Section, Project, Mode, Routine, Tags | list | `TestParse_MissingOptionalIDKeys` / `TestParse_UnknownIDKey` の期待値 |
| `KnownStatusTags` | Done, "In Progress", Todo | list | `TestParse_GoldenFixture` の `StatusCounts` 期待値のキー集合 |

- **禁止事項**:
  - D-08（`in-progress count` への依存禁止）: `rg -ni 'in-progress count|in_progress_count' internal/tcc2/parse_test.go` → 期待ヒット数 0
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/tcc2 --glob '!*_test.go'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/tcc2/parse_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/tcc2/parse.go` / `taskchute.go` / `version.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P02）の全 `BEH-02-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **security**: fixture は実測データを匿名化したもの（タスク名を伏字化し、ID を架空値に置換）。メールアドレスを一切含めない。
  - **error**: 「解析失敗」（`error != nil`）と「実行中タスクなし」（`error == nil` かつ `RunningTasks` 空）を型レベルで区別するテストを最優先で書く。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`ParseTaskChuteText` はテキストのみが提供される非契約インターフェース（TaskChute Cloud 2 の表示形式は API 契約ではない）を解析する唯一の経路であり、R1（表示形式変更による誤判定）の主要な緩和策は黄金 fixture テストと分岐網羅である。本 Process は実測フォーマット（F2〜F8）に基づく fixture を用いて `StatusCounts` を厳密に照合し、名前中の特殊文字・先頭空白・ID欠落・`isError`・実行中0件など、実装が誤りやすい16個の分岐を個別のテストとして固定する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/tcc2/parse_test.go` | 既存末尾に追記 | 16個のテスト関数（下記 Implementation Notes 参照） |
| `internal/tcc2/testdata/get_taskchute_sample.txt` | 新規 | 実測フォーマットに準拠した匿名化済み黄金 fixture（Done 111 / In Progress 1 / Todo 4） |
| `internal/tcc2/testdata/get_user_sample.txt` | 新規 | `get_user` markdown 太字形式の匿名化済み fixture |
| `internal/tcc2/testdata/get_taskchute_error.json` | 新規 | `isError:true` の MCP エラー応答 fixture |

## Symbol Targets
```yaml
file: internal/tcc2/parse_test.go
symbols:
  - name: TestParse_GoldenFixture
    kind: func
    line_hint: bottom
  - name: TestParse_NoRunningTask
    kind: func
    line_hint: bottom
  - name: TestParse_UnrecognizedFormat
    kind: func
    line_hint: bottom
  - name: TestParse_APIError
    kind: func
    line_hint: bottom
  - name: TestParse_EmptyContent
    kind: func
    line_hint: bottom
  - name: TestParse_TooManyRunningTasks
    kind: func
    line_hint: bottom
  - name: TestParse_NameContainsParens
    kind: func
    line_hint: bottom
  - name: TestParse_NameContainsBracket
    kind: func
    line_hint: bottom
  - name: TestParse_LeadingSpacePreserved
    kind: func
    line_hint: bottom
  - name: TestParse_DoneLineWithProgressInName
    kind: func
    line_hint: bottom
  - name: TestParse_TaskIDFormatMismatch
    kind: func
    line_hint: bottom
  - name: TestParse_MissingOptionalIDKeys
    kind: func
    line_hint: bottom
  - name: TestParse_UnknownIDKey
    kind: func
    line_hint: bottom
  - name: TestParse_DateAttribution
    kind: func
    line_hint: bottom
  - name: TestParse_MultipleRunningTasks
    kind: func
    line_hint: bottom
  - name: TestParseUser_Fields
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/tcc2/parse_test.go と internal/tcc2/testdata/ のみを変更する。W05 内の他 Process は internal/tcc2 に触れない。既存 probe.go は本 Process の対象外であり参照もしない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/tcc2/parse.go:ParseTaskChuteText"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P11-VG-01 | green | test | agent | true | `go test ./internal/tcc2 -race -count=1 -cover` | P11 task_delta | exit==0 かつ coverage >= 85.0% | GoalEvidence（exit_code・coverage 数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-04 | TEST-GREEN, SCOPE-01, QUALITY-01 |
| P11-VG-02 | green | conformance | agent | true | `go test ./internal/tcc2 -run TestParse_GoldenFixture -v` | P11 task_delta | exit==0。fixture 由来の `StatusCounts` が `{Done:111, "In Progress":1, Todo:4}` と一致 | GoalEvidence（PASS 行と StatusCounts の実測値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-04 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 11
- gate_ids: [P11-VG-01, P11-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（`MaxRunningTasks`, `TaskIDPattern`, `IDBlockKeys`, `KnownStatusTags`）
- regeneration_required_when: 実測フォーマット（RESEARCH.md F2〜F8）・`ParseTaskChuteText`/`parseUserText` のシグネチャ・Verification Gates のいずれかが変更されたとき
- appendix: process-11.appendix.md（実行時に Read しない）

## Implementation Notes
- **`TestParse_GoldenFixture`**: fixture 由来の `TaskChuteResult.StatusCounts` が `{Done:111, "In Progress":1, Todo:4}` と一致することを確認する。実測データ（RESEARCH.md F3）と同じ分布を匿名化 fixture で再現する。
  // Why: 個別の分岐テストだけでは「実測フォーマット全体を通して解析したときに数え落としがないか」を検証できない。黄金 fixture はリスク R1 の主要な緩和策として PLAN-for-mac.md に明記されている。
- **`TestParse_NoRunningTask`**: `- [` 行はあるが In Progress が0件のとき、`error == nil` かつ `RunningTasks` が空であることを確認する（**正常系**）。
- **`TestParse_UnrecognizedFormat`**: 非空行はあるが `- [` 行が0件のとき、`unrecognized_format` で**解析失敗**することを確認する。この2つのテストが並んでいること自体が「実行中なし」と「解析失敗」を型で区別する設計（BEH-02 の中核）の証拠になる。
- **`TestParse_APIError`**: `isError:true` の fixture から `tcc2_api_error` になることを確認する。
- **`TestParse_EmptyContent`**: text が空のとき `empty_content` になることを確認する。
- **`TestParse_TooManyRunningTasks`**: In Progress を `MaxRunningTasks`(32) を1件超える33件用意し `too_many_running_tasks` になることを確認する（暴走ガードの境界値テスト）。
- **`TestParse_NameContainsParens`**: タスク名に全角括弧・鍵括弧を含む行を正しく切り出す。`IDBlockDelimiter`（" [ID: "）を **右（LastIndex）から探す**実装でないと、名前中の `(` や関連する丸括弧トークンとの衝突で名前が誤って切り詰められる。
- **`TestParse_NameContainsBracket`**: タスク名に `[` を含む行。右アンカーでない実装（左から `[` を探す）だと壊れることを確認する。
- **`TestParse_LeadingSpacePreserved`**: `] ` の直後に追加の空白がある行で、名前を**左トリムせず**保持することを確認する（`TrimRight` のみを使う実装の裏付け）。
- **`TestParse_DoneLineWithProgressInName`**: `- [Done] progress整理 …` のような行を実行中と誤検出しないことを確認する（実測 F4 で報告された誤検知源の再現）。
- **`TestParse_TaskIDFormatMismatch`**: `task_id` が `TaskIDPattern` に不一致のとき `task_id=""` かつ `warnings` に `task_id_format` が積まれ、**解析全体は失敗させない**ことを確認する。
- **`TestParse_MissingOptionalIDKeys`**: Project/Mode/Routine が欠落しても成功することを確認する。
- **`TestParse_UnknownIDKey`**: 未知キーがあるとき `warnings` に `unknown_id_key:<key>` が積まれることを確認する。
- **`TestParse_DateAttribution`**: `## … YYYY-MM-DD` 見出しで日付が帰属し、見出し前の行は `date=""` になることを確認する（OQ-3 の Start of Day 符号問題の緩和策の裏付け）。
- **`TestParse_MultipleRunningTasks`**: 全件が出現順で返され、並べ替えられないことを確認する。
- **`TestParseUser_Fields`**: `- **<ラベル>:** <値>` 形式から Timezone / Start of Day / Default View ID を抽出することを確認する（`UserFieldPattern` の裏付け）。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- 実測フォーマットの黄金 fixture を解析した結果の `StatusCounts` は `{Done:111, "In Progress":1, Todo:4}` と一致する。
- `- [` 行があり In Progress が0件のとき `error == nil` かつ `RunningTasks` は空（実行中なし＝正常系）。
- `- [` 行が0件のとき（非空行はある）`unrecognized_format`（解析失敗）になる。この2分岐は型として区別される（`error` の有無で判別可能）。
- `isError:true` のとき `tcc2_api_error`、text が空のとき `empty_content`、In Progress が33件以上のとき `too_many_running_tasks` になる。
- タスク名に `(` `[` を含む行、および `]` 直後に追加空白がある行で、名前の切り出しが崩れない。
- `task_id` が `TaskIDPattern` に不一致でも解析は失敗せず、`warnings` に記録される。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- fixture ファイルの匿名化の具体的な置換ルール（伏字パターン・架空 ID の生成規則）
- テーブル駆動テストのテーブル要素の型名
- 各テストケース内でのアサーションヘルパ関数の有無

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P02 の green 状態（`ParseTaskChuteText`/`parseUserText` は実測フォーマットに基づき既に実装済み）であるため、テスト追加前に該当テスト名を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P11-VG-01/02）と共有する。
- [x] 匿名化済み fixture 3ファイルを `testdata/` に作成する
- [x] `internal/tcc2/parse_test.go` に16個のテスト関数を追加する

## Green Phase
- [x] `go test ./internal/tcc2 -race -count=1 -cover` を実行し、追加した全テストケースが PASS することを確認する
- [x] `TestParse_GoldenFixture` の `StatusCounts` が `{Done:111, "In Progress":1, Todo:4}` と厳密一致することを確認する
- [x] Behavior Specification の Correctness Criteria 全項目に対応するテストが存在し PASS することを確認する
- [x] coverage が 85.0% 以上であることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P11-VG-01 / status / command_or_action: `go test ./internal/tcc2 -race -count=1 -cover` / exit_code / expected: 0 かつ coverage>=85.0% / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P11-VG-02 / status / command_or_action: `go test ./internal/tcc2 -run TestParse_GoldenFixture -v` / exit_code / expected: 0 かつ StatusCounts 一致 / observed / attempt）

## Refactor Phase
- [x] D-08（`in-progress count` への依存禁止）・D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] fixture にメールアドレスや実在のタスク名が含まれていないことを目視確認する
- [x] `internal/tcc2/parse.go` / `taskchute.go` / `version.go` に差分が生じていないことを `git diff --stat` で確認する
- [x] 専用の Verification Gate はここには定義しない。D-08/D-09/D-12 の grep 結果は P11-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P02（`internal/tcc2` の実装が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
