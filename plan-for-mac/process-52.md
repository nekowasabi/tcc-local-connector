# Process 52: 安全ゲートの実行時再確認

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-52.md` を起動した際の自己完結ブリーフ。

- **背景**: P01（設定スキーマと検証）は `allow_shell`/`allow_force_terminate`/`allow_external_process_control` を起動時に一度だけ検証する。しかし「起動時に検証を通ったこと」と「実行の瞬間に安全側であること」は別の保証であり、テストからの直接呼び出しや将来追加される新しい呼び出し経路が P01 の検証をバイパスした場合、危険な操作（シェル実行・強制終了・外部プロセスへの操作）が漏れる可能性が構造的に残る。防御を検証フェーズという1層だけに賭けない。
- **目的**: `Engine.executeBackendActions` と `internal/ledger.Manager.Stop` の実行直前に、`allow_shell`/`allow_force_terminate`/`allow_external_process_control` を再判定する二重防御を実装する。設定検証を通過していても、実行直前の値が安全側でなければ操作を実行しない。
- **変更範囲**: `internal/engine/cycle.go`（`Engine.executeBackendActions` への実行時再確認分岐追加）、`internal/ledger/manage.go`（`Manager.Stop` への台帳照合・外部プロセス制御ガード追加）。対応するテストファイルへの追記。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | `DefaultProcessStopGraceSeconds` | 10 | seconds | `process.stop` の通常終了猶予。grace 経過後も SIGKILL 相当の経路へ進まないことを本 Process が保証する対象 |
  | `MinGraceSeconds` | 1 | seconds | grace 秒数の下限（検証自体は P01 の責務。本 Process は範囲内の値のみ受け取る） |
  | `MaxGraceSeconds` | 120 | seconds | grace 秒数の上限 |

- **禁止事項**: 該当する Don'ts のみ抜粋（本 Process のスコープに限定）。
  - D-01 強制終了の禁止 — `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal/engine internal/ledger` → **期待 0 件**（`BackendClient.hardKill` は Swift 側のみで例外扱い。Go 側には一切存在しない）
  - D-02 プロセス名だけによる終了の禁止 — `rg -n '\bpkill\b|\bkillall\b|runningApplications\(\)' internal/engine internal/ledger` → **期待 0 件**
  - D-03 シェル文字列連結の禁止 — `rg -n '"/bin/sh"|"-c"|bash -c|zsh -c|sh -c' internal/engine internal/ledger` → **期待 0 件**（本 Process のスコープに `allow_shell` ガードの実体はなく P06 側に既存。本 Process はガードの再確認のみを追加する）
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/engine internal/ledger` → **期待 0 件**（`*_test.go` は対象外）
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/engine internal/ledger` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: 本 Process は Commander's Intent の「完全な禁止ではなく継続的整合」「通常終了のみ」を実装の物理構造として保証する最終防波堤。検証フェーズ（P01）をバイパスする経路が将来生まれても、本 Process の再確認が残る限り危険操作は実行されない。
  - **error**: 実行時再確認で拒否した場合のエラーコードは `permission_denied`（P07 が定義する既存エラーコード enum の1つ）を使う。新規エラーコードを追加しない。
  - **互換性**: `internal/protocol` の RPC スキーマは変更しない。`internal/config` の検証ロジック（P01）も変更しない。本 Process は実行直前フックへの追加のみ。
  - **並行実行制約**: 本 Process は `internal/engine/cycle.go` を P50・P51 と共有するため、**W07 内で P50→P51→P52 の直列実行が必須**（Conflict Matrix 参照）。P51（dry_run 貫通）の完了後に着手する。
  - **該当 Don'ts**: D-01/D-02/D-03/D-09/D-12（上記）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

P01 は設定ファイルの読み込み時に `allow_shell`/`allow_force_terminate`/`allow_external_process_control` を含む全フィールドを検証し、違反があれば `*Config==nil` を返して起動そのものを止める。この検証は「起動時に1回」しか実行されない。本 Process はこれとは独立に、**実行の瞬間**（`Engine.executeBackendActions` がアクションを1件実行する直前、`Manager.Stop` がシグナルを送信する直前）に同じ3つのフラグを再判定する層を追加する。

`allow_shell==false` のとき、`shell:true` を要求するアクションは実行時再確認で `permission_denied` として拒否される。これは P06 が実装済みの「`shell:false` では `exec.Command(executable, args...)` を直接実行する」という既定動作には影響しない。既定動作（`shell:false`）はこの再確認を素通りし、`shell:true` かつ `allow_shell:false` という矛盾した状態（本来 P01 で弾かれているはずだが、何らかの経路でここまで到達した場合）だけが拒否される。

`allow_force_terminate` は**値に関わらず SIGKILL 相当のコードパスがコードベース上に存在しない**ことで保証する。これは実行時の分岐ではなく、そもそも「force_terminate が true のときに呼ばれる強制終了関数」という選択肢自体をコードに書かないという物理的な保証であり、D-01 の grep 監査（Go 側スコープで期待0件）がこれを常時照合する。

`allow_external_process_control` は**値に関わらず、台帳（`internal/ledger`）に存在しないプロセスへシグナルを送らない**ことで保証する。`Manager.Stop` は常に台帳の `Entry` と対象 PID を照合し、一致しない場合はフラグの値によらずシグナル送信を拒否する。

```
// Why: 「allow_external_process_control:true なら台帳外プロセスも操作可能にする」設計も
// 検討したが、PLAN-for-mac.md の Scope（対象外）に「他利用者のプロセス操作」の除外が
// 明記されている。台帳は自アプリが起動した（または追跡開始した）プロセスのみを保持する
// ため、台帳照合を外すとこの除外条件を破る経路が生まれる。フラグの意味を「台帳内
// プロセスへの操作可否」に限定し、台帳外への操作という選択肢自体を作らなかった。
```

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/engine/cycle.go` | 既存ファイルへの追記（`patch_only: true`） | `Engine.executeBackendActions` に `allow_shell` の実行時再確認分岐を追加 |
| `internal/ledger/manage.go` | 既存ファイルへの追記（`patch_only: true`） | `Manager.Stop` に台帳照合による `allow_external_process_control` ガードを追加 |
| `internal/engine/engine_test.go` | 追記 | `TestSafetyGate_*` 一式 |

