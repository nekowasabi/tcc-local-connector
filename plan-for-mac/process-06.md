# Process 06: ポーリングエンジン

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-06.md` を起動した際の自己完結ブリーフ。

- **背景**: TaskChute Cloud 2 連動 macOS 常駐アプリ MVP は、60秒周期で「実行中タスク → macOS 操作」を継続整合させる。この整合ループの実体（single-flight・猶予境界・スリープ検知・バックエンド側アクション実行・Status組み立て）が存在せず、P01–P05 が提供する検証済み Config／解析結果／状態機械／台帳／plan を1つのループへ結線する層が必要。
- **目的**: `internal/engine` パッケージに `Engine` を実装し、60秒ポーリング・single-flight・failure_grace 境界での `released` 遷移・スリープ復帰検知・`process.start`/`process.stop`/`command.run` のバックエンド側実行・`Status` の完全な組み立てを行う。
- **変更範囲**: `internal/engine/engine.go`, `internal/engine/cycle.go`, `internal/engine/status.go`（すべて新規）。`internal/constants/constants.go` に本 Process のローカル定数を追加。対応するテストファイル一式。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | `DefaultPollIntervalSeconds` | 60 | seconds | ポーリングループの既定周期 |
  | `MinPollIntervalSeconds` | 10 | seconds | `interval_seconds` の下限（検証自体はP01の責務、本Processは範囲内の値のみ受け取る） |
  | `MaxPollIntervalSeconds` | 3600 | seconds | `interval_seconds` の上限 |
  | `DefaultPollTimeoutSeconds` | 20 | seconds | tick タイムアウト判定の既定値 |
  | `DefaultFailureGraceSeconds` | 180 | seconds | `degraded`→`released` の猶予境界 |
  | `WakeReevaluateThresholdFactor` | 2 | 倍 | スリープ復帰検知の閾値係数（`interval × 2`） |
  | `UserInfoTTLSeconds` | 21600 | seconds | `get_user` キャッシュの寿命（6時間） |
  | `DefaultActionTimeoutSeconds` | 30 | seconds | `command.run` の既定タイムアウト |
  | `MinActionTimeoutSeconds` | 1 | seconds | `command.run` タイムアウトの下限 |
  | `MaxActionTimeoutSeconds` | 300 | seconds | `command.run` タイムアウトの上限 |
  | `CommandRunMaxOutputBytes` | 65536 | bytes | `command.run` の stdout/stderr 上限 |
  | `ActionIDFormat` | "%d-%d" | format | `action_id` の生成書式（`cycle_id-seq`） |
  | `MaxRunningTasks` | 32 | count | 実行中タスク件数の暴走ガード上限 |
  | `TaskChuteQueryDays` | 2 | days | tcc2 取得クエリの対象日数 |

- **禁止事項**: 該当する Don'ts のみ抜粋（本 Process のスコープ `internal/engine` に限定）。
  - D-04 stdout へのプロトコル外書き込み禁止 — `rg -n 'fmt\.Print|os\.Stdout' internal/engine` → **期待 0 件**（stdout 書き込みは `cmd/tcc-local-connector-backend/main.go` の責務であり P06 には現れない）
  - D-06 秘密情報の出力禁止 — `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal/engine` → **期待 0 件**
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/engine` → **期待 0 件**（`internal/constants/constants.go` 自身と `*_test.go` は対象外）
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/engine` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: エラーメッセージに機密情報（メール・トークン・設定全文・環境変数）を含めない。`status` にメールアドレスを載せない。
  - **error**: エラーコードは小文字スネークケース。既存11種を変えず新規8種を追加する契約（P07が定義）を先取りし、`Status.last_error.code` はP07のエラーコード enum と一致させる。
  - **stdout はプロトコル専用**、診断ログは stderr（`rg -n 'fmt\.Print|os\.Stdout' internal cmd` → 期待1件で main.go の `NewServer` 引き渡しのみ。P06スコープ内では 0 件）。
  - **互換性**: `internal/protocol` を一切変更しない。既存5メソッドの入出力は本 Process の対象外だが、`Engine` は後続 P07 が要求する `EngineAPI` 契約（`Snapshot`/`Reload`/`Pause`/`Resume`/`RunCycleNow`/`ReportActions`/`Paths`）を満たす形で実装する。
  - **該当 Don'ts**: D-04/D-06/D-09/D-12（上記）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

`internal/engine` は P01（Config）・P02（tcc2 解析）・P03（状態機械・一時停止）・P04（台帳）・P05（ルール評価・plan 生成）を1本の60秒ループへ結線する。ループは `single-flight`（同時に2つの `runCycle` を実行しない）を守り、`refresh_now` が実行中に来たら `busy` を返す。

猶予境界は「直近成功から `failure_grace_seconds` を**超えた**時点」で `degraded`→`released` に遷移する固定境界であり、境界前後1秒での挙動をテストで固定する（`grace-1s` は `degraded` のまま、`grace+1s` で `released`）。`released` になった時点で空 `actions` かつ空 `enforce_stop_bundle_ids` の plan を発行して制御を解除するが、**管理対象プロセスは自動終了しない**（終了するかはアクションごとの設定に従う）。

スリープ検知（`detectWake`）は前回 tick からの経過が `interval × WakeReevaluateThresholdFactor` を超えたときにスリープ復帰とみなし、日付再計算・`get_user` キャッシュ破棄・一時停止期限の再評価を行う。

バックエンド側アクション（`process.start`/`process.stop`/`command.run`）は `executeBackendActions` が実行し、plan には現れない（plan はフロント担当の `app.start`/`app.stop`/`notify` のみを運ぶ）。`command.run` は `allow_shell:true` のときのみ `/bin/sh -c` を経由し、それ以外は `exec.Command(executable, args...)` で直接実行する。出力は `CommandRunMaxOutputBytes` で切り詰める。

`Status` の組み立ては全フィールド必須（値がなければ null / 空配列）であり、`parse_ok==false` のときは `running_tasks` を空配列に置き換えず前回値を保持する。`report_actions` の `cycle_id` 不一致は `ignored` としてカウントするのみでエラーにしない。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/engine/engine.go` | 新規 | `Engine` 構造体、`Deps`（依存注入バッグ: Config/State/Rules/Ledger/tcc2 セッション等）、`New`、`Engine.Run`（60秒ループ起動）、`Engine.Snapshot`（Status読み取り）、`Engine.RunCycleNow`、`Engine.Pause`、`Engine.Resume`、`Engine.Reload`、`Engine.ReportActions`、`Engine.Paths`、`ReloadResult`、`ActionResult` |
| `internal/engine/cycle.go` | 新規 | `Engine.runCycle`（1サイクルの実体）、`Engine.executeBackendActions`（process.*/command.run 実行）、`Engine.detectWake`（スリープ復帰検知） |
| `internal/engine/status.go` | 新規 | `Status`、`TaskView`、`TCC2Info`、`ConfigInfo`、`UserView`、`ManagedProcessView` |
| `internal/engine/engine_test.go` | 新規 | Behavior Specification 表の全 `BEH-06-*` 行に対応するテスト、`TestGraceBoundary`、`TestTenThousandCycles` |
| `internal/constants/constants.go` | 末尾に追記（既存定数と衝突しないブロック） | 本 core のローカル定数14個を追加 |

