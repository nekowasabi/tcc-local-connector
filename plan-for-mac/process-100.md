# Process 100: Go 静的解析とレース

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-100.md` を起動した際の自己完結ブリーフ。

- **背景**: P01–P52 の実装が Go 側のコンパイル・単体テストレベルでは緑になっていても、静的解析（未使用変数・不到達コード・型の誤用等）とデータ競合（`-race`）は個別テストでは検出しきれない。実測ツールチェーンには `staticcheck` が導入済みであり `go vet` は Go 本体に同梱されるため、両方をリポジトリ全体に対して実行しない理由がない。`golangci-lint` は実測 F12 で未導入のため使用できない。
- **目的**: `go vet ./...` / `staticcheck ./...` / `go test ./... -race -count=1` / `go test ./... -race -count=10` の4コマンドをすべて exit 0 で通し、静的な健全性とデータ競合の不在（レース検出器を10回反復で通す統計的信頼性）を確認する。
- **変更範囲**: なし（読み取り専用の検証 Process）。指摘が見つかった場合の修正は当該コードを所有する Process（P01–P52 のいずれか）の担当範囲に差し戻し、本 Process 自体はコードを変更しない。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | `LINTER_TIMEOUT_SEC` | 120 | seconds | `go vet`/`staticcheck` の1回あたりの上限（計画運用定数。実行ハーネス向けでコードには現れない） |

- **禁止事項**: 該当する Don'ts のみ抜粋。
  - D-15 未導入 linter の記載禁止 — `rg -n 'golangci-lint|swiftlint|swift-format' . --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **期待 0 件**（実測 F12 で未導入。本 Process の検証コマンド自体もこの3ツールを使わない）

- **適用される横断方針（インライン展開）**:
  - **品質ゲート**: `go vet`/`staticcheck`/`go test -race` はいずれも実測ツールチェーンに存在するコマンドのみを使う。存在しない `golangci-lint` を検証手順に書けば計画が実行不能になる（PLAN-for-mac.md Scope の対象外項目）。
  - **修正責務の分離**: 本 Process で指摘が見つかった場合、修正は指摘箇所を所有する Process の Affected Files の範囲内で行う。本 Process 自体は「検証のみ」であり、ついでに無関係なコードを直すことはしない（surgical changes 原則）。
  - **該当 Don'ts**: D-15（上記）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

本 Process は新規コードを書かない。P01–P52 が実装したコードベース全体（`internal/...`、`cmd/...`）に対して、実測済みツールチェーンの静的解析コマンドを実行し、いずれも exit 0 であることを確認する検証専用の Process である。

`go vet ./...` は Go 標準の静的解析（フォーマット文字列の誤用、到達不能コード、構造体タグの誤りなど）を検出する。`staticcheck ./...` はより広範な静的解析（未使用コード、非効率なパターン、既知のバグパターン）を検出する。`go test ./... -race -count=1` は通常の単体テストにレース検出器を有効化して実行し、`go test ./... -race -count=10` は同じテストを10回反復することで、低頻度でしか顕在化しないデータ競合（特に P06 の single-flight や P50 のログ出力の並行性）を統計的信頼性をもって検出する。

指摘が見つかった場合、本 Process は自らコードを修正せず、該当箇所を所有する Process（Affected Files で該当ファイルを宣言している Process）の担当範囲へ差し戻す。これは「ついでに直す」ことで無関係な Process の Affected Files の境界を超えないための surgical changes 原則の適用である。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| （なし） | 読み取り専用 | 本 Process はファイルを変更しない。検証対象は `internal/...`/`cmd/...` 配下の全 Go ファイル（P01–P52 が生成済み） |

## Symbol Targets

```yaml
# n/a（読み取り専用の検証 Process）。新規・変更対象のシンボルは存在しない。
```

### Notes
- `disjoint_guarantee: n/a`（読み取り専用のため他 Process との衝突が構造的に発生しない）。
- `patch_only: true` は「本 Process がコードへパッチを当てない」ことを意味する（他 Process の `patch_only` とは異なる用法。読み取り専用の確認）。

