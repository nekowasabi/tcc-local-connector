# Process 201: docs/protocol-v1.md 新規

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-201.md` を起動した際の自己完結ブリーフ。

- **背景**: NDJSON RPC 契約（既存5メソッド + 新規7メソッド + イベント4種）は `PLAN-for-mac.md` 内で decision-complete として凍結されているが、実装者・レビュアーが参照する独立した契約文書が存在しない。`internal/protocol/server.go` の `ready.capabilities` は実装のソースオブトゥルースだが、それを**文書側から機械照合**する `TestCapabilitiesMatchDocs`（P16）が存在を前提としており、その入力となる `docs/protocol-v1.md` がまだない。
- **目的**: §1.3 の全契約（型・12メソッド・4イベント・`ready.capabilities` の15要素と確定順序・エラーコード19種）を、`TestCapabilitiesMatchDocs` がパースできる機械可読な形式で文書化する。
- **変更範囲**: `docs/protocol-v1.md`（新規作成のみ）。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `protocol.Version` | 1 | — | 「トランスポート」節でプロトコル版を明記する（server.go:19 と同値） |
| `protocol.DefaultMaxMessage` | 65536 | bytes | `message_too_large` エラーコードの発生条件として明記する |
| `protocol.DefaultDrainTimeout` | 5 | 秒 | トランスポート節の補足として明記する（該当する場合） |
| `sleep` の上限 | 30000 | ms | `sleep` メソッドのスキーマに `timeout` エラーとの関係で明記する |

- **禁止事項**:
  - D-15（未導入 linter の記載禁止）: `rg -n 'golangci-lint|swiftlint|swift-format' docs/protocol-v1.md` → 期待 0 件
  - D-14（`.xcodeproj`/`xcodebuild` 等の記載禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' docs/protocol-v1.md` → 期待 0 件
  - D-08（`in-progress count` への依存禁止）: `rg -ni 'in-progress count|in_progress_count' docs/protocol-v1.md` → 期待 0 件（本文書はプロトコル契約のみを扱い tcc2 の生テキストには触れない）

- **適用される横断方針（インライン展開）**:
  - **機械照合可能性**: `ready.capabilities` の15要素と確定順序は `TestCapabilitiesMatchDocs`（P16）がこの文書をパースしてメソッド名集合を抽出し、実装の `ready.capabilities` と完全一致を照合する。見出しレベルまたはリスト形式を一定にし、パーサが安定して抽出できる形式にする。
  - **双方向 RPC の禁止**: stdin は Request のみ、stdout は Response または Event のみと明記する。stdout はプロトコル専用、診断は stderr という原則を崩さない。
  - **識別規則**: Event と Response の判別は `event` キーの有無のみで行い、`id` の有無や値には依存しないことを明記する（`writeError` が `id:""` を omitempty で落とすため、`id` の有無で判別すると誤判定する）。
  - **フロント担当アクションの限定**: `plan.actions[].kind` はフロント担当の `app.start` / `app.stop` / `notify` の3種のみであり、`process.start`/`process.stop`/`command.run` はバックエンド自身が実行し plan に現れないことを明記する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`docs/protocol-v1.md` をリポジトリ直下の `docs/` に新規作成する。対象読者は「Go バックエンドまたは Swift フロントを実装・レビューする開発者」であり、NDJSON トランスポート、4つの型（`Request`/`Response`/`Event`/`RPCError`）、Event/Response の識別規則、12メソッドと4イベントの完全スキーマ、`ready.capabilities` の15要素と確定順序、エラーコード19種、`plan.actions[].kind` の3種限定、空 plan の意味を記載する。この文書は `TestCapabilitiesMatchDocs`（P16）の入力として機械的にパースされるため、メソッド名の列挙形式を一貫させる。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `docs/protocol-v1.md` | 新規全体 | トランスポート・型・識別規則・12メソッド・4イベント・capabilities順序・エラーコード19種・plan.actions種別・空planの意味の9節 |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: docs/protocol-v1.md