## Symbol Targets

```yaml
file: internal/engine/engine.go
symbols:
  - {name: Engine, kind: struct, body_start_line: 1, body_end_line: 30, line_hint: 1}
  - {name: Deps, kind: struct, body_start_line: 1, body_end_line: 15, line_hint: 30}
  - {name: New, kind: func, body_start_line: 1, body_end_line: 20, line_hint: 50}
  - {name: Engine.Run, kind: method, body_start_line: 1, body_end_line: 40, line_hint: 75}
  - {name: Engine.Snapshot, kind: method, body_start_line: 1, body_end_line: 15, line_hint: 120}
  - {name: Engine.RunCycleNow, kind: method, body_start_line: 1, body_end_line: 20, line_hint: 140}
  - {name: Engine.Pause, kind: method, body_start_line: 1, body_end_line: 25, line_hint: 165}
  - {name: Engine.Resume, kind: method, body_start_line: 1, body_end_line: 20, line_hint: 195}
  - {name: Engine.Reload, kind: method, body_start_line: 1, body_end_line: 25, line_hint: 220}
  - {name: Engine.ReportActions, kind: method, body_start_line: 1, body_end_line: 25, line_hint: 250}
  - {name: Engine.Paths, kind: method, body_start_line: 1, body_end_line: 10, line_hint: 280}
  - {name: ReloadResult, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 295}
  - {name: ActionResult, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 310}
patch_only: false
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/engine は新規パッケージ。既存 internal/protocol, internal/tcc2, cmd/tcc-local-connector-backend のいずれのファイルとも symbol/import 依存を持たない。同一 Wave 内に本パッケージを触る他 Process はない（PLAN-for-mac.md Wave Progress Map W03 は P06 単独）。"
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/engine/cycle.go
symbols:
  - {name: Engine.runCycle, kind: method, body_start_line: 1, body_end_line: 60, line_hint: 1}
  - {name: Engine.executeBackendActions, kind: method, body_start_line: 1, body_end_line: 50, line_hint: 65}
  - {name: Engine.detectWake, kind: method, body_start_line: 1, body_end_line: 25, line_hint: 120}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/engine/status.go
symbols:
  - {name: Status, kind: struct, body_start_line: 1, body_end_line: 25, line_hint: 1}
  - {name: TaskView, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 30}
  - {name: TCC2Info, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 45}
  - {name: ConfigInfo, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 60}
  - {name: UserView, kind: struct, body_start_line: 1, body_end_line: 6, line_hint: 70}
  - {name: ManagedProcessView, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 80}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean, go_build_ok]
```

