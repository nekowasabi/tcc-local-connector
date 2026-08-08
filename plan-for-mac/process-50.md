# Process 50: 構造化ログと保持日数

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-50.md` を起動した際の自己完結ブリーフ。

- **背景**: P06（ポーリングエンジン）と P07（protocol 配線）は動作するが、いずれも診断出力を持たない。障害発生時（`degraded`/`released` 遷移、`command.run` タイムアウト、バックエンド再起動）を事後に追跡する手段が stderr への生テキストしかなく、機械可読な形式も保持ポリシーもない。加えて実測 F8/F10 により `get_user` の `Email` と `tcc2 status` の `Logged in as: <EMAIL>` / `Token expires at: ...` がバックエンドの出力経路に流れ得ることが判明しており、これらをログへ出さない保証が構造として存在しない。
- **目的**: `internal/logging` パッケージに 1 行 1 JSON の構造化 Logger を実装し、`internal/engine.Engine` と `internal/protocol.Server` へ注入する。メールアドレス・`Bearer` トークン・トークン有効期限文字列を出力前に必ず除去し、状態ディレクトリ配下の古いログを起動時に削除する。ログ処理自体の失敗でアプリを止めない。
- **変更範囲**: `internal/logging/logger.go`（新規）。`internal/engine/engine.go`（`Deps`/`Engine` へ `Logger` フィールド注入、既存シンボルへの追記のみ）。`internal/protocol/server.go`（`Server` へ `Logger` フィールド注入、既存シンボルへの追記のみ）。`internal/constants/constants.go` に本 Process のローカル定数を追加。対応するテストファイル一式。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | `DefaultLogLevel` | "info" | — | `Logger` の既定レベル。debug/info/warn/error のうち info 未満（debug）を抑制 |
  | `MinLogRetainDays` | 1 | days | `retain_days` の下限（検証自体は P01 の責務。本 Process は範囲内の値のみ受け取る） |
  | `DefaultLogRetainDays` | 14 | days | `retain_days` 未指定時の既定値 |
  | `MaxLogRetainDays` | 365 | days | `retain_days` の上限 |
  | `LogRedactPatterns` | email 形式, "Bearer ", "Token expires" | list | `redactSecrets` が出力前に必ず落とすパターン集合 |
  | `LogFileName` | "backend.log" | — | 状態ディレクトリ配下のログファイル名（フロント側が生成） |
  | `StateDirRelPath` | ".local/state/tcc-local-connector" | path | `$HOME` 相対。ローテーション対象ディレクトリ |

- **禁止事項**: 該当する Don'ts のみ抜粋（本 Process のスコープ `internal/logging` に限定。D-06 のみ計画全体の期待件数を参照する）。
  - D-04 stdout へのプロトコル外書き込み禁止 — `rg -n 'fmt\.Print|os\.Stdout|println\(' internal/logging` → **期待 0 件**（`Logger` は stderr 専用であり stdout に一切触れない）
  - D-06 秘密情報の出力禁止 — `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal macos/Sources` → **期待2件**（判定用と redact パターン定義のみ。値そのものを保持・出力しない）
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/logging` → **期待 0 件**（`internal/constants/constants.go` 自身と `*_test.go` は対象外）
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/logging` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: メールアドレス・`Bearer` トークン・`Token expires` を含む文字列は `redactSecrets` を経由しない出力経路を作らない。`status` や通知にもこれらを載せない（P07/P09 の責務だが `Logger` はその最終防波堤として動作する）。
  - **error**: ログの `error_code` フィールドは P07 が定義するエラーコード enum（小文字スネークケース）とそのまま一致させる。ログ独自のエラーコード体系を新設しない。
  - **stdout はプロトコル専用、診断ログは stderr**（`rg -n 'fmt\.Print|os\.Stdout' internal/logging` → 期待0件）。`Logger` はファイルを直接開かず、常に `io.Writer`（既定は `os.Stderr`）へ書く。
  - **互換性**: `internal/protocol` の RPC スキーマ・`internal/engine` の `Status` フィールドは一切変更しない。`Logger` は両パッケージの `Deps`/構造体へのフィールド追加のみで、既存の公開メソッドのシグネチャを変えない。
  - **フロント責務との分担**: Go バックエンドはログファイルを直接開かない。全診断を stderr へ出し、ファイル化（`backend.log`）と生成物としての保持は Swift フロント（P08 の `BackendClient`）が担当する。**フロントが stderr を継続的に drain しないとバックエンドが書き込みでブロックし停止したように見える**（P08 側の責務として明記済み）。
  - **該当 Don'ts**: D-04/D-06/D-09/D-12（上記）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

`internal/logging.Logger` は 1 行 1 JSON の構造化ログを `io.Writer`（既定 `os.Stderr`）へ書く最小の実装であり、ファイルオープン・fsync・ローテーションのタイミング制御は一切行わない。フィールドは `ts`（RFC3339 オフセット付き）/`level`/`component`/`event`/`rule_id`/`action_type`/`duration_ms`/`error_code`/`msg` の固定集合とし、レベルは debug/info/warn/error の4段階、既定 `DefaultLogLevel`("info") 未満（debug のみ）は出力されない。

`redactSecrets` は `msg` フィールドへ書き込む直前に必ず通過する関門であり、`LogRedactPatterns`（email 形式の正規表現 / `"Bearer "` / `"Token expires"`）に一致する部分文字列を出力前に置換する。呼び出し側が redact 済みでない生テキストを直接 `Log` に渡しても、`Logger` 内部で再度パターン照合するため、呼び出し元の実装漏れに依存しない二重防御になる。

**ローテーション（`Rotate`）は Go 側で完結する純粋なファイルシステム操作**として実装する。実運用でのログファイル生成そのもの（stderr の内容を `backend.log` へ書き出す動作）は Swift フロント（`BackendClient`）が担う一方、`retain_days` を超えた古いファイルの削除は、より頻繁に起動されるバックエンド（P08 の再起動バックオフにより繰り返し起動され得る）が自身の `Engine` 起動シーケンスの一部として `StateDirRelPath` 配下を走査し実行する。この分担により、Swift 側に新たなファイル走査ロジックを追加せずに済む。

```
// Why: ログファイルの「書く」責務と「古いものを消す」責務を分離した。
// 「書く」を Go に持たせると複数バックエンドプロセス（再起動後の新旧）が同一ファイルへ
// 並行書き込みしうる（見出しの重要な落とし穴）。「消す」は削除操作が冪等かつ
// 同時実行されても安全（os.Remove の二重呼び出しはエラーになるが無害）なため、
// 起動頻度の高い Go 側に置いても事故が起きない。
```

ログ処理自体が失敗する場合（`io.Writer` への書き込みエラー、`Rotate` 中のファイルシステムエラー）は、エラーを内部で握りつぶして stderr へのベストエフォート出力に降格し、**アプリケーションの継続を優先する**。ログはアプリの目的そのものではなく診断の補助であるため、ログ機構の不調がタスク整合ループを止めてはならない。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/logging/logger.go` | 新規 | `Logger` 構造体、`New`、`Logger.Log`、`Logger.Rotate`、`redactSecrets` |
| `internal/logging/logger_test.go` | 新規 | Behavior Specification 表の全 `BEH-50-*` 行に対応するテスト |
| `internal/engine/engine.go` | 追記（既存 `Deps`/`Engine` 構造体へのフィールド追加のみ） | `Deps.Logger` フィールド追加、状態遷移・アクション実行時のログ呼び出し追加 |
| `internal/protocol/server.go` | 追記（既存 `Server` 構造体へのフィールド追加のみ） | `Server.Logger` フィールド追加、RPC 処理時のログ呼び出し追加 |
| `internal/constants/constants.go` | 末尾に追記（既存定数と衝突しないブロック） | 本 Process のローカル定数7個を追加 |