## Symbol Targets

```yaml
file: internal/engine/cycle.go
symbols:
  - {name: Engine.executeBackendActions, kind: method, body_start_line: 1, body_end_line: 55, line_hint: 65}
patch_only: true
disjoint_guarantee: false
disjoint_guarantee_evidence: "internal/engine/cycle.go は P50（Logger 注入）・P51（dry_run 貫通）と同一ファイルを触る。Conflict Matrix（PLAN-for-mac.md）が本 Process を disjoint:false と記録。W07 は P50→P51→P52 の直列実行が必須であり、本 Process は P51 完了後に着手する。"
pre_flight_checks: [git_clean, go_test_ok]
---
file: internal/ledger/manage.go
symbols:
  - {name: Manager.Stop, kind: method, body_start_line: 1, body_end_line: 35, line_hint: 35}
patch_only: true
disjoint_guarantee: false
pre_flight_checks: [git_clean, go_test_ok]
```

### Notes
- `Manager.Stop` は P51 で既に `dryRun` 分岐を受け取っている。本 Process はその分岐の外側（dry_run でない通常実行時）に台帳照合ガードを追加する。P51 と P52 の分岐が重ならないよう、P52 は P51 完了後の `Manager.Stop` の実体に対して追記する（Conflict Matrix の直列実行制約と一致）。
- `allow_force_terminate` は本 Process 内で判定分岐を書くのではなく、「呼ぶべき強制終了関数がコードベースに存在しない」という不在によって保証する。したがって Symbol Targets に強制終了関数は現れない（存在しないことが正しい）。