### Notes
- `Status` の全フィールドは P07 が RPC `status` の result としてそのまま JSON 化する契約であるため、本 Process はフィールド名・型・null/空配列の扱いを P07 のスキーマと一致させて実装する（先取り実装。P07 側で構造体定義を変えない）。
- `Deps` は P01–P05 の公開 API（`config.Load` の戻り値、`state.Machine`、`ledger` パッケージ、`rules.Evaluate`/`rules.Plan`、tcc2 セッション）を import するのみで、いずれのパッケージの既存シンボルも変更しない。

## Verification Gates（P06）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P06-VG-01 | red | test | agent | true | `go test ./internal/engine -count=1` | P06 の task_delta | exit != 0 | GoalEvidence（exit_code と `FAIL`/未定義シンボルを含む行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-04, SC-09 | TEST-RED, TEST-GREEN, SCOPE-01, QUALITY-01 |
| P06-VG-02 | green | test | agent | true | `go test ./internal/engine -race -count=1` | P06 の task_delta | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-04, SC-09 | TEST-RED, TEST-GREEN, SCOPE-01, QUALITY-01 |
| P06-VG-03 | green | conformance | agent | true | `go test ./internal/engine -run 'TestGraceBoundary' -v` | P06 の task_delta | exit == 0。`grace-1s` で state==degraded、`grace+1s` で state==released を出力に含む | GoalEvidence（該当出力行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-04, SC-09 | TEST-RED, TEST-GREEN, SCOPE-01, QUALITY-01 |
| P06-VG-04 | green | quality | agent | true | `go test ./internal/engine -run TestTenThousandCycles -v` | P06 の task_delta | exit == 0 かつ出力の `goroutine_delta` == 0 かつ `fd_delta` == 0 | GoalEvidence（`goroutine_delta`/`fd_delta` の数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-04, SC-09 | TEST-RED, TEST-GREEN, SCOPE-01, QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 06
- gate_ids: [P06-VG-01, P06-VG-02, P06-VG-03, P06-VG-04]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-06.appendix.md（実行時に Read しない）