## Symbol Targets

```yaml
file: internal/logging/logger.go
symbols:
  - {name: Logger, kind: struct, body_start_line: 1, body_end_line: 12, line_hint: 1}
  - {name: New, kind: func, body_start_line: 1, body_end_line: 20, line_hint: 15}
  - {name: Logger.Log, kind: method, body_start_line: 1, body_end_line: 30, line_hint: 40}
  - {name: Logger.Rotate, kind: method, body_start_line: 1, body_end_line: 25, line_hint: 75}
  - {name: redactSecrets, kind: func, body_start_line: 1, body_end_line: 15, line_hint: 105}
patch_only: false
disjoint_guarantee: false
disjoint_guarantee_evidence: "internal/logging/ は新規パッケージだが、Logger を internal/engine/engine.go と internal/protocol/server.go の既存 Deps/Server 構造体へ注入するため両ファイルを改変する。Conflict Matrix（PLAN-for-mac.md）が本 Process を disjoint:false と記録。W07（P50→P51→P52）は internal/engine/cycle.go の三重衝突により直列実行必須であり、P50 は engine.go への注入を P51/P52 より先に完了させる。"
pre_flight_checks: [git_clean, go_build_ok, go_test_ok]
---
file: internal/engine/engine.go
symbols:
  - {name: Deps, kind: struct, body_start_line: 1, body_end_line: 15, line_hint: 30}
  - {name: New, kind: func, body_start_line: 1, body_end_line: 20, line_hint: 50}
patch_only: true
disjoint_guarantee: false
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/protocol/server.go
symbols:
  - {name: Server, kind: struct, body_start_line: 1, body_end_line: 20, line_hint: 1}
  - {name: NewServer, kind: func, body_start_line: 1, body_end_line: 15, line_hint: 25}
patch_only: true
disjoint_guarantee: false
pre_flight_checks: [git_clean, go_build_ok]
```

