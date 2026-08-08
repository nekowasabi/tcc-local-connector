# Process 04: 管理対象プロセス台帳

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-04.md` を起動した際の自己完結ブリーフ。

- **背景**: バックエンドは `process.start` / `process.stop`（フロント担当の `app.*` とは別に、バックエンド自身が同一実行ホストで起動・停止するプロセス）を管理するが、PID は OS によって再利用されるため、単純な PID 記録では「別プロセスに誤ってシグナルを送る」事故が起こり得る。また強制終了（SIGKILL）はユーザーの作業データを壊すリスクがあるため、経路自体をコード上に存在させない方針を取る。
- **目的**: `internal/ledger` パッケージに `managed-processes.json` の永続化、`/bin/ps` を用いた PID 同一性検証（`lstart`+`args` 複合照合）、SIGTERM のみの停止手順、起動時 Reconcile を実装する。
- **変更範囲**: `internal/ledger/ledger.go`, `internal/ledger/verify.go`, `internal/ledger/manage.go` の新規作成。`internal/constants/constants.go` に本 Process のローカル定数を追加。対応するテストファイル一式。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| LedgerVersion | 1 | - | managed-processes.json の `version` フィールド。不一致ならファイル破棄（台帳空で開始） |
| PSExecutablePath | "/bin/ps" | 絶対パス | PID 同一性検証で実行するコマンドの実体パス（PATH 非依存で固定） |
| PSArgsFormat | `-p <pid> -o lstart=,args=` | フォーマット文字列 | `/bin/ps` に渡す引数の組み立て |
| DefaultProcessStopGraceSeconds | 10 | 秒 | SIGTERM 送信後、終了確認までの既定待機時間 |
| MinGraceSeconds | 1 | 秒 | grace_seconds の下限バリデーション |
| MaxGraceSeconds | 120 | 秒 | grace_seconds の上限バリデーション |
| StateFileMode | 0600 | ファイルモード | managed-processes.json の作成パーミッション |
| StateDirRelPath | ".local/state/tcc-local-connector" | 相対パス | 状態ファイルの配置ディレクトリ |
| LedgerFileName | "managed-processes.json" | ファイル名 | 台帳ファイル名 |
| DefaultActionTimeoutSeconds | 30 | 秒 | `Manager.Start`/`Manager.Stop` 全体のタイムアウト上限 |
| CommandRunMaxOutputBytes | 65536 | バイト | `/bin/ps` 出力の読み取り上限（暴走出力対策） |

- **禁止事項**:
  - D-01（強制終了禁止）: `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal cmd macos/Sources scripts` → 期待ヒット数1件（`BackendClient.hardKill` のみ）。`internal` 配下は0件であること
  - D-02（プロセス名だけによる終了禁止）: `rg -n '\bpkill\b|\bkillall\b' internal` → 期待ヒット数 0
  - D-09（マジックナンバー直書き禁止）: grace_seconds・タイムアウト・バイト上限は上表の定数を参照する
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/ledger` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **security**: `managed-processes.json` は `StateFileMode`(0600) で作成し、tmp + rename でアトミック書き込みする。壊れた JSON（パース失敗・`version` 不一致）は台帳を空として起動する。**タスク名は Entry に一切含めない**。
  - **error**: `StopResult.status` は `already_running` / `orphan_dropped` / `refused` のような小文字スネークケースの code とし、メッセージに実行パスの機密情報を含めない。
  - **命名規約**: 本 Process が導入する定数はすべて `internal/constants/constants.go` に集約する。
  - **stdout/stderr**: 本 Process のコードは stdout に書き込まない。`/bin/ps` 実行結果や診断は内部で処理し、外部に漏らさない。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) core に列挙した更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/ledger` パッケージは、バックエンドが自ら起動した外部プロセス（`process.start` で起動するもの）の台帳 `managed-processes.json` を管理する。中心となるのは PID 再利用対策の **PID 同一性検証**（`VerifyEntry`）で、`/bin/ps -p <pid> -o lstart=,args=` の出力と台帳に記録済みの `ps_lstart` / `ps_args` を**両方とも完全一致**させることでのみ「自分が起動したプロセス」と判定する。停止操作は SIGTERM のみを送り、SIGKILL への経路はコード上に一切存在させない。

## Affected Files（パス・行番号・変更内容）
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/ledger/ledger.go` | 新規全体 | `Entry` 構造体、`Ledger` 構造体、`Load`, `Ledger.Save`, `Ledger.Put`, `Ledger.Remove` |
| `internal/ledger/verify.go` | 新規全体 | `VerifyEntry`（PID同一性検証）、`psSnapshot`（`/bin/ps` 実行の内部ヘルパ） |
| `internal/ledger/manage.go` | 新規全体 | `Manager` 構造体、`NewManager`, `StartSpec`, `StopResult`, `Manager.Start`, `Manager.Stop`, `Manager.Reconcile` |
| `internal/ledger/verify_test.go` | 新規全体 | `VerifyEntry` の全分岐（存在しない/lstart不一致/args不一致/一致/ps実行失敗）のテスト |
| `internal/ledger/manage_test.go` | 新規全体 | `Manager.Start`/`Stop`/`Reconcile` の全分岐テスト。`TestStop_PIDReused` を含む |
| `internal/constants/constants.go` | 末尾に追記（既存定数と衝突しないブロックとして追加） | 本 core のローカル定数11個を追加 |