## Verification Gates（P100）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P100-VG-01 | green | lint | agent | true | `go vet ./...` | 全 task_delta（Go 側） | exit == 0 | GoalEvidence（exit_code を逐語引用。非0の場合は指摘行を逐語引用） | failure_class:lint, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | QUALITY-01 |
| P100-VG-02 | green | lint | agent | true | `staticcheck ./...` | 全 task_delta（Go 側） | exit == 0 | GoalEvidence（exit_code を逐語引用。非0の場合は指摘行を逐語引用） | failure_class:lint, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | QUALITY-01 |
| P100-VG-03 | green | test | agent | true | `go test ./... -race -count=1` | 全 task_delta（Go 側） | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | QUALITY-01 |
| P100-VG-04 | green | test | agent | true | `go test ./... -race -count=10` | 全 task_delta（Go 側） | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 100
- gate_ids: [P100-VG-01, P100-VG-02, P100-VG-03, P100-VG-04]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-100.appendix.md（実行時に Read しない）

## Implementation Notes

**なぜ `-count=10` を追加で要求するか**: `-race` は実行時にしか競合を検出できない検出器であり、タイミング依存で低頻度にしか顕在化しない競合（特に P06 の single-flight 実装や P50 の `Logger` の並行書き込み）は1回の実行では見逃され得る。10回反復することで、実行順序のばらつきによって競合が顕在化する確率を実務上十分な水準まで引き上げる。固定回数（10）は ★ Constants ではなくコマンドライン引数として PLAN-for-mac.md の Verification Gates に既に現れている既存の慣例（`WXX-IGXX` の `-count=10`）を踏襲する。

**なぜ `golangci-lint` を使わないか**: 実測 F12 でこの環境に導入されていないことが確認済みであり、存在しないコマンドを検証手順に書くと計画そのものが実行不能になる（PLAN-for-mac.md Scope の対象外項目、D-15 で機械照合）。`go vet` と `staticcheck` の組み合わせで実測ツールチェーン内で完結する静的解析を行う。

## Behavior Specification
対象外: リポジトリ全体の静的性質（型の健全性・データ競合の不在）の検証であり、`internal`/`cmd` パッケージの外部挙動（RPC スキーマ・状態遷移・ファイル形式）を一切変更しないため。

## Red Phase: テスト作成と失敗確認
対象外: 本 Process は新規コードもテストも書かない読み取り専用の検証 Process であり、Red（意図的な失敗）を作る対象が存在しない。P01–P52 の各 Process がそれぞれの Red Phase を既に完了している前提で本 Process は実行される。

## Green Phase: 最小実装と成功確認
- [x] `go vet ./...` を実行し exit 0 を確認
- [x] `staticcheck ./...` を実行し exit 0 を確認
- [x] `go test ./... -race -count=1` を実行し exit 0 を確認
- [x] `go test ./... -race -count=10` を実行し exit 0 を確認
- [x] いずれかが非0の場合、指摘箇所を所有する Process の Affected Files を特定し差し戻す（本 Process では修正しない）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P100-VG-01 / status: / command_or_action: `go vet ./...` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P100-VG-02 / status: / command_or_action: `staticcheck ./...` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P100-VG-03 / status: / command_or_action: `go test ./... -race -count=1` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P100-VG-04 / status: / command_or_action: `go test ./... -race -count=10` / exit_code: / expected: exit==0 / observed: / attempt:

## Refactor Phase: 品質改善
対象外: 本 Process はコードを変更しないため、リファクタリング対象が存在しない。D-15 grep の再実行のみが本フェーズに相当する。

✅ **Phase Complete**（GoalEvidence）
- gate_id: P100-VG-01 / status: / command_or_action: `rg -n 'golangci-lint|swiftlint|swift-format' . --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` / exit_code: / expected: ヒット==0 / observed: / attempt:

## Manual Verification
対象外: 自動テストで十分（4コマンドすべてが機械的に exit 0/非0 を判定できるため、人間の確認を要する曖昧さが残らない）。

## Dependencies
- Requires: P07, P18
- Blocks: なし
