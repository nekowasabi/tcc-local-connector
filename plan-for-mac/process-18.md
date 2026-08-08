# Process 18: Go E2E テスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-18.md` を起動した際の自己完結ブリーフ。

- **背景**: P10〜P17 は各パッケージを個別にテストするが、実際のバックエンドバイナリを起動して stdio 経由で `ready`→`plan`→`report_actions`→`pause`→`resume` の一連のライフサイクルが実際に動くことを確認するテストは存在しない。擬似 `tcc2`（`testdata/fake-tcc2.sh`）を用いた End-to-End テストにより、単体テストでは検出できない配線ミス（Engine と protocol.Server の接続不整合など）を検出する。SC-01・SC-03・SC-05・SC-07 の統合的な裏付けとなる。
- **目的**: `internal/engine/e2e_test.go` で実バイナリを `Process` として起動し、stdio で JSON を往復させ、出力に5つのマーカー（`ready`/`plan`/`report_actions accepted`/`paused`/`resumed`）がすべて出現することを確認する。
- **変更範囲**: `internal/engine/e2e_test.go`（新規）と `internal/engine/testdata/fake-tcc2.sh`（新規）の作成。既存プロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:
  本 Process はテスト専用であり定数を直接参照しない。E2E テスト内のタイムアウト・ポーリング待機時間はテストデータとして直接記述し、D-09 の検査対象外（`*_test.go`）として扱う。`fake-tcc2.sh` は MCP 応答の固定文字列を返すのみで定数を参照しない。

- **禁止事項**:
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/engine --glob '!*_test.go' --glob '!testdata/**'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/engine/e2e_test.go internal/engine/testdata/fake-tcc2.sh` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない。既存の `internal/engine/engine.go` / `cycle.go` / `status.go` / `internal/protocol/server.go` / `cmd/tcc-local-connector-backend/main.go` に1行も差分を作らない。ただし本 Process が追加する `testdata/fake-tcc2.sh` は Go の `*_test.go` ではないシェルスクリプトであるため、`patch_only` は false とする（テストファイル以外の新規アーティファクトを追加するため）。
  - **トレーサビリティ**: `e2e_test.go` の `TestEndToEnd` が SC-01・SC-03・SC-05・SC-07 それぞれに対応するマーカーを出力することをテストファイル冒頭のコメントで対応づける。
  - **セキュリティ**: `fake-tcc2.sh` はテスト専用の固定応答スクリプトであり、実際の認証情報・ネットワークアクセスを一切含まない。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`TestEndToEnd` は本アプリの Go バックエンド全体（`internal/engine` が `internal/tcc2`・`internal/state`・`internal/rules`・`internal/ledger`・`internal/protocol` を束ねた実バイナリ）を `os/exec` で起動し、標準入出力経由で実際の JSON RPC メッセージを送受信する統合テストである。`internal/tcc2` の取得先は擬似 `tcc2` CLI（`testdata/fake-tcc2.sh`）に差し替え、MCP の `initialize`/`notifications/initialized`/`tools/list`/`tools/call get_taskchute`/`tools/call get_user` に実測フォーマット準拠の固定応答を返させる。これにより、単体テストでは検出できない「各パッケージ間の配線ミス」を検出する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/engine/e2e_test.go` | 新規 | `TestEndToEnd` |
| `internal/engine/testdata/fake-tcc2.sh` | 新規 | MCP プロトコルの固定応答を返す擬似 `tcc2` CLI |

