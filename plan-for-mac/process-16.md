# Process 16: RPC 契約テスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-16.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/protocol/server.go`（P07）は既存5メソッドを無改変のまま維持しつつ、新規7メソッド（`status`/`reload_config`/`pause`/`resume`/`refresh_now`/`config_paths`/`report_actions`）を追加する。`ready.capabilities` の15要素・順序・`docs/protocol-v1.md` との一致は Swift 側（P08/P09）が信頼する契約の正本であり、ここが崩れると Swift 側が `backendIncompatible` を誤判定するか、逆に非互換なバックエンドと通信し続けるリスクがある。SC-01・SC-08 を専用のテスト強化 Process として固定する。
- **目的**: `internal/protocol/server_test.go`（既存ファイル）へ新規7メソッドの result 形状・capabilities 15要素の順序込み一致・docs との集合一致・エラー分岐のテストを**追記**する。
- **変更範囲**: `internal/protocol/server_test.go` への追記のみ。**既存ケースを1行も変更しない**。`internal/protocol/server.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `protocol.Version` | 1 | - | `TestVersionMismatch_UnsupportedVersion` で不一致値を投入する基準 |
| `protocol.DefaultMaxMessage` | 65536 | bytes | `TestMessageTooLarge` で64KiB超の入力を作る基準 |

- **禁止事項**:
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/protocol --glob '!*_test.go'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/protocol/server_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/protocol/server.go` / `params.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P07）の全 `BEH-07-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **既存ケース無改変**: `internal/protocol/server_test.go` の既存テストケースを1行も変更・削除しない。追記のみ行う。この制約は `P16-VG-01` の `git diff` 照合と `FINAL-VG-02` の両方で機械的に検証される。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/protocol` は Go バックエンドと Swift フロントエンドを繋ぐ stdio RPC の唯一の契約であり、この契約が壊れると即座に「起動しない」「操作を受け付けない」というフロントエンド全体の機能不全に直結する。本 Process は、新規7メソッドの result 形状、`ready.capabilities` の15要素が順序込みで一致すること、`docs/protocol-v1.md` に記載されたメソッド集合が `ready.capabilities` と完全一致すること（`TestCapabilitiesMatchDocs`）、そして version 不一致・重複 ID・メッセージサイズ超過・不正 JSON といった防御的なエラー処理が正しく機能することを固定する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/protocol/server_test.go` | 既存末尾に追記（既存行は一切変更しない） | 新規7メソッドの result 形状テスト、`TestReadyCapabilities`、`TestCapabilitiesMatchDocs`、version 不一致・重複ID・メッセージサイズ超過・不正JSON・pause paramsのエラー分岐テスト、`report_actions` の accepted+ignored 整合性テスト |