### Notes
- `internal/engine/engine.go` と `internal/protocol/server.go` への変更は `patch_only: true`（既存シンボルへのフィールド追加とログ呼び出し行の挿入のみ）に限定する。`Engine.runCycle`/`Engine.executeBackendActions`（`internal/engine/cycle.go`）本体のロジック変更は本 Process のスコープ外であり、P51/P52 が担う。
- `Logger.Rotate` は `(dir string, retainDays int, now time.Time) error` のような純粋なファイルシステム走査関数として実装し、`internal/engine.New` から `Engine` 起動時に一度だけ呼び出す。呼び出し配線自体は `engine.go` の `patch_only: true` 範囲内（数行の追記）に収まる。

## Verification Gates（P50）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P50-VG-01 | red | test | agent | true | `go test ./internal/logging -count=1` | P50 の task_delta | exit != 0 | GoalEvidence（exit_code と `FAIL`/未定義シンボルを含む行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01 | TEST-RED, SCOPE-01, QUALITY-01 |
| P50-VG-02 | green | test | agent | true | `go test ./internal/logging -race -count=1` | P50 の task_delta | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01 | TEST-GREEN, SCOPE-01, QUALITY-01 |
| P50-VG-03 | green | grep | agent | true | `go test ./internal/engine -run TestEndToEnd 2>/tmp/e2e.log; rg -n '@[a-z0-9.-]+\.[a-z]{2,}\|Bearer\|Token expires' /tmp/e2e.log` | P50 の task_delta（E2E 出力全体） | ヒット == 0 | GoalEvidence（grep のヒット件数を逐語引用。0 件であれば `rg` の exit 1 とその旨を明記） | failure_class:grep, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01 | DONT-01, QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 50
- gate_ids: [P50-VG-01, P50-VG-02, P50-VG-03]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-50.appendix.md（実行時に Read しない）

## Implementation Notes

**なぜ `redactSecrets` を呼び出し元でなく `Logger.Log` 内部で強制するか**:
```
// Why: 呼び出し元（engine.go / server.go）に「redact してから渡す」運用を徹底させる
// 設計にすると、新しいログ呼び出しを追加するたびに redact 漏れのリスクが生まれる。
// Logger.Log の内部で必ず redactSecrets を通す構造にすることで、呼び出し元の実装漏れに
// 依存しない二重防御になる。
```

**なぜログ失敗をエラーとして呼び出し元に伝播させないか**: `Logger` は診断のための補助機構であり、アプリの主目的（60秒周期の整合ループ）はログの成否に依存してはならない。`io.Writer` への書き込みエラーや `Rotate` 中のファイルシステムエラーは `Logger` 内部で握りつぶし、可能な限り stderr への出力を継続する。エラーを伝播させて `Engine.runCycle` を失敗させる設計にすると、ディスク容量枯渇やパーミッション変更のような無関係な要因でタスク整合が止まるという本末転倒な事故になる。

