# Process 102: 定数単一ソース化監査

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-102.md` を起動した際の自己完結ブリーフ。

- **背景**: PLAN-for-mac.md の ★ Constants は「唯一の正」であり、Go は `internal/constants/constants.go`、Swift は `macos/Sources/ConnectorCore/Constants.swift` に実体を置く二言語構成である。P01–P52 の各 Process は個別に D-09（マジックナンバー直書き禁止）等の Don'ts を自スコープ内で grep 照合しているが、計画全体を横断する Don'ts D-01〜D-15 の一括監査と、Go/Swift 間で同値を要求される定数（`DefaultFailureGraceSeconds`/`backendDownReleaseGraceSeconds` 等）の照合は、個別 Process のスコープでは行えない。
- **目的**: `scripts/forbidden-audit.sh` を新規実装し、Don'ts D-01〜D-15 をこの順で実行して1つでも期待ヒット数と不一致なら exit 1 にする。加えて `internal/constants` の未使用定数0（staticcheck の既存解析を再利用）と、Go/Swift 間で同値を要求する3組の定数（`backendDownReleaseGraceSeconds`/`pauseNextDayStartHour`/`supportedProtocolVersion` とその Go 対応値）の一致を照合する。
- **変更範囲**: `scripts/forbidden-audit.sh`（新規）。既存コードへの変更はなし。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | （本 Process 固有の新規ローカル定数なし） | — | — | D-01〜D-15 の grep パターンと期待ヒット数は PLAN-for-mac.md の Don'ts セクションを正本として本 core に転記する |

- **禁止事項**: 本 Process は D-01〜D-15 **すべて**を検査対象とする（他 Process のように部分抜粋ではない）。個々のパターン・期待件数は下記「scripts/forbidden-audit.sh の仕様」を正とする。

- **適用される横断方針（インライン展開）**:
  - **品質ゲート**: `scripts/forbidden-audit.sh` は `rg`（実測ツールチェーンに存在）のみを使い、`golangci-lint`/`swiftlint`/`swift-format` に依存しない（D-15 自己適用）。
  - **単一ソース原則**: 本 Process が生成する `scripts/forbidden-audit.sh` 自体が新たな正本を作ってはならない。grep パターンと期待件数は PLAN-for-mac.md の Don'ts セクションをそのまま転記し、数値の不一致が生じたら PLAN-for-mac.md 側を修正してから本 Process を再生成する。
  - **該当 Don'ts**: D-01〜D-15（全件。本 Process はこの監査そのものが成果物）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

`scripts/forbidden-audit.sh` は Don'ts D-01〜D-15 の grep 監査を1本のスクリプトに集約する。各項目は「検出 grep コマンド」と「期待ヒット数」の組で定義され、スクリプトはこの順で実行し、実ヒット数が期待件数と異なる項目が1つでもあれば exit 1 にする。各項目の実行結果（コマンド・実ヒット数・期待ヒット数・判定）を1行ずつ標準出力へ表示する。

D-09（マジックナンバー直書き禁止）は個別 Process（P06/P50/P51/P52 等）がそれぞれ自スコープ内で照合済みだが、本 Process は `internal/{engine,rules,state,ledger,config}` 全体を対象にした横断照合を行う（PLAN-for-mac.md の D-09 定義そのもの）。

`internal/constants` の未使用定数0は、独自の解析ロジックを新規実装せず、P100（Go 静的解析とレース）が既に実行する `staticcheck ./...` の未使用コード検出（U1000 系解析）を根拠として扱う。本 Process では `staticcheck ./internal/constants` を明示的に実行し、未使用定数に起因する指摘が0件であることを確認する。

Go/Swift 間で同値を要求する定数（`backendDownReleaseGraceSeconds == DefaultFailureGraceSeconds == 180` / `pauseNextDayStartHour == PauseNextDayStartHour == 5` / `supportedProtocolVersion == protocol.Version == 1`）は、2つの言語のソースファイルからそれぞれの数値リテラルを抽出し、比較する。これは「1つの概念が2つのファイルに実体を持つ」という二言語構成に固有のリスク（片方だけ値を変更し同期を忘れる）を防ぐための照合であり、★ Constants セクションの「他は定数名で参照」という単一ソース原則を、言語をまたいでも保証する。

```
// Why: Go と Swift という異なる言語ランタイムでは定数を1つのファイルに統合できない。
// 「概念としては1つの値」を2箇所に実体として持たざるを得ない構造上の制約に対し、
// コンパイル時の型システムでは検出できない不一致を、ビルド後の grep 照合で
// 機械的に検出する設計にした。
```

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `scripts/forbidden-audit.sh` | 新規 | D-01〜D-15 の grep 監査、`internal/constants` 未使用定数チェック、Go/Swift 定数同値照合を1本のスクリプトとして実装 |

## Symbol Targets

```yaml
file: scripts/forbidden-audit.sh
symbols:
  - {name: main, kind: script, body_start_line: 1, body_end_line: 120, line_hint: 1}
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "scripts/forbidden-audit.sh は新規ファイル。Progress Map（PLAN-for-mac.md）は P100–P102 を Disjoint 列 'n.a.'（読み取り専用の検証群）として記録しており、同一 Wave（W08）内の他 Process（P100/P101）とファイルが重複しない。"
pre_flight_checks: [git_clean, go_test_ok, swift_build_ok]
```

### Notes
- `scripts/forbidden-audit.sh` 自体は新規ファイルの実装だが、既存コードへの変更を一切伴わないため計画運用上は「読み取り専用の検証群」（W08）に分類される。

## scripts/forbidden-audit.sh の仕様

D-01〜D-15 の grep を**この順**で実行し、1つでも期待ヒット数と不一致なら exit 1。各項目の実行結果（コマンド・実ヒット数・期待ヒット数・判定）を1行ずつ出力する。

- D-01 強制終了禁止 → `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal cmd macos/Sources scripts` → **期待1件**（`BackendClient.hardKill` のみ）
- D-02 → `rg -n '\bpkill\b|\bkillall\b|runningApplications\(\)' internal cmd macos/Sources` → **0件**
- D-03 → `rg -n '"/bin/sh"|"-c"|bash -c|zsh -c|sh -c' internal cmd` → **1件**（`allow_shell` ガード内のみ）
- D-04 → `rg -n 'fmt\.Print|os\.Stdout|println\(' internal cmd` → **1件**
- D-05 → `rg -n 'wsl\.exe|GOOS=windows|go:build windows|NotifyIcon|PowerShell|wslpath' internal cmd macos/Sources scripts/make-app-bundle.sh scripts/dev-run.sh` → **0件**
- D-06 → `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal macos/Sources` → **2件**
- D-07 → `rg -n 'com\.tinyspeck|com\.amazon\.Lassen|/opt/homebrew|/Users/takets' internal cmd macos/Sources --glob '!**/testdata/**'` → **0件**
- D-08 → `rg -ni 'in-progress count|in_progress_count' internal cmd macos/Sources` → **0件**
- D-09 → `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/engine internal/rules internal/state internal/ledger internal/config` → **0件**（constants.go 自身と `*_test.go` は対象外）
- D-10 → `rg -n 'time\.Since|monotonic|remainingSeconds|elapsedSeconds|time\.Tick' internal/state` → **0件**
- D-11 → `pattern: n/a`。代替: `rg -c 'errors\s*\)\s*>\s*0' internal/config/config.go` が **1件以上**
- D-12 → `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal cmd macos/Sources scripts` → **0件**
- D-13 → `pattern: n/a`。代替: `rg -n 'retry.*[0-9]+\s*回|retries?\s*[:=]\s*[0-9]' PLAN-for-mac.md plan-for-mac/` が **0件**
- D-14 → `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' . --glob '!docs/**' --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **0件**
- D-15 → `rg -n 'golangci-lint|swiftlint|swift-format' . --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **0件**

加えて **Go / Swift 間で同値を要求する定数**を照合する: `backendDownReleaseGraceSeconds == DefaultFailureGraceSeconds == 180` / `pauseNextDayStartHour == PauseNextDayStartHour == 5` / `supportedProtocolVersion == protocol.Version == 1`。

`internal/constants` の未使用定数チェックは `staticcheck ./internal/constants` を実行し、未使用コードに起因する指摘が0件であることを確認する（独自の解析ロジックを新規実装しない）。

## Verification Gates（P102）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P102-VG-01 | green | grep | agent | true | `bash scripts/forbidden-audit.sh` | 全 task_delta | exit == 0（D-01..D-15 の各 rg が規定ヒット数と一致、Go/Swift 定数同値照合と `internal/constants` 未使用定数チェックも合格） | GoalEvidence（スクリプトの各行出力と最終 exit_code を逐語引用） | failure_class:grep, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | DONT-01,QUALITY-01 |
| P102-VG-02 | green | grep | agent | true | `rg -n '\b(60\|180\|30000\|65536\|1048576)\b' internal/engine internal/rules internal/state internal/ledger` | 全 task_delta | ヒット == 0 | GoalEvidence（`rg` の exit code と件数0を逐語引用） | failure_class:grep, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | DONT-01,QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 102
- gate_ids: [P102-VG-01, P102-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき（特に D-01〜D-15 の grep パターン・期待件数、Go/Swift 同値定数の3組が変わったとき）
- appendix: process-102.appendix.md（実行時に Read しない）

## Implementation Notes

**なぜ D-01〜D-15 を個別 Process の grep と別に、まとめて1本のスクリプトへ集約したか**: 個別 Process（P06/P50/P51/P52 等）は自スコープに限定した grep（例: P06 は `internal/engine` のみ）を実行するため、Process 単体では計画全体を横断する不変条件（例えば D-07 のバンドルID直書き禁止はリポジトリ全体が対象）を検証できない。1本のスクリプトに集約することで、W08（P100–P102）の時点でリポジトリ全体に対する最終的な機械照合を一度に行える。

**なぜ Go/Swift 定数の同値照合を新しい抽象化（設定ファイル等）で解決しなかったか**: 「Go と Swift で共通の定数定義ファイルを1つ持つ」という設計も検討したが、両言語のビルドシステム（`go build`/`swift build`）がそれぞれ独立したソースファイルを要求するため、共有ファイルを持たせるには生成ステップ（コード生成）を追加で導入する必要があり、投機的な複雑性になる。grep によるビルド後照合は追加のビルドステップを要さず、既存の2ファイル構成をそのまま維持できる最小の解決策である。

**なぜ「未使用定数0」を独自実装せず staticcheck に委譲したか**: staticcheck の U1000 系解析は既に未使用のパッケージレベル変数・定数を検出する機能を持っており、P100 が既にリポジトリ全体に対して実行している。同じ検査を本 Process で再実装すると、2つの独立した未使用コード検出ロジックが計画内に存在することになり、判定基準が食い違うリスクを生む。既存ツールの結果を再利用することで単一の判定基準を維持する。

## Behavior Specification
対象外: マジックナンバー・ハードコード値・未使用定数の grep 監査であり、`internal`/`cmd`/`macos` の外部挙動（RPC スキーマ・状態遷移・UI）を一切変更しないため。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `scripts/forbidden-audit.sh` が未実装の状態で `bash scripts/forbidden-audit.sh` を実行し、ファイル不在によるエラー（exit != 0）を確認（`scripts/forbidden-audit.sh` 実装済みのため本項目は **スキップ**）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P102-VG-01 / status: / command_or_action: `bash scripts/forbidden-audit.sh` / exit_code: / expected: exit!=0（ファイル未実装） / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `scripts/forbidden-audit.sh` に D-01〜D-15 の grep 監査をこの順で実装
- [x] Go/Swift 間で同値を要求する3組の定数照合を実装
- [x] `staticcheck ./internal/constants` による未使用定数チェックを組み込む
- [x] `bash scripts/forbidden-audit.sh` を実行し exit 0 を確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P102-VG-01 / status: / command_or_action: `bash scripts/forbidden-audit.sh` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P102-VG-02 / status: / command_or_action: `rg -n '\b(60|180|30000|65536|1048576)\b' internal/engine internal/rules internal/state internal/ledger` / exit_code: / expected: ヒット==0 / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `scripts/forbidden-audit.sh` の各 D-XX ブロックの出力フォーマットを統一（コマンド・実ヒット数・期待ヒット数・判定の4項目を1行に揃える）
- [x] D-15 grep をスクリプト自身に対しても再実行しゼロヒットを確認（自己適用）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P102-VG-01 / status: / command_or_action: `bash scripts/forbidden-audit.sh` / exit_code: / expected: exit==0（Refactor 後も維持） / observed: / attempt:

## Manual Verification
対象外: 自動テストで十分（`scripts/forbidden-audit.sh` の exit code のみで機械的に合否判定できるため、人間の確認を要する曖昧さが残らない）。

## Dependencies
- Requires: P100, P101
- Blocks: なし