symbols:
  - name: protocol-v1-document
    kind: document
    line_hint: top
  - name: capabilities-list
    kind: document-section
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - symbol_exists:internal/protocol/server.go:Serve
```

**Notes**:
- `capabilities-list` は `TestCapabilitiesMatchDocs`（P16）が実際にパースする節であり、メソッド名の列挙順序・表記ゆれに特に注意する。
- `docs/protocol-v1.md` は P201 で唯一の対象ファイルであり、他 Process とファイルレベルで衝突しない。

## Verification Gates（P201）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P201-VG-01 | green | conformance | agent | true | `go test ./internal/protocol -run TestCapabilitiesMatchDocs` | P201 task_delta | exit == 0 | GoalEvidence | failure_class:conformance, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-08 | SCOPE-01,QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 201
- gate_ids: [P201-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: RPC契約（メソッド・イベント・エラーコード・capabilities順序）を変更したとき、または `TestCapabilitiesMatchDocs`（P16）のパース仕様を変更したとき
- appendix: process-201.appendix.md（実行時に Read しない）

## Implementation Notes
以下9項目を記載する。
1. **トランスポート**: 改行区切り JSON（NDJSON）。stdin は Request のみ、stdout は Response または Event のみ。双方向 RPC にしない。stdout はプロトコル専用、診断は stderr。`protocol.Version = 1` 据え置き。
2. **型**: `Request{version,id,method,params?}` / `Response{version,id?,result?,error?}` / `Event{version,event,data?}` / `RPCError{code,message}` の完全な型定義。
3. **識別規則**: `event` キーの有無で Event と Response を分ける（`id` の有無に依存しない）ことを明示的に記載する。
4. **メソッド12種の完全スキーマ**: 既存5（`health`/`echo`/`sleep`(上限30000ms)/`cancel`/`tcc2_probe`）+ 新規7（`status`/`reload_config`/`pause`/`resume`/`refresh_now`/`config_paths`/`report_actions`）それぞれについて、フィールド名・型・必須/任意・既定値・post_state・発生しうる `RPCError` を記載する。
5. **イベント4種の完全スキーマ**: `ready` / `plan` / `state_changed` / `notify` それぞれのフィールド構成。
6. **`ready.capabilities` の15要素と確定順序**: health, echo, sleep, cancel, tcc2_probe, status, reload_config, pause, resume, refresh_now, config_paths, report_actions, event.plan, event.state_changed, event.notify の順で列挙する（機械照合対象。順序を変えない）。
7. **エラーコード全19種**: 既存分類（`message_too_large`/`invalid_json`/`unsupported_version`/`invalid_request`/`duplicate_id`/`not_found`/`invalid_params`/`cancelled`/`timeout`/`unsupported`/`tcc2_error`/`method_not_found`）+ 新規8（`config_not_found`/`insecure_permissions`/`io_error`/`busy`/`paused`/`invalid_state`/`internal_error`/`config_error`）を一覧化する。実装（`internal/protocol/server.go`／`internal/protocol/params.go`）の実際の定義数と齟齬がないか Refactor Phase で突き合わせる。
8. **`plan.actions[].kind` の3種限定**: フロント担当は `app.start`/`app.stop`/`notify` のみ。`process.start`/`process.stop`/`command.run` はバックエンド自身が実行し plan に現れないことを明記する。
9. **空 plan の意味**: `actions` と `enforce_stop_bundle_ids` がともに空 = 制御解除であることを明記する。

**重要**: `TestCapabilitiesMatchDocs`（P16）がこの文書をパースしてメソッド名集合を抽出し `ready.capabilities` と完全一致を照合するため、メソッド名を一定の見出しレベルまたはリスト形式で列挙する（Left to Implementation にせず、機械可読形式そのものを Correctness Criteria とする）。

## Behavior Specification
対象外: 本 Process は文書作成でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（文書が満たすべき観測可能な条件）
- `go test ./internal/protocol -run TestCapabilitiesMatchDocs` が exit 0 で終了する（文書側のメソッド名集合と `ready.capabilities` が完全一致する）。
- 12メソッド・4イベント・19エラーコードのすべてに個別のスキーマ記載がある（欠落がない）。
- `plan.actions[].kind` がフロント担当3種（`app.start`/`app.stop`/`notify`）のみとして明記され、`process.start`/`process.stop`/`command.run` はバックエンド自身が実行する旨が明記されている。
- Event/Response の識別規則が `event` キーの有無であることが明記されている。

### Left to Implementation（文章表現の自由度）
- 各メソッド・イベントのスキーマをテーブルで書くかコードブロックで書くかの選択（ただし `ready.capabilities` の列挙形式のみは機械照合のため固定する）。
- エラーコード一覧の並び順（機能グループ順かアルファベット順か）。
- 説明文の詳細さ・補足コメントの分量。

## Red Phase
- [x] `test ! -f docs/protocol-v1.md` を実行し、文書が未作成であることを確認する
- [x] `go test ./internal/protocol -run TestCapabilitiesMatchDocs` を実行し、文書不在（または未整備）によりテストが失敗することを確認する（P16 実装済み前提。`docs/protocol-v1.md` 作成済みのため本項目は **スキップ**）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（作成前の存在確認は正式ゲート化していない）/ status / command_or_action: `test ! -f docs/protocol-v1.md` / exit_code / expected: 0（未作成） / observed / attempt）

## Green Phase
- [x] Implementation Notes の9項目すべてを満たす `docs/protocol-v1.md` を作成する
- [x] `ready.capabilities` の15要素を確定順序で列挙する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P201-VG-01 / status / command_or_action: `go test ./internal/protocol -run TestCapabilitiesMatchDocs` / exit_code / expected: 0 / observed / attempt）

## Refactor Phase
- [x] D-08（`in-progress count` への依存禁止）の grep（`rg -ni 'in-progress count|in_progress_count' docs/protocol-v1.md`）が期待ヒット数（0件）と一致することを確認する
- [x] 12メソッド・4イベント・19エラーコードの一覧と実装（server.go）を突き合わせ、記載漏れがないことをレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（禁止事項節記載の grep を Refactor Phase の証跡として使う）/ status / command_or_action: `rg -ni 'in-progress count|in_progress_count' docs/protocol-v1.md` / exit_code / expected: ヒット0件 / observed / attempt）

## Manual Verification（最大3件）
1. **実装との照合（executor: human）**: 操作=`docs/protocol-v1.md` と `internal/protocol/server.go`/`internal/protocol/params.go` を並べて読み、12メソッド・4イベント・19エラーコードの記載が実装と齟齬なく一致するか確認する → 期待=齟齬0件 → データ状態の確認方法=目視比較の結果を記録

## Dependencies
- Requires: P102
- Blocks: P300