## Symbol Targets
```yaml
file: internal/protocol/server_test.go
symbols:
  - name: TestHandle_Status_ResultShape
    kind: func
    line_hint: bottom
  - name: TestHandle_ReloadConfig_ResultShape
    kind: func
    line_hint: bottom
  - name: TestHandle_Pause_ResultShape
    kind: func
    line_hint: bottom
  - name: TestHandle_Resume_ResultShape
    kind: func
    line_hint: bottom
  - name: TestHandle_RefreshNow_ResultShape
    kind: func
    line_hint: bottom
  - name: TestHandle_ConfigPaths_ResultShape
    kind: func
    line_hint: bottom
  - name: TestHandle_ReportActions_ResultShape
    kind: func
    line_hint: bottom
  - name: TestReadyCapabilities
    kind: func
    line_hint: bottom
  - name: TestCapabilitiesMatchDocs
    kind: func
    line_hint: bottom
  - name: TestVersionMismatch_UnsupportedVersion
    kind: func
    line_hint: bottom
  - name: TestDuplicateID_DuplicateIDError
    kind: func
    line_hint: bottom
  - name: TestMessageTooLarge_RecoversAfterError
    kind: func
    line_hint: bottom
  - name: TestInvalidJSON_RecoversAfterError
    kind: func
    line_hint: bottom
  - name: TestPause_BothParamsSpecified_InvalidParams
    kind: func
    line_hint: bottom
  - name: TestPause_BothParamsMissing_InvalidParams
    kind: func
    line_hint: bottom
  - name: TestReportActions_AcceptedPlusIgnoredEqualsResults
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: false
disjoint_guarantee_evidence: "既存ファイル internal/protocol/server_test.go への追記であり、P07（W04）が確立した既存テストケースの保護が最優先事項のため disjoint_guarantee は false とする。追記のみであることは git diff の削除行0件で機械照合する（P16-VG-01 / FINAL-VG-02）。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/protocol/server_test.go"
  - "file_exists:docs/protocol-v1.md"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P16-VG-01 | green | test | agent | true | `go test ./internal/protocol -race -count=10` | P16 task_delta | exit==0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-GREEN, SCOPE-01, QUALITY-01 |
| P16-VG-02 | green | conformance | agent | true | `go test ./internal/protocol -run TestCapabilitiesMatchDocs -v` | P16 task_delta | exit==0（docs のメソッド集合 == ready.capabilities） | GoalEvidence（PASS 行とメソッド集合の一致結果を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。
> **重要**: 既存ケースを1行も変更しないことは `git diff -- internal/protocol/server_test.go | rg '^-' | rg -v '^---'` の出力が空であることで照合する（`P07-VG-03` および `FINAL-VG-02` と同一の照合方法）。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 16
- gate_ids: [P16-VG-01, P16-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（`protocol.Version`, `protocol.DefaultMaxMessage`）
- regeneration_required_when: P07 の RPC メソッド一覧・`ready.capabilities` の15要素・`docs/protocol-v1.md` の内容・Verification Gates のいずれかが変更されたとき
- appendix: process-16.appendix.md（実行時に Read しない）

## Implementation Notes
- **新規7メソッドの result 形状テスト**: `status`/`reload_config`/`pause`/`resume`/`refresh_now`/`config_paths`/`report_actions` それぞれについて、正常系呼び出しの result JSON が期待するフィールド構成であることを確認する。
- **`TestReadyCapabilities`**: `ready.capabilities` が15要素で**順序込み**一致することを確認する。
  // Why: capabilities の順序は Swift 側の `requiredCapabilities` 判定に影響しうるため、集合一致だけでなく順序も契約として固定する。
- **`TestCapabilitiesMatchDocs`**: `docs/protocol-v1.md` をパースしてメソッド名集合を抽出し、`ready.capabilities` と完全一致することを確認する。これは `W09-IG-01` でも再照合される契約の正本テストである。
- **`TestVersionMismatch_UnsupportedVersion`**: version 不一致のリクエストが `unsupported_version` エラーになることを確認する。
- **`TestDuplicateID_DuplicateIDError`**: 重複した ID を持つリクエストが `duplicate_id` エラーになることを確認する。
- **`TestMessageTooLarge_RecoversAfterError`**: `protocol.DefaultMaxMessage`(64KiB) を超えるメッセージが `message_too_large` エラーになり、かつ**接続がその後も復帰して次のリクエストを処理できる**ことを確認する。
- **`TestInvalidJSON_RecoversAfterError`**: 不正な JSON が `invalid_json` エラーになり、同様に復帰することを確認する。
- **`TestPause_BothParamsSpecified_InvalidParams`** / **`TestPause_BothParamsMissing_InvalidParams`**: `pause` の params が「両方指定」または「両方欠落」のとき `invalid_params` になることを確認する（排他的必須パラメータの検証）。
- **`TestReportActions_AcceptedPlusIgnoredEqualsResults`**: `report_actions` の `accepted + ignored == len(results)` が常に成立することを確認する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- 新規7メソッド（`status`/`reload_config`/`pause`/`resume`/`refresh_now`/`config_paths`/`report_actions`）はそれぞれ期待する result 形状を返す。
- `ready.capabilities` は15要素で、順序を含めて固定された契約と一致する。
- `docs/protocol-v1.md` に記載されたメソッド名集合は `ready.capabilities` と完全一致する。
- version 不一致は `unsupported_version`、重複 ID は `duplicate_id`、64KiB 超のメッセージは `message_too_large`、不正 JSON は `invalid_json` になり、いずれもエラー後に接続は復帰する。
- `pause` の params は「両方指定」「両方欠落」のいずれも `invalid_params` になる。
- `report_actions` は常に `accepted + ignored == len(results)` を満たす。
- `internal/protocol/server_test.go` の既存テストケースは1行も変更・削除されない（追記のみ）。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `docs/protocol-v1.md` をパースしてメソッド名集合を抽出する具体的な正規表現・パース手順
- テスト用の stdio モック接続の構築方法（既存テストの流儀に従う）
- result 形状テストのアサーションヘルパ関数名

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P07 の green 状態であるため、テスト追加前に該当テスト名を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P16-VG-01/02）と共有する。
- [x] `internal/protocol/server_test.go` の**末尾に**16個のテスト関数を追記する（既存行の変更・削除は一切行わない）
- [x] `docs/protocol-v1.md` が存在し、7メソッドの記述があることを事前確認する（`file_exists` pre_flight_check の対象）

## Green Phase
- [x] `go test ./internal/protocol -race -count=10` を実行し、追加した全テストケースと既存テストケースの両方が10回連続で PASS することを確認する
- [x] `go test ./internal/protocol -run TestCapabilitiesMatchDocs -v` を実行し、`docs/protocol-v1.md` のメソッド集合と `ready.capabilities` が完全一致することを確認する
- [x] `git diff -- internal/protocol/server_test.go | rg '^-' | rg -v '^---'` の出力が空であることを確認する（既存ケース無改変の照合）
- [x] Behavior Specification の Correctness Criteria 全項目に対応するテストが存在し PASS することを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P16-VG-01 / status / command_or_action: `go test ./internal/protocol -race -count=10` / exit_code / expected: 0 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P16-VG-02 / status / command_or_action: `go test ./internal/protocol -run TestCapabilitiesMatchDocs -v` / exit_code / expected: 0 かつメソッド集合一致 / observed / attempt）

## Refactor Phase
- [x] D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `internal/protocol/server.go` / `params.go` の差分を確認し、実装差分が期待範囲内であることを `git diff --stat` で検証する
- [x] `git diff -- internal/protocol/server_test.go | rg '^-' | rg -v '^---'` を再確認し、既存ケースが無改変であることを最終確認する
- [x] 専用の Verification Gate はここには定義しない。上記の grep/diff 結果は P16-VG-01/02 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P07（`internal/protocol/server.go` の実装が green であり、`docs/protocol-v1.md` が存在すること）。Wave Progress Map 上は W05（P10-15, P17）の完了も前提。
- Blocks: P50（Wave Progress Map W06→W07 の順序）