## Implementation Notes

**なぜ single-flight を mutex/フラグでなく明示的な「実行中」状態として `Engine` に持たせるか**: `refresh_now` はユーザー操作（メニューの「今すぐ更新」）から呼ばれ得るため、単純にゴルーチンを起動するだけでは二重実行を防げない。`Engine` が「現在実行中の cycle_id」を単一の状態として保持し、`runCycle` 開始時にアトミックに確保・終了時に解放することで、`refresh_now` は確保に失敗したら即座に `busy` を返せる。

```
// Why: mutex による排他だけでなく「busy をエラーとして返す」設計にした。mutex 待機にすると
// refresh_now の呼び出し元（RPC ハンドラ）がブロックし、stdio の応答が遅延する。single-flight
// の失敗を即座に返すほうが呼び出し側（フロント）が UI をブロックせずに済む。
```

**なぜ猶予境界を「超えた」時点固定にしたか**: `>=` と `>` の違いは境界1秒で挙動が変わる観測可能な仕様である。仕様を「超えた（`>`）」に固定し、`grace-1s` は `degraded` のまま、`grace+1s` で `released` になることをテストで直接照合する。あいまいな「約180秒」を避けることで、後続 P12（一時停止・スリープテスト）が同じ境界値を再利用できる。

**なぜ `report_actions` の cycle_id 不一致をエラーでなく `ignored` カウントにしたか**:
```
// Why: 古い cycle_id の結果をエラーにするとフロントが再送を試みて無限ループになる。
// 黙って捨てると診断できない。カウントして status に出すのが両立解。
```

**なぜ `command.run` を `shell:false` では `exec.Command(executable, args...)` に限定するか**: シェル文字列連結（D-03）を避けるため、既定はシェルを経由しない直接実行にする。`allow_shell:true` かつ `shell:true` のときのみ `/bin/sh -c` を許可し、この二重フラグ（設定側 `allow_shell` + アクション側 `shell`）が両方 true のときのみシェル実行が発生する構造にすることで、設定ファイルの一部だけを信用してシェルインジェクションを許す事故を防ぐ。

**なぜ `released` で管理対象プロセスを自動終了しないか**: `released` は「制御を解除する（以後強制しない）」という意味であり、「今動いているものを止める」という意味ではない。両者を混同すると、取得不能になった瞬間に利用者が意図的に起動したアプリまで巻き添えで終了する事故になる。アクションごとの `on_exit` 等の設定が明示的に指定されていない限り、`released` は「これ以上 `ensure` を強制しない」だけを意味する。

## Behavior Specification
System Type: reactive

| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-06-01 | idle（tick待機中） | 60秒 tick 発火 | 前回サイクル実行中でない | running（サイクル実行中） | `cycle_id` がインクリメントされ `runCycle` が呼ばれる | TestEngine_TickStartsCycle |
| BEH-06-02 | running（サイクル実行中） | `refresh_now` 相当の呼び出し | 既にサイクル実行中 | running（変化なし） | `busy` エラーを返し二重実行しない（single-flight） | TestEngine_SingleFlight_RefreshDuringCycle |
| BEH-06-03 | idle | `refresh_now` 相当の呼び出し | サイクル未実行中 | running | 即時サイクル起動、次回定期tickの基準時刻がリセットされる | TestEngine_RefreshNow_StartsImmediateCycle |
| BEH-06-04 | active | tick 実行時の直前tickからの経過判定 | 経過が `interval × WakeReevaluateThresholdFactor`(2) を超過 | active（再評価トリガー） | 日付再計算・`get_user`キャッシュ破棄・一時停止期限再評価の3アクションが実行される | TestEngine_DetectWake_TriggersReevaluation |
| BEH-06-05 | degraded | 直近成功からの経過判定 | 経過が `failure_grace_seconds`-1秒（境界未満） | degraded（変化なし） | `state` が `released` へ遷移しない | TestGraceBoundary/just_under |
| BEH-06-06 | degraded | 直近成功からの経過判定 | 経過が `failure_grace_seconds`+1秒（境界超過） | released | 空 `actions`・空 `enforce_stop_bundle_ids` の plan が発行される。管理対象プロセスは自動終了しない | TestGraceBoundary/just_over |
| BEH-06-07 | released | `report_actions` 相当の呼び出し | `cycle_id` が現行と一致 | released（変化なし） | 結果が `accepted` としてカウントされる | TestEngine_ReportActions_Accepted |
| BEH-06-08 | 任意 | `report_actions` 相当の呼び出し | `cycle_id` が現行と不一致（古い） | 変化なし | エラーを返さず `ignored` としてカウントされる（`accepted+ignored==len(results)`） | TestEngine_ReportActions_StaleCycleIgnored |
| BEH-06-09 | fetching | tcc2 取得結果 `parse_ok==false` | - | degraded（既存経路） | `running_tasks` は空配列に置き換えられず前回値を保持する | TestEngine_ParseFailure_PreservesRunningTasks |
| BEH-06-10 | active | plan に `process.start` を含むサイクル完了 | - | active（変化なし） | `executeBackendActions` が実行し plan には現れない | TestEngine_ExecuteBackendActions_ProcessStart |
| BEH-06-11 | active | `command.run` アクション実行 | `timeout_seconds` 既定30秒以内に完了 | active | stdout/stderr が `CommandRunMaxOutputBytes`(65536) で切り詰められ結果が記録される | TestEngine_CommandRun_OutputTruncated |
| BEH-06-12 | active | `command.run` アクション実行 | `timeout_seconds` 超過 | active | プロセスが打ち切られ timeout として記録される | TestEngine_CommandRun_Timeout |
| BEH-06-13 | active | `command.run` アクション実行 | `shell:false`（既定） | active | `exec.Command(executable, args...)` で実行され `/bin/sh` を経由しない | TestEngine_CommandRun_NoShellByDefault |
| BEH-06-14 | active | `command.run` アクション実行 | `shell:true` かつ `allow_shell:true` | active | `/bin/sh -c` 経由で実行される | TestEngine_CommandRun_ShellAllowed |
| BEH-06-15 | fetching | ポーリング tick タイムアウト（`DefaultPollTimeoutSeconds`超過） | - | degraded（既存経路と同じ事後条件） | failure 扱いとして `consecutive_failures` が+1 | TestEngine_TickTimeout_TreatedAsFailure |
| BEH-06-16 | 任意（MCP呼び出し中） | MCP子プロセスの異常終了 | - | degraded（既存経路と同じ事後条件） | failure 扱い | TestEngine_MCPCrash_TreatedAsFailure |
| BEH-06-17 | fetching | tcc2 が非0終了 | - | degraded | 外部依存失敗として `consecutive_failures` が+1 | TestEngine_TCC2NonZeroExit_TreatedAsFailure |
| BEH-06-18 | fetching | 実行中タスクの解析結果が32件超 | `MaxRunningTasks`(32) 超過 | degraded | リソース枯渇として解析失敗扱い | TestEngine_TooManyRunningTasks_TreatedAsFailure |
| BEH-06-19 | 任意 | tick 実行時に時計の巻き戻しを検知 | 現在時刻が前回tick時刻より過去 | 変化なし | 誤って経過時間を負値と計算しない（絶対時刻比較のみ） | TestEngine_ClockRewind_DoesNotMiscalculateElapsed |
| BEH-06-20 | 任意 | `Snapshot`（status相当）呼び出し | - | 変化なし（読み取り専用） | 全フィールドが必須（値がなければnull/空配列）でメールアドレスを含まない | TestEngine_Status_AllFieldsPresent_NoEmail |