## Symbol Targets
```yaml
file: internal/ledger/ledger.go
symbols:
  - name: Entry
    kind: type
    line_hint: top
  - name: Ledger
    kind: type
    line_hint: top
  - name: Load
    kind: func
    line_hint: middle
  - name: Ledger.Save
    kind: method
    line_hint: middle
  - name: Ledger.Put
    kind: method
    line_hint: middle
  - name: Ledger.Remove
    kind: method
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
# 根拠: internal/ledger は新規パッケージであり、既存の internal/protocol, internal/tcc2, internal/state（Process 03）,
# internal/rules（Process 05）のいずれとも symbol / import 依存を持たない。
pre_flight_checks:
  - git_clean
  - go_build_ok
  - bin_exists:/bin/ps
---
file: internal/ledger/verify.go
symbols:
  - name: VerifyEntry
    kind: func
    line_hint: top
  - name: psSnapshot
    kind: func
    line_hint: middle
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
  - bin_exists:/bin/ps
---
file: internal/ledger/manage.go
symbols:
  - name: Manager
    kind: type
    line_hint: top
  - name: NewManager
    kind: func
    line_hint: top
  - name: StartSpec
    kind: type
    line_hint: top
  - name: StopResult
    kind: type
    line_hint: top
  - name: Manager.Start
    kind: method
    line_hint: middle
  - name: Manager.Stop
    kind: method
    line_hint: middle
  - name: Manager.Reconcile
    kind: method
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
  - bin_exists:/bin/ps
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P04-VG-01 | red | test | agent | true | `go test ./internal/ledger -count=1` | P04 task_delta | exit != 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-06 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P04-VG-02 | green | test | agent | true | `go test ./internal/ledger -race -count=1` | P04 task_delta | exit == 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-06 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P04-VG-03 | green | conformance | agent | true | `go test ./internal/ledger -run TestStop_PIDReused -v` | P04 task_delta | exit == 0 かつ出力に `signals_sent=0` を含む | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-06 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P04-VG-04 | refactor | grep | agent | true | `rg -n 'SIGKILL\|kill -9' internal/ledger/` | P04 task_delta | ヒット == 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-06 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 04
- gate_ids: [P04-VG-01, P04-VG-02, P04-VG-03, P04-VG-04]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-04.appendix.md（実行時に Read しない）

## Implementation Notes
- **`/bin/ps` を選ぶ理由**: `golang.org/x/sys` を追加せず `/bin/ps` を使う。
  // Why: go.mod は依存ゼロであり、PID 同一性検証のためだけに外部依存を1本増やすのは割に合わない。`lstart`＋`args` の複合照合で PID 再利用は事実上排除できる。
- **「自分のものではない」側に倒す安全設計**: `/bin/ps` の実行自体（`exec.Command` の起動失敗等）に失敗した場合は `(false, err)` を返し、呼び出し側はシグナルを送らない。
  // Why: 検証できない状態でシグナルを送ると、無関係なプロセスを誤って停止させるリスクがある。判定不能なら「操作しない」方向に倒すのが安全側。
- **停止の再試行はしない**: `Manager.Stop` が `status:"refused"` を返した場合、呼び出し側で自動リトライしない設計とする。
  // Why: grace_seconds 待っても終了しないプロセスに対して機械的に再試行を繰り返すのは SIGKILL への圧力になりやすく、D-01 の精神（強制終了禁止）に反する。人間の判断を挟む前提とする。
- **シェルを介さない起動**: `Manager.Start` は `exec.Command(executable, args...)` で直接起動し、シェル経由（`sh -c`）にしない。
  // Why: シェルインジェクションのリスクを構造的に排除するため。

## Behavior Specification
System Type: both

### Transformation（`VerifyEntry`: `(Entry, live ps snapshot) → (bool, error)` の純関数的検証）
| behavior_id | 入力 | 出力 | pre_state | post_state | invariants | test_ref |
|---|---|---|---|---|---|---|
| BEH-04-01 | 台帳の Entry（pid=P）。`/bin/ps -p P` がプロセス不在（非0終了 or 出力空） | `(false, nil)` | 台帳に Entry(P) が存在 | 呼び出し側が Entry(P) を削除し `notify{process_orphan_dropped}` | シグナルは送信されない | TestVerifyEntry_ProcessNotFound |
| BEH-04-02 | 台帳の Entry（`ps_lstart`=L1）。`/bin/ps` の `lstart` が L1 と不一致 | `(false, nil)` | - | 呼び出し側が Entry(P) を削除（シグナル送信なし） | PID 再利用時に他プロセスへシグナルを送らない | TestVerifyEntry_LstartMismatch |
| BEH-04-03 | 台帳の Entry（`ps_args`=A1）。`/bin/ps` の `args` が A1 と不一致 | `(false, nil)` | - | 呼び出し側が Entry(P) を削除（シグナル送信なし） | 同上 | TestVerifyEntry_ArgsMismatch |
| BEH-04-04 | 台帳の Entry。`/bin/ps` の `lstart` と `args` が両方とも記録値と完全一致 | `(true, nil)` | - | 呼び出し側は「自分が起動したプロセス」として扱う | - | TestVerifyEntry_ExactMatch |
| BEH-04-05 | `/bin/ps` コマンド自体の実行が失敗（バイナリ起動不可等） | `(false, err)` | - | 呼び出し側はシグナルを送らず「自分のものではない」側に倒す | 判定不能時は安全側（操作しない） | TestVerifyEntry_PsExecFailure_SafeSide |

### Reactive（`Manager.Start` / `Manager.Stop` / `Manager.Reconcile`）
| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-04-06 | 台帳に同一 `process_id` の Entry あり | `Manager.Start` 呼び出し | `VerifyEntry` が true | 変化なし（新規起動しない） | `StartResult` 相当として `already_running` を返す。二重起動しない | TestManagerStart_AlreadyRunning_NoDuplicate |
| BEH-04-07 | 台帳に同一 `process_id` の Entry なし、または `VerifyEntry` が false | `Manager.Start` 呼び出し | - | プロセス起動済み、台帳に新規 Entry 保存済み | `exec.Command` はシェルを介さない。起動直後に `/bin/ps` で `lstart`/`args` を取得し台帳へアトミック保存 | TestManagerStart_NewProcess_Recorded |
| BEH-04-08 | 台帳に Entry あり、`VerifyEntry` が false（PID再利用） | `Manager.Stop` 呼び出し | - | Entry 削除済み | シグナルは送信されない。`StopResult{status:"orphan_dropped"}` を返す | TestStop_PIDReused |
| BEH-04-09 | 台帳に Entry あり、`VerifyEntry` が true | `Manager.Stop` 呼び出し | grace_seconds 待機後の再 `VerifyEntry` が false（終了確認） | Entry 削除済み | SIGTERM のみ送信（SIGKILLなし）。台帳から削除される | TestManagerStop_GracefulTermination |
| BEH-04-10 | 台帳に Entry あり、`VerifyEntry` が true | `Manager.Stop` 呼び出し | grace_seconds 待機後の再 `VerifyEntry` が true（終了未確認） | Entry は台帳に残存 | `StopResult{status:"refused"}` を返し、自動再試行しない | TestManagerStop_Refused_NoRetry |
| BEH-04-11 | 台帳に複数 Entry（一部が VerifyEntry false） | `Manager.Reconcile` 呼び出し（起動時） | - | false だった Entry がすべて削除され保存済み | 削除件数が戻り値として返る | TestManagerReconcile_RemovesStaleEntries |

### Correctness Criteria（観測可能・固定する）
- `VerifyEntry` は `lstart` と `args` の両方が記録値と完全一致した場合にのみ `true` を返す。片方だけの一致では `true` にならない。
- `Manager.Stop` の SIGTERM 送信から grace_seconds 経過後の VerifyEntry まで、SIGKILL に相当する呼び出しがコード上のどの分岐にも存在しない（`TestStop_PIDReused` は signaler の呼び出し回数を数え `signals_sent=0` を出力することで PID 再利用時にシグナルが1回も送られないことを保証する）。
- `managed-processes.json` の `version` が `LedgerVersion` と不一致の場合、`Load` はエラーを返さず空の `Ledger` を返す。
- `Manager.Start` は同一 `process_id` かつ `VerifyEntry==true` のとき、必ず `already_running` を返し、新しい `exec.Command` を起動しない。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `psSnapshot` の内部での `/bin/ps` 出力パース方法（固定長パースか区切り文字パースか）
- `Manager` 内部での signaler（シグナル送信抽象）のインターフェース名・注入方法
- tmp ファイルの命名規則

## Red Phase
- [x] `internal/ledger/verify_test.go` に BEH-04-01〜05 のテストケースを実装し、実装がないため失敗することを確認する
- [x] `internal/ledger/manage_test.go` に BEH-04-06〜11（`TestStop_PIDReused` を含む）を実装する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P04-VG-01 / status / command_or_action: `go test ./internal/ledger -count=1` / exit_code / expected: != 0 / observed / attempt）

## Green Phase
- [x] `internal/ledger/ledger.go`, `verify.go`, `manage.go` を実装し、Behavior Specification 表の全行に test_ref のテストが存在し PASS することを確認する
- [x] `internal/constants/constants.go` に本 core のローカル定数を追加する
- [x] `TestStop_PIDReused` が実プロセスへ実シグナルを送らず、注入可能な signaler の呼び出し回数のみで `signals_sent=0` を検証していることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P04-VG-02 / status / command_or_action: `go test ./internal/ledger -race -count=1` / exit_code / expected: 0 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P04-VG-03 / status / command_or_action: `go test ./internal/ledger -run TestStop_PIDReused -v` / exit_code / expected: 0 かつ出力に signals_sent=0 / observed / attempt）

## Refactor Phase
- [x] D-01/D-02 grep（SIGKILL/kill -9/pkill/killall）がゼロヒットであることを確認する
- [x] D-12（TODO/FIXME）がゼロヒットであることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P04-VG-04 / status / command_or_action: `rg -n 'SIGKILL|kill -9' internal/ledger/` / exit_code / expected: ヒット0 / observed / attempt）

## Manual Verification
1. **executor: human** — 操作: バックエンドが起動した子プロセスを `kill -9` で強制終了させたあと、同じ PID が別プロセス（例: `sleep 999`）に再利用される状況を意図的に作る（テスト環境限定） → 期待される出力: バックエンドがその PID に対して SIGTERM 等のシグナルを送らない → 確認方法: バックエンドログに `orphan_dropped` が記録され、`managed-processes.json` から該当 Entry が削除されていること
2. **executor: agent** — 操作: `Manager.Stop` を呼び出し、対象プロセスが grace_seconds 内に終了しないケースを再現する → 期待される出力: `StopResult{status:"refused"}` が返り、自動再試行されない → 確認方法: `managed-processes.json` に Entry が残存していること、および呼び出しログに再試行の形跡がないこと

## Dependencies
- Requires: なし
- Blocks: P06, P13