## Verification Gates（P52）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P52-VG-01 | red | test | agent | true | `go test ./internal/engine -run 'TestSafetyGate' -v` | P52 の task_delta | exit != 0 | GoalEvidence（exit_code と `FAIL`/未定義シンボルを含む行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-RED, SCOPE-01, QUALITY-01 |
| P52-VG-02 | green | test | agent | true | `go test ./internal/engine -run 'TestSafetyGate' -v` | P52 の task_delta | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-GREEN, SCOPE-01, QUALITY-01 |
| P52-VG-03 | green | grep | agent | true | `rg -n 'forceTerminate\(\|SIGKILL\|signal\.SIGKILL\|syscall\.SIGKILL\|kill -9' internal/engine internal/ledger` | P52 の task_delta | ヒット == 0 | GoalEvidence（`rg` の exit code と件数0を逐語引用） | failure_class:grep, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | DONT-01, QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 52
- gate_ids: [P52-VG-01, P52-VG-02, P52-VG-03]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-52.appendix.md（実行時に Read しない）

## Implementation Notes

**注意**: 本 Process の差分が `MIN_PARALLEL_DIFF_LINES`(30行) 未満になる場合は P51 へ統合してよい（PLAN-for-mac.md の計画運用定数に基づく）。統合した場合はその旨を報告すること。

**なぜ検証（P01）を通ったことに依存せず実行直前にも再判定するか**:
```
// Why: 設定検証はプロセス起動時の1回きりの関門であり、テストからの直接呼び出しや
// 将来の新しい呼び出し経路がこの関門を素通りする可能性を排除できない。「起動時に
// 検証済みだから実行時は信頼してよい」という前提を置かず、実行の瞬間にも同じ3つの
// フラグを再判定することで、検証を回避するコードパスが生まれても危険操作が漏れない
// 構造にした。防御を1層に賭けない。
```

**なぜ `allow_force_terminate` を分岐でなく「経路の不在」で保証するか**: 分岐（`if allow_force_terminate { forceKill() }`）で実装すると、`forceKill()` という関数自体はコードベースに存在し続ける。将来のリファクタリングや別の呼び出し元がこの関数を誤って呼び出すリスクが残る。関数自体を書かないことで、リスクをゼロにする。これは「部分適用できない型にする」という Commander's Intent の Key Tasks の実装方針と一致する。

## Behavior Specification
System Type: reactive

| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-52-01 | 実行直前（設定検証済みの `Plan` を保持） | `command.run`（`shell:true`）アクションの実行判定 | 実行時再確認で `allow_shell==false` | 変化なし | `exec.Command`（シェル経由）を呼ばず `ActionResult{status:"failed", error_code:"permission_denied"}` を返す | TestSafetyGate_ShellDeniedAtRuntime |
| BEH-52-02 | 実行直前 | `command.run`（`shell:false`）アクションの実行判定 | `allow_shell` の値に関わらず（既定動作の回帰確認） | 変化なし | `exec.Command(executable, args...)` で直接実行される（P06 の既存動作を維持） | TestSafetyGate_NonShellActionUnaffected |
| BEH-52-03 | 実行直前 | `process.stop` アクションの実行判定 | 対象 PID が台帳（`managed-processes.json`）に存在しない | 変化なし | シグナル送信を行わず `ActionResult{status:"failed", error_code:"permission_denied"}` を返す（`allow_external_process_control` の値に関わらず） | TestSafetyGate_UnmanagedProcess_NoSignal |
| BEH-52-04 | 実行直前 | `process.stop` アクションの実行判定 | 対象 PID が台帳の `Entry` と一致（PID+lstart+args 照合、P04 の既存ロジック） | 変化なし | 通常終了シグナルが送信される（管理対象への正規操作は妨げない） | TestSafetyGate_ManagedProcess_SignalAllowed |
| BEH-52-05 | 実行直前 | `process.stop` の grace 期限超過（`DefaultProcessStopGraceSeconds` 経過後もプロセスが終了しない） | `allow_force_terminate` の値に関わらず | 変化なし | SIGKILL 相当のコードパスへ到達せず、`ActionResult{status:"failed", error_code:"timeout"}` として記録される | TestSafetyGate_ForceTerminateNeverInvoked |