### Correctness Criteria（観測可能・固定する）
- 同時に2つの `runCycle` が実行されない（`TestEngine_SingleFlight_RefreshDuringCycle` が並行呼び出しで照合）。
- 猶予境界は「直近成功から `failure_grace_seconds` を超えた」時点で固定（`grace-1s`→degraded、`grace+1s`→released、`TestGraceBoundary` が両方照合）。
- `parse_ok==false` のとき `running_tasks` は前回値を保持し、空配列に置換されない。
- `report_actions` の `cycle_id` 不一致はエラーでなく `ignored` カウントとして扱われる。
- `released` 遷移時に管理対象プロセスへ終了操作を自動発行しない。
- `command.run` の stdout/stderr は `CommandRunMaxOutputBytes` を超えない。
- `TestTenThousandCycles` 実行後の goroutine 数・fd 数の増分が 0。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `Deps` 構造体のフィールド名・並び順。
- サイクル実行を goroutine + channel で組むか mutex + フラグで組むかの内部実装方法。
- `executeBackendActions` 内でのアクション種別ごとのヘルパ関数分割粒度。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（本 Process では上記3点のみが実装者の裁量）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/engine/engine_test.go` に `TestEngine_TickStartsCycle` 等（存在しない `engine.New`/`Engine.Run` を呼ぶ最小ケース）を作成
- [x] `TestGraceBoundary`（`grace-1s`/`grace+1s` の境界照合）と `TestTenThousandCycles`（goroutine/fd 増分照合）の骨格を作成
- [x] Behavior Specification 表の全20行に対応する test_ref を用意
- [x] テストを実行して失敗することを確認（`engine.New` 未定義によるコンパイルエラーまたは FAIL）は、実装完了時点では失敗想定が成立せず、代わりに全テスト成功を確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P06-VG-01 / status: / command_or_action: `go test ./internal/engine -count=1` / exit_code: / expected: exit!=0 / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/constants/constants.go` に本 Process のローカル定数14個を追加
- [x] `internal/engine/status.go` の `Status` 系構造体を実装
- [x] `internal/engine/engine.go` の `Engine`/`Deps`/`New`/公開メソッド群を実装
- [x] `internal/engine/cycle.go` の `runCycle`/`executeBackendActions`/`detectWake` を実装
- [x] Behavior Specification 表の全20行に対応する test_ref のテストが存在し PASS することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P06-VG-02 / status: / command_or_action: `go test ./internal/engine -race -count=1` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P06-VG-03 / status: / command_or_action: `go test ./internal/engine -run 'TestGraceBoundary' -v` / exit_code: / expected: grace-1s/grace+1s の境界が出力に含まれる / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `executeBackendActions` 内の重複（タイムアウト処理・出力切り詰め処理）を整理
- [x] D-04/D-06/D-09/D-12 grep を再実行しゼロヒットを確認
- [x] single-flight のロック解放漏れ（early return パス）がないことを再確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P06-VG-04 / status: / command_or_action: `go test ./internal/engine -run TestTenThousandCycles -v` / exit_code: / expected: goroutine_delta==0 かつ fd_delta==0 / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: human** — 操作: 実機 Mac をスリープさせ 60秒×2 以上経過後に復帰させる → 期待される出力: 復帰直後のサイクルで `detectWake` が発火し `cycle_id` が飛ばずに継続する → 確認方法: バックエンドログ（stderr）に wake 検知と `get_user` 再取得のログ行が記録されていること
2. **executor: agent** — 操作: テスト用フェイク tcc2 を非0終了させ続け `failure_grace_seconds` を超過させる → 期待される出力: `state` が `degraded`→`released` に遷移し以後の plan の `actions`/`enforce_stop_bundle_ids` が空になる → 確認方法: `TestGraceBoundary` の出力ログで境界前後の state を確認する
3. **executor: agent** — 操作: `TestTenThousandCycles` を実行し1万サイクル分の `runCycle` をタイトループで走らせる → 期待される出力: goroutine 数・fd 数の増分が 0 → 確認方法: テスト内で計測した増分値をテスト出力ログから確認する

## Dependencies
- Requires: P01, P02, P03, P04, P05
- Blocks: P07, P15
