# Process 202: docs/config-schema.md 新規

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-202.md` を起動した際の自己完結ブリーフ。

- **背景**: 設定スキーマ（§1.5）は `internal/config` パッケージ（P01）で実装される契約だが、利用者・レビュアーが参照できる独立した仕様文書が存在しない。特に「`config.yml` を書き換えられる利用者は本アプリの権限で任意コードを実行できる」という性質は、実装のコメントだけでは伝わらず、明文化された警告が必要である。
- **目的**: §1.5 の全キー・全アクション種別6種・検証エラーコード15種・マッチ意味論の非対称性・権限違反とスキーマ違反の2段階分岐・適用ポリシーを1文書に集約し、任意コード実行の性質を明記する。
- **変更範囲**: `docs/config-schema.md`（新規作成のみ）。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `ConfigSchemaVersion` | 2 | — | `version` キーの必須値として明記する |
| `MinPollIntervalSeconds` / `DefaultPollIntervalSeconds` / `MaxPollIntervalSeconds` | 10 / 60 / 3600 | 秒 | `polling.interval_seconds` の範囲・既定値 |
| `MinPollTimeoutSeconds` / `DefaultPollTimeoutSeconds` / `MaxPollTimeoutSeconds` | 1 / 20 / 120 | 秒 | `polling.timeout_seconds` の範囲・既定値。`interval` 未満の制約も明記 |
| `MinFailureGraceSeconds` / `DefaultFailureGraceSeconds` / `MaxFailureGraceSeconds` | 0 / 180 / 3600 | 秒 | `polling.failure_grace_seconds` の範囲・既定値 |
| `MinActionTimeoutSeconds` / `DefaultActionTimeoutSeconds` / `MaxActionTimeoutSeconds` | 1 / 30 / 300 | 秒 | `command.run.timeout_seconds` の範囲・既定値 |
| `MinGraceSeconds` / `DefaultAppStopGraceSeconds` / `DefaultProcessStopGraceSeconds` / `MaxGraceSeconds` | 1 / 10 / 10 / 120 | 秒 | `app.stop`/`process.stop` の `grace_seconds` の範囲・既定値 |
| `NotifyTitleMaxRunes` / `NotifyMessageMaxRunes` | 200 / 500 | runes | `notify` アクションのフィールド長 |
| `MinLogRetainDays` / `DefaultLogRetainDays` / `MaxLogRetainDays` | 1 / 14 / 365 | days | `logging.retain_days` の範囲・既定値 |
| `MaxRules` / `MaxActionsPerRule` | 100 / 20 | count | `rules` の上限 |
| `RuleIDPattern` / `ProcessIDPattern` | `^[a-z0-9][a-z0-9-]{0,63}$` | regexp | `rules[].id` / `process.start.process_id` の形式検証 |
| `BundleIDPattern` | `^[A-Za-z0-9][A-Za-z0-9._-]*$` | regexp | `app.start`/`app.stop` の `bundle_id` の形式検証 |
| `EnvKeyPattern` | `^[A-Za-z_][A-Za-z0-9_]*$` | regexp | `process.start.env` のキー検証 |
| `ConfigForbiddenModeMask` | 0o022 | mask | 配置のパーミッション要件（mode 0600 推奨の根拠） |
| `ConfigStrictModeMask` | 0o077 | mask | `allow_shell: true` 時の追加パーミッション要求 |

- **禁止事項**:
  - D-15（未導入 linter の記載禁止）: `rg -n 'golangci-lint|swiftlint|swift-format' docs/config-schema.md` → 期待 0 件
  - D-14（`.xcodeproj`/`xcodebuild` 等の記載禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' docs/config-schema.md` → 期待 0 件
  - D-09（マジックナンバー直書き禁止）は `internal/{engine,rules,state,ledger,config}` が対象でありコードを持たない本 Process には構造的に適用されないが、数値は必ず ★ Constants の定数名と併記する（利用者が実装の正本と突き合わせられるようにするため）

