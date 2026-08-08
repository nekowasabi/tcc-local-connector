# Process 10: 設定検証テスト網羅

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-10.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/config`（P01）は所有者・権限検証（段階1）とスキーマ検証（段階2、15種のエラーコード）を実装し、`len(errors)>0` のとき `*Config` に必ず `nil` を返すことで部分適用を型で防ぐ設計を採る。しかし P01 時点のテストは主要経路の確認にとどまり、15種のエラーコードすべて・reload 失敗時の直前設定維持・`KnownFields(true)` の実際の動作・権限検証6分岐が体系的に網羅されているとは限らない。この安全性の根幹（`errors>0 ⇒ cfg==nil`）を後続 Process（P07 の reload_config、P52 の実行時二重ガード）が信頼できる前提にするため、独立したテスト強化 Process として切り出す。
- **目的**: `internal/config/config_test.go`, `internal/config/validate_test.go` に表駆動テストを追加し、15種の検証エラーコード全件・`TestLoadInvalidReturnsNilConfig`・`TestReloadInvalidKeepsPrevious`・`TestUnknownField`・権限検証6分岐を網羅する。
- **変更範囲**: `internal/config/config_test.go`, `internal/config/validate_test.go` の2ファイルのみ。`internal/config/config.go` / `validate.go` / `permissions.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:
  本 Process はテスト専用であり定数を直接参照しない。テストケース中の境界値（例: 権限マスク `0o022`/`0o077` 相当のモード値）はテストデータとして直接記述し、D-09 の検査対象外（`*_test.go`）として扱う。

- **禁止事項**:
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/config --glob '!*_test.go'` → 期待ヒット数 0（本 Process はテストファイルしか変更しないため、非テストファイルに新規ヒットは生じない）
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/config/config_test.go internal/config/validate_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/config/config.go` / `validate.go` / `permissions.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P01）の全 `BEH-01-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **error**: 検証エラーは小文字スネークケースの code を持つ。テストはこの code 文字列を直接比較し、メッセージ文言には依存しない。
  - **命名規約**: 追加するテスト関数名は `Test<対象>_<観点>` 形式（例: `TestValidate_UnsupportedConfigVersion`）に統一し、既存の命名規則（P01 で確立済み）を踏襲する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/config` パッケージの検証ロジック（`Validate`）は、TaskChute Cloud 2 連動アプリの唯一の設定入力を検査する最終防衛線であり、`task_source.executable` / `process.start.executable` / `command.run.executable` を通じて任意コード実行に直結する。本 Process は、この防衛線が 15 種の検証エラーコードすべてで正しく機能し、かつ「検証エラーが1件でもあれば `*Config` は必ず `nil`」という部分適用防止の不変条件が破られていないことを、表駆動テストで機械的に固定する。あわせて `Reload` 相当の操作が失敗した場合に直前の有効な設定を維持すること、YAML の `KnownFields(true)` が未知キーを実際に拒否することも検証する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/config/config_test.go` | 既存末尾に追記 | `TestLoadInvalidReturnsNilConfig`, `TestReloadInvalidKeepsPrevious`, `TestUnknownField` |
| `internal/config/validate_test.go` | 既存末尾に追記 | 15種のエラーコードを網羅する表駆動テスト `TestValidate_ErrorCodes`、権限検証6分岐のテスト |