## Symbol Targets
```yaml
file: internal/engine/e2e_test.go
symbols:
  - name: TestEndToEnd
    kind: func
    line_hint: top
patch_only: false
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/engine/e2e_test.go と internal/engine/testdata/fake-tcc2.sh はいずれも新規ファイルであり、既存の internal/engine/*.go・internal/protocol/server.go・cmd/.../main.go のいずれも変更しない。W06 内の並列相手 P16 は internal/protocol/server_test.go のみを触るため衝突しない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
---
file: internal/engine/testdata/fake-tcc2.sh
symbols:
  - name: fake-tcc2.sh
    kind: script
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P18-VG-01 | green | test | agent | true | `go test ./internal/engine -run TestEndToEnd -race -count=1 -v` | P18 task_delta | exit==0。出力に `ready` `plan` `report_actions accepted` `paused` `resumed` の5マーカーがすべて出現 | GoalEvidence（exit_code と5マーカーの出現箇所を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-03, SC-05, SC-07 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 18
- gate_ids: [P18-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（本 Process は参照のみ、転記なし）
- regeneration_required_when: P07 の RPC メソッド一覧・MCP プロトコルバージョン（`"2025-06-18"`）・実測フォーマット（F2/F3/F5/F6）・Verification Gates のいずれかが変更されたとき
- appendix: process-18.appendix.md（実行時に Read しない）

## Implementation Notes
- **`TestEndToEnd`**: `os/exec` でバックエンドの実バイナリ（`cmd/tcc-local-connector-backend`）をビルド・起動し、標準入出力を通じて実際の JSON RPC メッセージ（`ready` 受信 → `refresh_now` 相当の呼び出しで `plan` 生成を確認 → `report_actions` 送信 → `pause` 呼び出し → `resume` 呼び出し）を往復させる。出力ログに5つのマーカー文字列がすべて含まれることを確認する。
  // Why: 単体テストはパッケージ境界内の正しさしか保証しない。実バイナリを起動する E2E テストによって、`main.go` の配線・`internal/engine` と `internal/protocol` の接続・stdio のフレーミングが実際に機能することを、最終製品に近い形で検証する。
- **`fake-tcc2.sh`**: MCP の `initialize` / `notifications/initialized` / `tools/list` / `tools/call get_taskchute` / `tools/call get_user` に対し、実測フォーマット（RESEARCH.md F2〜F8）に準拠した固定応答を返すシェルスクリプト。`internal/tcc2.Open` が実行する `tcc2` 実行可能ファイルの代わりに、テスト時の `config.yml` 相当の `task_source.executable` としてこのスクリプトを指定する。
- **実行環境依存の回避**: `fake-tcc2.sh` は外部ネットワークアクセスを行わず、標準入出力のみで完結するため、CI 環境でも決定的に動作する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- 実バイナリを起動し、擬似 `tcc2` を経由して `ready` 応答を受信できる。
- `refresh_now` 相当の呼び出しにより `plan` が生成され、出力に含まれる。
- `report_actions` を送信すると `accepted` を含む応答が返る。
- `pause` を呼び出すと一時停止状態になり、`resume` を呼び出すと解除される。
- 上記5つの事象すべてが1回の `TestEndToEnd` 実行のログに出現する。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- テスト用の一時 `config.yml` の組み立て方（`t.TempDir()` の使用）
- stdio との JSON 往復を行うヘルパ関数名
- `fake-tcc2.sh` が読み取る引数解析の具体的な実装（`$1` の分岐か、環境変数か）

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P07 の green 状態（バックエンド全体が既に動作する）であるため、テスト追加前に `TestEndToEnd` を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P18-VG-01）と共有する。
- [x] `internal/engine/testdata/fake-tcc2.sh` を作成し、実行権限を付与する
- [x] `internal/engine/e2e_test.go` に `TestEndToEnd` を追加する

## Green Phase
- [x] `go test ./internal/engine -run TestEndToEnd -race -count=1 -v` を実行し、出力に5マーカーすべてが出現することを確認する
- [x] Behavior Specification の Correctness Criteria 全項目に対応する事象がログに出現することを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P18-VG-01 / status / command_or_action: `go test ./internal/engine -run TestEndToEnd -race -count=1 -v` / exit_code / expected: 0 かつ5マーカー全出現 / observed / attempt）

## Refactor Phase
- [x] D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] 既存の `internal/engine/*.go`（`e2e_test.go` を除く）・`internal/protocol/server.go`・`cmd/tcc-local-connector-backend/main.go` に差分が生じていないことを `git diff --stat` で確認する
- [x] `fake-tcc2.sh` が外部ネットワークアクセスや実認証情報を含まないことを目視確認する
- [x] 専用の Verification Gate はここには定義しない。上記の grep/確認結果は P18-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。

## Dependencies
- Requires: P07（`internal/protocol/server.go` と `cmd/tcc-local-connector-backend/main.go` の実装が green であること）。Wave Progress Map 上は W05（P10-15, P17）の完了も前提。
- Blocks: P50（Wave Progress Map W06→W07 の順序）