- **適用される横断方針（インライン展開）**:
  - **security**: `command.run` と `shell: true` が任意コード実行経路であることを明記する。対策として所有者・権限検証（内容を1バイトも読まずに拒否する2段階分岐）と `allow_shell` 既定 false があることを明記する。
  - **静かな誤動作の防止**: `browser.redirect` を静かに無視せず `unsupported_action` として明示拒否することを明記する。
  - **マッチ意味論の非対称性**: 実行中タスクが0件のとき `task_name_contains` は偽・`task_name_not_contains` は真という非対称性を明記する（これを見落とすと「タスクがない = 何もしない」と誤解する実装者がいる）。
  - **Candor（A7）**: 「`config.yml` を書き換えられる利用者は本アプリの権限で任意コードを実行できる」性質を明記する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`docs/config-schema.md` をリポジトリ直下の `docs/` に新規作成する。対象読者は「`config.yml` を書く利用者」と「`internal/config` を実装・レビューする開発者」の両方であり、全キーの階層・型・必須/任意・既定値・範囲、アクション種別6種の全フィールド、マッチ意味論、検証エラーコード15種、権限違反とスキーマ違反の2段階分岐、適用ポリシー、そして「任意コード実行」という性質そのものを記載する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `docs/config-schema.md` | 新規全体 | 配置・全キー階層・アクション種別6種・`browser.redirect`拒否・マッチ意味論・検証エラー15種・2段階分岐・適用ポリシー・任意コード実行の明記の9節 |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: docs/config-schema.md