**なぜ `Logger.Rotate` を Go 側の純粋なファイルシステム走査として実装したか（Swift 側に持たせなかったか）**: バックエンド（Go）は P08 の再起動バックオフにより Swift フロントより高頻度で起動され得る。起動のたびに `StateDirRelPath` 配下の古いファイルを削除する処理を挟んでも、削除操作自体が冪等（既に削除済みのファイルへの再アクセスはエラーにはなるが実害がない）であるため、複数回実行されても安全である。この設計により、Swift 側に新たなディレクトリ走査コードを追加せずに済み、`macos/Sources/` への変更なしに保持日数ポリシーを実装できる。

## Behavior Specification
System Type: both（`redactSecrets`/フォーマット整形は純関数の transformation、`Logger.Log` のレベルフィルタリングと `Rotate` の起動時削除は reactive）

### Transformation（redactSecrets / フォーマット整形）

| behavior_id | 入力 | 出力 | pre_state | post_state | invariants | test_ref |
|---|---|---|---|---|---|---|
| BEH-50-01 | `msg` にメールアドレス形式（`user@example.com` 相当）を含む文字列 | メールアドレス部分が除去された文字列 | - | - | `LogRedactPatterns` の email パターンに一致する部分は出力に含まれない | TestRedactSecrets_Email |
| BEH-50-02 | `msg` に `"Bearer xxxxx"` を含む文字列 | `"Bearer "` 以降が除去された文字列 | - | - | `Bearer` トークン文字列は出力に含まれない | TestRedactSecrets_Bearer |
| BEH-50-03 | `msg` に `"Token expires at: ..."` を含む文字列 | `"Token expires"` 以降が除去された文字列 | - | - | トークン有効期限情報は出力に含まれない | TestRedactSecrets_TokenExpires |
| BEH-50-04 | `Logger.Log` の1回の呼び出し引数一式 | 1行の JSON（末尾に改行1つ） | - | - | 出力は必ず1行で完結し、JSON として復号可能 | TestLog_SingleLineJSON |

### Reactive（レベルフィルタリング / 起動時ローテーション / 失敗時の降格）

| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-50-05 | Logger（level=info、既定） | `Log(level=debug, ...)` 呼び出し | level(debug) < 閾値(info) | 変化なし | `io.Writer` へ何も書き込まれない | TestLog_LevelFiltering_BelowThreshold |
| BEH-50-06 | Logger（level=info） | `Log(level=warn, ...)` 呼び出し | level(warn) >= 閾値(info) | 変化なし | 1行の JSON が `io.Writer` へ書き込まれる | TestLog_LevelFiltering_AtOrAboveThreshold |
| BEH-50-07 | 起動時（状態ディレクトリに複数の古いログファイルが存在） | `Rotate(dir, retainDays, now)` 呼び出し | ファイルの mtime が `now - retainDays日` より古い | 変化なし | 該当ファイルが削除される | TestRotate_DeletesOlderThanRetainDays |
| BEH-50-08 | 起動時 | `Rotate(dir, retainDays, now)` 呼び出し | ファイルの mtime が `retain_days` 日以内 | 変化なし | 該当ファイルは削除されず保持される | TestRotate_KeepsWithinRetainDays |
| BEH-50-09 | 正常（`io.Writer` への書き込み可能） | `io.Writer` への書き込みでエラー発生 | - | 変化なし | エラーは呼び出し元へ伝播せず、以後の `Log` 呼び出しは継続してベストエフォートで試行される | TestLog_WriteFailure_DoesNotPanic |
| BEH-50-10 | 起動時 | `Rotate` 実行中にファイル削除でエラー発生（権限不足等） | - | 変化なし | エラーは呼び出し元へ伝播せず `Rotate` は残りのファイルの走査を継続する | TestRotate_DeleteFailure_ContinuesRemaining |