## Symbol Targets
```yaml
file: internal/config/config_test.go
symbols:
  - name: TestLoadInvalidReturnsNilConfig
    kind: func
    line_hint: bottom
  - name: TestReloadInvalidKeepsPrevious
    kind: func
    line_hint: bottom
  - name: TestUnknownField
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/config/*_test.go のみを変更する。W05 内の他 Process（P11-P15, P17）は internal/tcc2, internal/state, internal/ledger, internal/rules, macos/Tests のいずれかで internal/config には触れない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/config/validate.go:Validate"
---
file: internal/config/validate_test.go
symbols:
  - name: TestValidate_ErrorCodes
    kind: func
    line_hint: bottom
  - name: TestValidate_PermissionViolations
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/config/validate.go:Validate"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P10-VG-01 | green | test | agent | true | `go test ./internal/config -race -count=1 -cover` | P10 task_delta | exit==0 かつ coverage >= 85.0% | GoalEvidence（exit_code・coverage 数値・最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 10
- gate_ids: [P10-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（本 Process は参照のみ、転記なし）
- regeneration_required_when: P01 の検証エラーコード一覧・`Validate`/`CheckPermissions` のシグネチャ・Verification Gates（P10-VG-01）のいずれかが変更されたとき
- appendix: process-10.appendix.md（実行時に Read しない）

## Implementation Notes
- **15種の検証エラーコードを表駆動で網羅**（`unsupported_config_version` / `missing_required_field` / `unknown_field` / `unknown_action_type` / `unsupported_action` / `duplicate_rule_id` / `duplicate_process_id` / `value_out_of_range` / `timeout_exceeds_interval` / `unsupported_value` / `executable_not_found` / `invalid_identifier` / `shell_not_allowed` / `forbidden_safety_flag` / `rule_conflict_same_priority`）。各コードにつき最小限の壊れた設定断片を用意し、`Validate` が返す `[]ValidationError` に該当 code がちょうど1件含まれることを確認する。
  // Why: 15種を個別の Test 関数にすると保守コストが線形に増える。テーブル駆動にすることで新しいエラーコードの追加時に1行足すだけで済む。
- **`TestLoadInvalidReturnsNilConfig`**: `errors` が1件以上あるとき `Load` の戻り値 `*Config` が必ず `nil` であることを検証する。これは D-11（設定検証エラー時の部分適用の禁止）を型レベルで裏付ける唯一の自動テストであり、`FINAL-VG-04` の反証レビューが参照する。
- **`TestReloadInvalidKeepsPrevious`**: 一度正しい設定を読み込んだ後、無効な設定に書き換えて reload 相当の操作を行い、呼び出し側が保持する「直前の有効な `*Config`」が変化しないことを確認する（`Load` 自体は nil を返すが、呼び出し側の状態は壊れない）。
- **`TestUnknownField`**: YAML に未定義キーを含めたとき `yaml.Decoder.KnownFields(true)` の効果で `unknown_field` エラーになることを確認する。これは A-3（Assumption）の検証でもある。
- **権限検証6分岐**: 所有者 UID 不一致 / group 書き込み可能（`ConfigForbiddenModeMask` 相当） / other 書き込み可能 / 通常ファイルでない（ディレクトリ等） / symlink 解決後の再検証（O_NOFOLLOW で開いた fd に対する検証） / `allow_shell:true` かつ mode `0644`（`ConfigStrictModeMask` 超過）。すべて `insecure_permissions` の RPCError 相当として拒否され、`errors[]` 経路（スキーマ検証）とは独立した分岐であることを確認する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- `Validate` は15種のエラーコードのそれぞれについて、最小の壊れた入力に対し該当 code を含む `[]ValidationError` を返す。
- `len(errors) > 0` のとき、`Load` の戻り値 `*Config` は必ず `nil` である（`TestLoadInvalidReturnsNilConfig` で固定）。
- reload 相当の操作が無効な設定を検出したとき、呼び出し側が保持する直前の有効な `*Config` は変更されない（`TestReloadInvalidKeepsPrevious` で固定）。
- YAML に未定義キーが含まれる場合、`unknown_field` エラーになる（`TestUnknownField` で固定）。
- 権限検証6分岐（所有者不一致・group書込可・other書込可・非通常ファイル・symlink解決後再検証・`allow_shell` 時の追加マスク）は、スキーマ検証（`errors[]`）とは独立した拒否経路である。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- 表駆動テストのテーブル要素の型名・フィールド名
- 各エラーコード用の最小設定断片をどう組み立てるか（インラインリテラルかヘルパ関数か）
- 権限検証テストで一時ファイルを作る際のディレクトリ構成（`t.TempDir()` の使い方）

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P01 の green 状態（`Validate`/`Load`/`CheckPermissions` は既に実装済みで正しく動作する）であるため、テスト関数を追加する前の状態で `go test ./internal/config -run 'TestValidate_ErrorCodes|TestValidate_PermissionViolations|TestLoadInvalidReturnsNilConfig|TestReloadInvalidKeepsPrevious|TestUnknownField'` を実行しても該当テスト名が存在せず「no tests to run」で exit 0 となり、意図的な失敗を観測できない。したがって Red Phase の完了は Green Phase の gate（P10-VG-01）と共有し、個別の ✅ Phase Complete はここには記載しない。
- [x] `internal/config/validate_test.go` に15種のエラーコードのテーブルと権限検証6分岐のテストケースを追加する（この時点ではまだ full green gate を実行しない）
- [x] `internal/config/config_test.go` に `TestLoadInvalidReturnsNilConfig` / `TestReloadInvalidKeepsPrevious` / `TestUnknownField` を追加する

## Green Phase
- [x] `go test ./internal/config -race -count=1 -cover` を実行し、追加した全テストケースが PASS することを確認する
- [x] Behavior Specification の Correctness Criteria 5項目すべてに対応するテストが存在し PASS することを確認する
- [x] coverage が 85.0% 以上であることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P10-VG-01 / status / command_or_action: `go test ./internal/config -race -count=1 -cover` / exit_code / expected: 0 かつ coverage>=85.0% / observed / attempt）

## Refactor Phase
- [x] D-09（マジックナンバー直書き禁止。`*_test.go` は対象外）と D-12（TODO/FIXME 残存禁止）の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `internal/config/config.go` / `validate.go` / `permissions.go` に差分が生じていないことを `git diff --stat` で確認する（`patch_only:true` の遵守）
- [x] 専用の Verification Gate はここには定義しない。D-09/D-12 の grep 結果は Implementation Brief の禁止事項セクションで照合済みであり、P10-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P01（`internal/config` の実装が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