### Correctness Criteria（観測可能・固定する）
- `allow_shell==false`（実行時再判定）のとき、`shell:true` を要求するアクションは `exec.Command` のシェル経由呼び出しを一切行わない。
- `shell:false` のアクションは `allow_shell` の値に関わらず常に `exec.Command(executable, args...)` で直接実行される（既存動作を壊さない）。
- 台帳に存在しない PID へは `allow_external_process_control` の値に関わらずシグナルが送信されない。
- `internal/engine`・`internal/ledger` 配下に SIGKILL 相当のコードパスが1件も存在しない（D-01 grep が期待0件で常時照合）。
- grace 期限を超過しても強制終了へエスカレーションせず、`timeout` として記録される。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- 実行時再確認のヘルパー関数名・配置（`cycle.go` 内のローカル関数か `internal/config` の既存検証関数を再利用するか）。
- `permission_denied` を返す際のログメッセージの内部フォーマット（`error_code` フィールドの値自体は固定）。
- 台帳照合のキャッシュ有無（毎回ファイル読み込みするか、`Manager` が保持するメモリ上の状態を使うか。P04 の既存実装方針に従う）。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（本 Process では上記3点のみが実装者の裁量）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/engine/engine_test.go` に `TestSafetyGate_ShellDeniedAtRuntime` 等（存在しない実行時再確認分岐を前提とする最小ケース）を追記
- [x] Behavior Specification 表の全5行に対応する test_ref を用意
- [x] テストを実行して失敗することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P52-VG-01 / status: / command_or_action: `go test ./internal/engine -run 'TestSafetyGate' -v` / exit_code: / expected: exit!=0 / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/engine/cycle.go` の `Engine.executeBackendActions` に `allow_shell` 実行時再確認分岐を追加
- [x] `internal/ledger/manage.go` の `Manager.Stop` に台帳照合ガードを追加
- [x] Behavior Specification 表の全5行に対応する test_ref のテストが存在し PASS することを確認
- [x] D-01 grep（`internal/engine internal/ledger`）が期待0件であることを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P52-VG-02 / status: / command_or_action: `go test ./internal/engine -run 'TestSafetyGate' -v` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P52-VG-03 / status: / command_or_action: `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal/engine internal/ledger` / exit_code: / expected: ヒット==0 / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `Engine.executeBackendActions` 内の実行時再確認とアクション種別分岐の重複を整理
- [x] D-01/D-02/D-03/D-09/D-12 grep を再実行しゼロヒットを確認
- [x] `Manager.Stop` の台帳照合ガードが P51 の dry_run 分岐と重複・矛盾していないことを再確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P52-VG-02 / status: / command_or_action: `go test ./internal/engine -run 'TestSafetyGate' -v` / exit_code: / expected: exit==0（Refactor 後も維持） / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: agent** — 操作: `allow_shell:false` の設定で `shell:true` を要求するアクションを含む plan を実行時再確認に通す → 期待される出力: `permission_denied` として拒否され、`exec.Command` のシェル経由呼び出しが発生しない → 確認方法: `TestSafetyGate_ShellDeniedAtRuntime` のテスト出力で `ActionResult.error_code` を確認
2. **executor: agent** — 操作: 台帳に存在しない PID を対象とする `process.stop` アクションを `allow_external_process_control:true` の設定で実行する → 期待される出力: シグナルが送信されず拒否される → 確認方法: `TestSafetyGate_UnmanagedProcess_NoSignal` のテスト出力を確認

## Dependencies
- Requires: P06
- Blocks: なし