### Correctness Criteria（観測可能・固定する）
- `level` が `Logger` の設定閾値未満のログ呼び出しは `io.Writer` へ一切書き込まれない（`TestLog_LevelFiltering_BelowThreshold`）。
- `msg` フィールドに `LogRedactPatterns` のいずれかに一致する部分文字列が含まれる場合、出力される JSON の `msg` にはその部分文字列が含まれない（`TestRedactSecrets_*` 3件）。
- `Rotate` は `now - retainDays日` を境界として、境界より古いファイルのみを削除し、境界以内のファイルは保持する。
- `Logger.Log`/`Logger.Rotate` はいずれもエラーを戻り値として返さない、または返してもアプリの継続を止めない形で呼び出し元が扱う（`TestLog_WriteFailure_DoesNotPanic`/`TestRotate_DeleteFailure_ContinuesRemaining`）。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- JSON のフィールド出力順序（`encoding/json` の構造体タグ順に従うか、カスタムマーシャラを書くか）。
- `redactSecrets` を正規表現1本にまとめるか、パターンごとに関数分割するかの内部実装方法。
- `Rotate` 内でのファイル一覧取得に `os.ReadDir` と `filepath.WalkDir` のどちらを使うか。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（本 Process では上記3点のみが実装者の裁量）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/logging/logger_test.go` に `TestLog_SingleLineJSON` 等（存在しない `logging.New`/`Logger.Log` を呼ぶ最小ケース）を作成
- [x] `TestRedactSecrets_Email`/`TestRedactSecrets_Bearer`/`TestRedactSecrets_TokenExpires` の骨格を作成
- [x] `TestRotate_DeletesOlderThanRetainDays`/`TestRotate_KeepsWithinRetainDays` の骨格を作成
- [x] Behavior Specification 表の全10行に対応する test_ref を用意
- [x] テストを実行して失敗することを確認（`logging.New` 未定義によるコンパイルエラーまたは FAIL）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P50-VG-01 / status: / command_or_action: `go test ./internal/logging -count=1` / exit_code: / expected: exit!=0 / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/constants/constants.go` に本 Process のローカル定数7個を追加
- [x] `internal/logging/logger.go` の `Logger`/`New`/`Logger.Log`/`Logger.Rotate`/`redactSecrets` を実装
- [x] `internal/engine/engine.go` の `Deps`/`Engine`/`New` へ `Logger` フィールドを注入
- [x] `internal/protocol/server.go` の `Server`/`NewServer` へ `Logger` フィールドを注入
- [x] Behavior Specification 表の全10行に対応する test_ref のテストが存在し PASS することを確認
- [x] `go test ./internal/engine -run TestEndToEnd` の出力にメールアドレス・`Bearer`・`Token expires` が含まれないことを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P50-VG-02 / status: / command_or_action: `go test ./internal/logging -race -count=1` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P50-VG-03 / status: / command_or_action: `go test ./internal/engine -run TestEndToEnd 2>/tmp/e2e.log; rg -n '@[a-z0-9.-]+\.[a-z]{2,}|Bearer|Token expires' /tmp/e2e.log` / exit_code: / expected: ヒット==0 / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `Logger.Log` 内の JSON マーシャリングとレベル判定の重複を整理
- [x] D-04/D-06/D-09/D-12 grep を再実行しゼロヒット（D-06 は期待2件）を確認
- [x] `Rotate` のエラー握りつぶし箇所に握りつぶし理由のコメントが1箇所以上あることを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P50-VG-03 / status: / command_or_action: `rg -n 'fmt\.Print|os\.Stdout|println\(' internal/logging` / exit_code: / expected: ヒット==0 / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: agent** — 操作: `retain_days=1` の設定で `backend.log` 相当のディレクトリに2日前・当日のダミーファイルを作成し `Rotate` を実行する → 期待される出力: 2日前のファイルのみ削除され当日のファイルは残る → 確認方法: `TestRotate_DeletesOlderThanRetainDays`/`TestRotate_KeepsWithinRetainDays` のテスト出力でファイル一覧を確認
2. **executor: agent** — 操作: `internal/engine -run TestEndToEnd` を実行しログ出力をキャプチャする → 期待される出力: 出力全体にメールアドレス形式・`Bearer`・`Token expires` の文字列が1件も含まれない → 確認方法: `rg` によるゼロヒット確認（P50-VG-03）

## Dependencies
- Requires: P06, P07
- Blocks: P51