symbols:
  - name: config-schema-document
    kind: document
    line_hint: top
  - name: validation-error-codes-section
    kind: document-section
    line_hint: middle
  - name: arbitrary-code-execution-notice
    kind: document-section
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - symbol_exists:internal/config/validate.go:Validate
```

**Notes**:
- `arbitrary-code-execution-notice` は本 Process の必須節であり、P202-VG-01 が「任意コード」の出現を grep で照合する対象。
- `internal/config/validate.go:Validate` は P01 完了後に存在するシンボルであり、本 Process は P01 完了後（依存関係上は P102 経由）に実行することを前提とする。

## Verification Gates（P202）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P202-VG-01 | green | grep | agent | true | `rg -c '任意コード' docs/config-schema.md` | P202 task_delta | ヒット >= 1 | GoalEvidence | failure_class:grep, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | SCOPE-01,DONT-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 202
- gate_ids: [P202-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: 設定スキーマ（キー・型・範囲・アクション種別・検証エラーコード）を変更したとき、または ★ Constants の該当値を変更したとき
- appendix: process-202.appendix.md（実行時に Read しない）

## Implementation Notes
以下9項目を記載する。
1. **配置**: `~/.config/tcc-local-connector/config.yml`。mode 0600 推奨、`allow_shell: true` の場合は必須。
2. **全キーの階層・型・必須/任意・既定値・範囲**: `version`(2) / `polling`{interval_seconds, timeout_seconds, failure_grace_seconds, failure_policy(`release_controls`のみ)} / `task_source`{type(`tcc2_mcp`のみ), executable(絶対パス必須), args(既定`["mcp"]`), view_id(既定null)} / `safety`{dry_run, allow_shell, allow_external_process_control, allow_force_terminate（後2者は true が検証エラー）} / `logging`{level(debug|info|warn|error,既定info), retain_days} / `rules`[]{id, priority, match{task_name_contains, task_name_not_contains}, ensure[], on_enter[], on_exit[]} を表形式で網羅する。
3. **アクション種別6種の全フィールド**: `app.start`(bundle_id) / `app.stop`(bundle_id, grace_seconds) / `process.start`(process_id, executable, args, working_dir, env) / `process.stop`(process_id, grace_seconds) / `command.run`(executable, args, timeout_seconds, shell) / `notify`(title, message, level)。
4. **`browser.redirect` の明示拒否**: 検証エラー `unsupported_action` になることを明記する（別コンポーネントとして分離済み。静かに無視しない）。
5. **マッチ意味論**: `task_name_contains` は OR×OR、`task_name_not_contains` は AND-NOT。実行中タスクが0件のとき `contains` は偽・`not_contains` は真という非対称性を明記する。大文字小文字を区別しない部分文字列比較で、正規表現は使わない。
6. **検証エラーコード15種**: `unsupported_config_version`/`missing_required_field`/`unknown_field`/`unknown_action_type`/`unsupported_action`/`duplicate_rule_id`/`duplicate_process_id`/`value_out_of_range`/`timeout_exceeds_interval`/`unsupported_value`/`executable_not_found`/`invalid_identifier`/`shell_not_allowed`/`forbidden_safety_flag`/`rule_conflict_same_priority` の一覧と発生条件。
7. **権限違反とスキーマ違反の2段階分岐**: 権限違反（RPCError）は内容を1バイトも読まずに拒否する。スキーマ違反（errors[]）はパース後の内容検証。両者を明確に区別して記載する。
8. **適用ポリシー表**: 起動時の違反→`config_error`（空設定で動かない）／reload 時の違反→直前設定を維持／reload 成功→消滅したルールの `on_exit` を1回だけ発火。
9. **任意コード実行の明記（必須）**: 「`config.yml` を書き換えられる利用者は、実質的にこのアプリの権限で任意コードを実行できる」という性質を明記する。`command.run` と `shell: true` が任意コード実行経路であること、対策として所有者・権限検証と `allow_shell` 既定 false があることを記載する。

## Behavior Specification
対象外: 本 Process は文書作成でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（文書が満たすべき観測可能な条件）
- `rg -c '任意コード' docs/config-schema.md` が1件以上ヒットする。
- 検証エラーコード15種すべてに個別の発生条件記載がある。
- アクション種別6種すべてに全フィールドの記載がある。
- `browser.redirect` が `unsupported_action` になる旨が明記されている（静かに無視される旨の記載がない）。
- マッチ意味論の非対称性（実行中タスク0件時の `contains`/`not_contains` の真偽）が明記されている。
- 権限違反とスキーマ違反が別々の節・別々の扱いとして記載されている。

### Left to Implementation（文章表現の自由度）
- 全キー階層をYAML例で示すか表で示すかの選択。
- 検証エラーコード15種の並び順。
- 補足説明の分量。

## Red Phase
- [x] `test ! -f docs/config-schema.md` を実行し、文書が未作成であることを確認する
- [x] `rg -c '任意コード' docs/config-schema.md` を実行し、ファイル不在によりエラー終了することを確認する（`docs/config-schema.md` 作成済みのため本項目は **スキップ**）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（作成前の存在確認は正式ゲート化していない）/ status / command_or_action: `test ! -f docs/config-schema.md` / exit_code / expected: 0（未作成） / observed / attempt）

## Green Phase
- [x] Implementation Notes の9項目すべてを満たす `docs/config-schema.md` を作成する
- [x] 「任意コード」という語を含む節を必ず設ける
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P202-VG-01 / status / command_or_action: `rg -c '任意コード' docs/config-schema.md` / exit_code / expected: ヒット >= 1 / observed / attempt）

## Refactor Phase
- [x] D-15/D-14 の grep が期待ヒット数（いずれも0件）と一致することを確認する
- [x] 検証エラーコード15種・アクション種別6種の一覧と `internal/config/validate.go` の実装（存在する場合）を突き合わせ、記載漏れがないことをレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（禁止事項節記載の grep を Refactor Phase の証跡として使う）/ status / command_or_action: `rg -n 'golangci-lint|swiftlint|swift-format|xcodebuild' docs/config-schema.md` / exit_code / expected: ヒット0件 / observed / attempt）

## Manual Verification（最大3件）
1. **実装との照合（executor: human）**: 操作=`docs/config-schema.md` と `internal/config/validate.go`（存在する場合）を並べて読み、15種の検証エラーコードと6種のアクションフィールドが実装と齟齬なく一致するか確認する → 期待=齟齬0件 → データ状態の確認方法=目視比較の結果を記録

## Dependencies
- Requires: P102
- Blocks: P300
