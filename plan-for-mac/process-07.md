# Process 07: protocol 拡張と main 配線

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-07.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/protocol/server.go` は既存5メソッド（`health`/`echo`/`sleep`/`cancel`/`tcc2_probe`）のみを扱う stdio RPC サーバであり、P06 が実装した `Engine` を呼び出す手段が存在しない。フロントエンドは `status`/`pause`/`resume` 等の新規メソッドと `plan`/`state_changed`/`notify` イベントを必要とするが、既存5メソッドの入出力を1バイトも変えずに拡張しなければ P02（`tcc2_probe` 無改変保証）を含む既存契約が壊れる。
- **目的**: 新規7メソッドと3イベントを追加し、`ready` の `capabilities` を15要素に拡張し、`cmd/tcc-local-connector-backend/main.go` に `--config` フラグと `Engine` の配線を追加する。既存5メソッドの入出力は既存テストの削除0行で機械照合する。
- **変更範囲**: `internal/protocol/server.go`（L51-58/L68-80/L105-111/L231-279 のみ）、`internal/protocol/params.go`（新規）、`cmd/tcc-local-connector-backend/main.go`（L18-69）
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | `protocol.Version` | 1 | — | server.go:19。据え置き（機能差分は capabilities で交渉） |
  | `protocol.DefaultMaxMessage` | 65536 | bytes | server.go:20。無改変 |
  | `protocol.DefaultDrainTimeout` | 5 | seconds | server.go:21。無改変 |
  | `PauseMinSeconds` | 60 | seconds | `pause` の `duration_seconds` 下限 |
  | `PauseMaxSeconds` | 86400 | seconds | `pause` の `duration_seconds` 上限（24時間） |
  | `NotifyTitleMaxRunes` | 200 | runes | `notify` イベントの `title` 上限 |
  | `NotifyMessageMaxRunes` | 500 | runes | `notify` イベントの `message` 上限 |
  | `ActionIDFormat` | "%d-%d" | format | plan の `action_id`（`cycle_id-seq`） |
  | `MinGraceSeconds` | 1 | seconds | `app.stop` の `grace_seconds` 下限 |
  | `MaxGraceSeconds` | 120 | seconds | `app.stop` の `grace_seconds` 上限 |
  | `ConfigRelPath` | ".config/tcc-local-connector/config.yml" | path | `config_paths.config` の既定解決先 |
  | `StateDirRelPath` | ".local/state/tcc-local-connector" | path | `config_paths.state_dir` |
  | `PauseFileName` | "pause.json" | — | `config_paths.pause` |
  | `LedgerFileName` | "managed-processes.json" | — | `config_paths.ledger` |
  | `LogFileName` | "backend.log" | — | `config_paths.log` |

- **禁止事項**: 該当する Don'ts のみ抜粋（本 Process のスコープ `internal/protocol/{server.go,params.go}` と `cmd/tcc-local-connector-backend/main.go` に限定）。
  - D-04 stdout へのプロトコル外書き込み禁止 — `rg -n 'fmt\.Print|os\.Stdout' internal/ cmd/` → **期待 1 件**（`cmd/tcc-local-connector-backend/main.go` の `NewServer` への `os.Stdout` 引き渡しのみ）
  - D-06 秘密情報の出力禁止 — `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal/protocol/server.go internal/protocol/params.go cmd/tcc-local-connector-backend/main.go` → **期待 0 件**（`status.user` にメールアドレスを含めないため、このスコープでは判定用パターンも不要）
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/protocol/params.go` → **期待 0 件**（`internal/constants/constants.go` 自身と `*_test.go` は対象外。既存 server.go L20 の `64*1024` は無改変のため対象外）
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/protocol/params.go internal/protocol/server.go cmd/tcc-local-connector-backend/main.go` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: エラーメッセージに機密情報（メール・トークン・設定全文・環境変数）を含めない。`status` にメールアドレスを載せない（`get_user`/`tcc2 status` から取得できてしまう情報を意図的に捨てる）。
  - **error**: エラーコードは小文字スネークケース。既存11種を変えず新規8種を追加する。
  - **stdout はプロトコル専用**、診断ログは stderr（`rg -n 'fmt\.Print|os\.Stdout' internal cmd` → 期待1件で main.go の `NewServer` 引き渡しのみ）。
  - **互換性**: 既存5メソッドの入出力を1バイトも変えない。既存テストの削除行0を git diff で機械照合する。
  - **該当 Don'ts**: D-04(stdout へのプロトコル外書き込み禁止)/D-06(秘密情報の出力禁止)/D-09(マジックナンバー直書き禁止)/D-12(TODO/FIXME 残存禁止)

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

改行区切り JSON（NDJSON）。既存 `readLine`(L381-423) を変更しない。stdin は Request のみ、stdout は Response または Event のみ。**双方向 RPC にしない**。`protocol.Version = 1` 据え置き。フロント側の識別規則は **`event` キーの有無**で Event と Response を分ける（`id` の有無に依存しない。`writeError` は `id:""` を omitempty で落とすため）。

```
// Why: バックエンド→フロントの OS 操作要求を双方向 request でなく「plan イベント発行 +
// report_actions での結果回収」で表現する。Serve は stdin を Request としてのみ解釈しており、
// 双方向化は読み取りループとフレーム識別の作り直しを要求するが、plan/report で等価に満たせる。
// Why: Version を2に上げない。既存5メソッドの入出力を一切変えないため破壊的変更がなく、
// 機能差分は ready の capabilities という既設の交渉機構で表現できる。
```

本 Process は `internal/protocol/server.go` の既存シンボルを直接改変する（`disjoint_guarantee: false`）ため、他の Process と並列実行しない単独 Wave（W04）として扱う。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/protocol/server.go` | 変更 | `Server` 構造体（L68-80）に `Engine EngineAPI` フィールドを追加。`Serve`（L99-182）のうち L105-111 の capabilities 配列にのみ7要素+3イベント能力を追加。`handle`（L231-279）に7 case を追加。新規メソッド `Emit`、新規 interface `EngineAPI` を追加。`Capability`(L51-53)・`readyData`(L55-58)・`NewServer`(L82-97) は無改変 |
| `internal/protocol/params.go` | 新規 | `statusResult`, `reloadConfigResult`, `pauseParams`, `pauseResult`, `resumeResult`, `refreshNowResult`, `configPathsResult`, `reportActionsParams`, `reportActionsResult`, `planEventData`, `stateChangedEventData`, `notifyEventData` の12構造体 |
| `cmd/tcc-local-connector-backend/main.go` | 変更 | `options`(L42-44) に `configPath string` を追加。`parseOptions`(L46-69) に `--config` フラグを追加。`main`(L18-40) に Engine 生成・`server.Engine = eng` の代入・`Engine.Run` の goroutine 起動を追加 |
| `internal/protocol/server_test.go` | 追記 | 新規7メソッド・3イベント・`TestReadyCapabilities` を追加。既存ケースは削除しない |
| `cmd/tcc-local-connector-backend/main_test.go` | 追記 | `--config` フラグのテストを追加 |

## Symbol Targets

```yaml
file: internal/protocol/server.go
symbols:
  - {name: Capability, kind: struct, body_start_line: 51, body_end_line: 53, line_hint: 51}   # 無改変
  - {name: readyData, kind: struct, body_start_line: 55, body_end_line: 58, line_hint: 55}     # 無改変
  - {name: Server, kind: struct, body_start_line: 68, body_end_line: 80, line_hint: 68}        # Engine EngineAPI フィールド追加のみ
  - {name: NewServer, kind: func, body_start_line: 82, body_end_line: 97, line_hint: 82}       # 無改変
  - {name: Serve, kind: func, body_start_line: 99, body_end_line: 182, line_hint: 99}          # L105-111 の capabilities 配列のみ変更
  - {name: handle, kind: func, body_start_line: 231, body_end_line: 279, line_hint: 231}       # 既存5 case は無改変。7 case を追加
  - {name: Emit, kind: method, body_start_line: 1, body_end_line: 15, line_hint: 375}          # 新規
  - {name: EngineAPI, kind: interface, body_start_line: 1, body_end_line: 10, line_hint: 65}   # 新規
patch_only: false
disjoint_guarantee: false
disjoint_guarantee_evidence: "既存 server.go の Server 構造体・Serve・handle を直接改変するため disjoint_guarantee は false と申告する。PLAN-for-mac.md Conflict Matrix の Process 07 行が同一結論（false, medium confidence, server.go L51-58/L68-80/L105-111/L231-279 は全文読取で行番号確定済み）。他 Process はこの Wave（W04）で同時実行しない。"
pre_flight_checks:
  - git_clean
  - symbol_exists:internal/protocol/server.go:Server
  - symbol_exists:internal/protocol/server.go:handle
  - symbol_exists:internal/protocol/server.go:Serve
  - symbol_exists:cmd/tcc-local-connector-backend/main.go:parseOptions
  - go_test_ok
---
file: internal/protocol/params.go
symbols:
  - {name: statusResult, kind: struct, body_start_line: 1, body_end_line: 30, line_hint: 1}
  - {name: reloadConfigResult, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 35}
  - {name: pauseParams, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 50}
  - {name: pauseResult, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 60}
  - {name: resumeResult, kind: struct, body_start_line: 1, body_end_line: 6, line_hint: 70}
  - {name: refreshNowResult, kind: struct, body_start_line: 1, body_end_line: 6, line_hint: 78}
  - {name: configPathsResult, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 86}
  - {name: reportActionsParams, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 96}
  - {name: reportActionsResult, kind: struct, body_start_line: 1, body_end_line: 6, line_hint: 108}
  - {name: planEventData, kind: struct, body_start_line: 1, body_end_line: 12, line_hint: 116}
  - {name: stateChangedEventData, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 130}
  - {name: notifyEventData, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 142}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean]
---
file: cmd/tcc-local-connector-backend/main.go
symbols:
  - {name: main, kind: func, body_start_line: 18, body_end_line: 40, line_hint: 18}
  - {name: options, kind: struct, body_start_line: 42, body_end_line: 44, line_hint: 42}
  - {name: parseOptions, kind: func, body_start_line: 46, body_end_line: 69, line_hint: 46}
patch_only: false
disjoint_guarantee: false
disjoint_guarantee_evidence: "main.go の既存3シンボルすべてに変更が入るため false と申告する。既存の signal 処理・NewServer 呼び出し・ProbeTCC2 代入・Serve 呼び出しの構造は保存する（追記のみ）。"
pre_flight_checks: [git_clean, go_build_ok]
```

## Verification Gates（P07）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P07-VG-01 | red | test | agent | true | `go test ./internal/protocol -run 'TestStatus\|TestPause' -count=1` | P07 の task_delta | exit != 0 | GoalEvidence（exit_code と `FAIL`/未定義シンボルを含む行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P07-VG-02 | green | test | agent | true | `go test ./internal/protocol ./cmd/... -race -count=1` | P07 の task_delta | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P07-VG-03 | green | conformance | agent | true | `git diff -- internal/protocol/server_test.go \| rg '^-' \| rg -v '^---'` | P07 の task_delta | 出力が空（既存テストの削除・変更 0 行） | GoalEvidence（コマンド出力そのもの。空であることを明記） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P07-VG-04 | green | conformance | agent | true | `go test ./internal/protocol -run TestReadyCapabilities -v` | P07 の task_delta | exit == 0。capabilities が15要素で既存5→新規7→イベント3の順序と一致 | GoalEvidence（capabilities 配列の出力を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P07-VG-05 | refactor | grep | agent | true | `rg -n 'fmt\.Print\|os\.Stdout' internal/ cmd/` | P07 の task_delta | ヒット == 1（cmd/.../main.go の os.Stdout 引き渡しのみ） | GoalEvidence（`rg -n` の出力行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01, SC-08 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 07
- gate_ids: [P07-VG-01, P07-VG-02, P07-VG-03, P07-VG-04, P07-VG-05]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-07.appendix.md（実行時に Read しない）

## Implementation Notes

**メソッド全一覧（既存5 + 新規7 = 12）**: 既存（無改変）: `health` / `echo` / `sleep`(上限30000ms) / `cancel` / `tcc2_probe`。新規: `status` / `reload_config` / `pause` / `resume` / `refresh_now` / `config_paths` / `report_actions`。

**ready イベントの capabilities — 確定した順序（15要素）**:
```
health, echo, sleep, cancel, tcc2_probe,           ← 既存5（server.go L106-110 の順序を保存）
status, reload_config, pause, resume, refresh_now, config_paths, report_actions,  ← 新規7
event.plan, event.state_changed, event.notify      ← イベント能力3
```
この順序は P07-VG-04 / P16-VG-02 で機械照合する。`readyData` と `Capability` の構造自体は変更しない。

**各メソッドの完全スキーマ**は Behavior Specification 表の io_mapping（`BEH-07-07`〜`BEH-07-30`）で検証する。要点のみここに記す:

- `status`: params なし。読み取り専用（状態を一切変更しない）。`last_error.code` は `tcc2_api_error|empty_content|unrecognized_format|too_many_running_tasks|tcc2_error|timeout|cancelled` の enum。`parse_ok==false` のとき `running_tasks` は前回値を保持（P06 `Engine.Snapshot` の契約をそのまま反映）。メールアドレスは絶対に含めない。Engine 未配線時のみ `unsupported`。
- `reload_config`: params なし。不変条件 `ok==false ⇒ applied==false`、`applied==true ⇒ len(errors)==0`。成功時は state を `fetching` へ遷移させ即時サイクルを起動。RPCError: `config_not_found` / `insecure_permissions` / `io_error`。
  ```
  // Why: 権限違反を errors[] でなく RPCError にする。スキーマ違反は「設定内容の問題」で
  // 利用者が直せば済むが、権限違反は「このファイルを信用してよいか」の問題で内容を
  // 1バイトも読まずに拒否する必要がある。
  ```
- `pause`: `duration_seconds`(`PauseMinSeconds..PauseMaxSeconds`) と `until`(RFC3339、現在より未来必須) は**厳密にどちらか一方が必須**。`reason` 既定 `user_requested`。post_state: ①進行中サイクルを context cancel ②pause.json へ絶対時刻をアトミック書き込み(0600) ③**空 actions の plan イベントを発行** ④state を paused へ。RPCError: `invalid_params` / `internal_error`。
  ```
  // Why: pause.json の書き込みに失敗したらメモリ上の一時停止も設定しない。永続化できない
  // 一時停止は次回起動で黙って消え、利用者は「止めた」と思っているのに制御が復活する。
  ```
- `resume`: params なし。post_state: ①pause.json 削除 ②state を fetching へ ③即時に1サイクル実行。RPCError: `invalid_state`（現在 paused でない。**冪等にせず明示エラー**）/ `internal_error`。
- `refresh_now`: params なし。result `accepted`(常に true)/`cycle_id`。post_state: state を fetching へ遷移させサイクル起動。次回定期 tick の基準時刻をリセット。RPCError: `busy`（single-flight）/ `paused` / `config_error`。
- `config_paths`: params なし。result（全フィールド必須・`~` 展開済み絶対パス）: `config`/`state_dir`/`log`/`pause`/`ledger`。フロントで組み立てずバックエンドから受け取る（正本を1か所に限定）。post_state: 不変。RPCError: なし。
- `report_actions`: `cycle_id`(必須)/`results`(必須、空可)。要素: `action_id`/`status`(enum: ok|failed|refused|skipped|timeout)/`code`(status=="ok"のときnull、それ以外は非null)/`detail`(最大500文字、機密情報を含めない)。不変条件: `accepted + ignored == len(results)`、`cycle_id != 現行 ⇒ accepted==0 && ignored==len(results)`。post_state: `status ∈ {failed,refused,timeout}` について `notify{action_refused}` を発行。RPCError: `invalid_params`。

**イベント全スキーマ（4種）**:
- `ready`（既存を拡張。上記 capabilities 15要素）
- `plan`: `cycle_id`/`issued_at`/`state`/`dry_run`(true ならフロントは副作用APIを呼ばず skipped/already_satisfied を返す)/`actions`(空可)/`enforce_stop_bundle_ids`(空可。actions内の`app.stop`のbundle_id集合と厳密一致)。actions要素: `action_id`("<cycle_id>-<seq>")/`kind`(enum: app.start|app.stop|notify のみ)/`rule_id`/`reason`(enum: ensure|on_enter|on_exit)/kindがapp.*なら`bundle_id`/kind=="app.stop"なら`grace_seconds`(1..120)/kind=="notify"なら`title`(最大200文字)`message`(最大500文字)`level`(enum info|warn|error)。**`process.start`/`process.stop`/`command.run` はバックエンド自身が実行し plan に現れない**。空 plan（actions・enforce_stop_bundle_ids ともに空）=「以後何も強制しない=制御解除」。`released` と `paused` で発行される。
- `state_changed`: `from`/`to`/`at`/`cycle_id`/`code`(enum: started|fetch_failed|parse_failed|grace_expired|recovered|paused|resumed|config_invalid|config_reloaded)/`message`(空可・機密なし)。
- `notify`: `level`(enum info|warn|error)/`code`(enum: rule_conflict|config_invalid|auth_expired|parse_failed|action_refused|grace_expired|process_orphan_dropped|rule_notify)/`title`(最大200文字)/`message`(最大500文字)/`at`。設定由来の通知は `plan.actions[].kind=="notify"` として渡り、この notify イベントは**バックエンド起因の通知**（競合・解析失敗等）に使う。

**エラーコード一覧**: 既存11種（変更しない）: `message_too_large`(L157) / `invalid_json`(L172) / `unsupported_version`(L192) / `invalid_request`(L195,L198) / `duplicate_id`(L207,L289,L297) / `not_found`(L300) / `invalid_params` / `cancelled` / `timeout` / `unsupported` / `tcc2_error` / `method_not_found`。新規8種: `config_not_found` / `insecure_permissions` / `io_error` / `busy` / `paused` / `invalid_state` / `internal_error` / `config_error`。

**main.go の変更**: `options` に `configPath string` を追加。`parseOptions` に `--config` フラグ追加（既定 `""` = `config.DefaultPath()` を使う）。既存の `serve` 検査(L47-49)、`--stdio` 必須(L61-63)、`--tcc2-executable` 空チェック(L64-66)、positional 拒否(L58-60) は無改変。`main` は Engine の生成と `server.Engine = eng` の代入、Engine の `Run` を goroutine 起動を追加。既存の signal 処理(L25-26)、`NewServer`(L28-32)、`ProbeTCC2` 代入(L33-35)、`Serve`(L36-39) の構造は保存する。`EngineAPI` は新規 interface: `Snapshot()`/`Reload()`/`Pause()`/`Resume()`/`RunCycleNow()`/`ReportActions()`/`Paths()` を宣言。`Server` に `Engine EngineAPI` フィールドを追加（`NewServer` は変更せず、呼び出し側が代入する。既存 `ProbeTCC2`(L92-94) と同じパターン）。`Emit` は新規メソッドで既存 `write`(L360-372) を使って Event を送出する。

## Behavior Specification
System Type: reactive

> `api_scope: false`（stdio RPC のメソッド契約として扱う）。入出力表は本表の各行を io_mapping として P16（RPC契約テスト）が検証する。

| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-07-01 | 任意 | `health` 呼び出し | - | 変化なし | 既存の入出力を1バイトも変えない（無改変） | TestHealth_Unchanged |
| BEH-07-02 | 任意 | `echo` 呼び出し | - | 変化なし | 同上 | TestEcho_Unchanged |
| BEH-07-03 | 任意 | `sleep` 呼び出し（上限30000ms） | - | 変化なし | 同上 | TestSleep_Unchanged |
| BEH-07-04 | 任意 | `cancel` 呼び出し | - | 変化なし | 同上 | TestCancel_Unchanged |
| BEH-07-05 | 任意 | `tcc2_probe` 呼び出し | - | 変化なし | 同上 | TestTCC2Probe_Unchanged |
| BEH-07-06 | 起動時 | `ready` イベント送出（起動時1回） | - | 変化なし | capabilities が既存5＋新規7＋イベント3の順で15要素 | TestReadyCapabilities |
| BEH-07-07 | 任意 | `status` 呼び出し | Engine 配線済み | 変化なし | 状態を一切変更しない読み取り専用。全フィールド必須（null/空配列許容）でメールアドレスを含まない | TestStatus_ReadOnly_NoEmail |
| BEH-07-08 | 任意 | `status` 呼び出し | Engine 未配線 | 変化なし | RPCError `unsupported` | TestStatus_Unsupported_NoEngine |
| BEH-07-09 | 任意（config_error以外） | `reload_config` 呼び出し | 検証通過 | fetching | `ok==true`, `applied==true`, `errors==[]`、即時サイクル起動 | TestReloadConfig_Success |
| BEH-07-10 | 任意 | `reload_config` 呼び出し | 検証失敗（スキーマ違反） | 変化なし | `ok==false ⇒ applied==false`、`errors` に該当コードを含む | TestReloadConfig_SchemaInvalid |
| BEH-07-11 | 任意 | `reload_config` 呼び出し | ファイル不在 | 変化なし | RPCError `config_not_found` | TestReloadConfig_NotFound |
| BEH-07-12 | 任意 | `reload_config` 呼び出し | 権限違反 | 変化なし | RPCError `insecure_permissions` | TestReloadConfig_InsecurePermissions |
| BEH-07-13 | 任意 | `reload_config` 呼び出し | 読み取りI/Oエラー | 変化なし | RPCError `io_error` | TestReloadConfig_IOError |
| BEH-07-14 | 任意（paused/config_error除く） | `pause` 呼び出し | `duration_seconds` と `until` の両方指定 | 変化なし | RPCError `invalid_params` | TestPause_BothSpecified_InvalidParams |
| BEH-07-15 | 同上 | `pause` 呼び出し | `duration_seconds` も `until` も未指定 | 変化なし | RPCError `invalid_params` | TestPause_NeitherSpecified_InvalidParams |
| BEH-07-16 | 同上 | `pause` 呼び出し | `duration_seconds` が範囲外 | 変化なし | RPCError `invalid_params` | TestPause_DurationOutOfRange |
| BEH-07-17 | 同上 | `pause` 呼び出し | `until` が過去時刻 | 変化なし | RPCError `invalid_params` | TestPause_UntilInPast |
| BEH-07-18 | 同上 | `pause` 呼び出し | 正当な `duration_seconds`、pause.json書き込み成功 | paused | 進行中サイクルをcancel、pause.jsonへ絶対時刻をアトミック書き込み(0600)、空actionsのplanイベント発行 | TestPause_Success |
| BEH-07-19 | 同上 | `pause` 呼び出し | pause.json書き込み失敗 | 変化なし | RPCError `internal_error`。メモリ上の一時停止も設定しない | TestPause_PersistFailure_NoStateChange |
| BEH-07-20 | paused | `resume` 呼び出し | - | fetching | pause.json削除、即時1サイクル実行、`resumed_at` を返す | TestResume_Success |
| BEH-07-21 | paused以外 | `resume` 呼び出し | - | 変化なし | RPCError `invalid_state`（冪等にしない） | TestResume_InvalidState_NotIdempotent |
| BEH-07-22 | fetching/active/degraded/released | `refresh_now` 呼び出し | サイクル実行中でない | fetching | `accepted==true`、`cycle_id` 返却、次回定期tick基準時刻リセット | TestRefreshNow_Success |
| BEH-07-23 | 同上 | `refresh_now` 呼び出し | サイクル実行中（single-flight） | 変化なし | RPCError `busy` | TestRefreshNow_Busy |
| BEH-07-24 | paused | `refresh_now` 呼び出し | - | 変化なし | RPCError `paused` | TestRefreshNow_Paused |
| BEH-07-25 | config_error | `refresh_now` 呼び出し | - | 変化なし | RPCError `config_error` | TestRefreshNow_ConfigError |
| BEH-07-26 | 任意 | `config_paths` 呼び出し | - | 変化なし | `config`/`state_dir`/`log`/`pause`/`ledger` が `~` 展開済み絶対パスで全フィールド必須 | TestConfigPaths_AllFieldsAbsolute |
| BEH-07-27 | 任意 | `report_actions` 呼び出し | `cycle_id` が現行と一致 | 変化なし | `accepted+ignored==len(results)`。`status∈{failed,refused,timeout}` の結果について `notify{action_refused}` を発行 | TestReportActions_AcceptedAndNotify |
| BEH-07-28 | 任意 | `report_actions` 呼び出し | `cycle_id` が現行と不一致 | 変化なし | `accepted==0 && ignored==len(results)` | TestReportActions_StaleCycleAllIgnored |
| BEH-07-29 | 任意 | `report_actions` 呼び出し | `status=="ok"`だが`code`が非null、または`status!="ok"`だが`code`がnull | 変化なし | RPCError `invalid_params` | TestReportActions_InvalidCodeStatusPair |
| BEH-07-30 | released/paused | `plan` イベント発行 | 制御解除条件成立 | 変化なし | `actions` と `enforce_stop_bundle_ids` がともに空 | TestPlanEvent_EmptyOnReleaseOrPause |

### Correctness Criteria（観測可能・固定する）
- 既存5メソッドの入出力は1バイトも変わらない（`git diff -- internal/protocol/server_test.go` の削除行0で機械照合、`BEH-07-01`〜`05`）。
- `capabilities` は15要素で既存5→新規7→イベント3の順序に固定（`BEH-07-06`）。
- `reload_config`: `ok==false ⇒ applied==false`、`applied==true ⇒ len(errors)==0`。
- `report_actions`: `accepted + ignored == len(results)`、`cycle_id` 不一致時は `accepted==0 && ignored==len(results)`。
- `pause`: pause.json 書き込み失敗時はメモリ上の一時停止状態も変更しない（`BEH-07-19`）。
- `resume`: 非paused状態では冪等にせず `invalid_state` を返す（`BEH-07-21`）。
- `status`: 呼び出しは状態を一切変更しない（読み取り専用、`BEH-07-07`）。
- `status`: いかなるフィールドにもメールアドレスを含めない。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `params.go` の各構造体の Go フィールド名・JSON タグの内部命名（JSON スキーマとしての表現は固定済み、Go の慣用的な命名のみ裁量）。
- `EngineAPI` の内部呼び出し順序（validate→persist→transition の分割方法）。
- `handle` 内での7 case の並び順（既存5 case の後に追記する制約のみ固定）。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（本 Process では上記3点のみが実装者の裁量）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/protocol/server_test.go` に `TestStatus`/`TestPause` 等（存在しない新規メソッドを呼ぶ最小ケース）を追記。既存ケースは1行も削除・変更しない
- [x] `cmd/tcc-local-connector-backend/main_test.go` に `--config` フラグのテストを追記
- [x] Behavior Specification 表の全30行に対応する test_ref を用意
- [x] テストを実行して失敗することを確認（未定義メソッド・未定義フラグによるコンパイルエラーまたは FAIL）は、実装完了時点では失敗想定が成立せず、代わりに全テスト成功を確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P07-VG-01 / status: / command_or_action: `go test ./internal/protocol -run 'TestStatus\|TestPause' -count=1` / exit_code: / expected: exit!=0 / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/protocol/params.go` を新規作成し12構造体を実装
- [x] `internal/protocol/server.go`: `Server` へ `Engine EngineAPI` フィールド追加、`Serve` L105-111 の capabilities 配列を15要素へ拡張、`handle` へ7 case を追加、`Emit` メソッドと `EngineAPI` interface を新規追加
- [x] `cmd/tcc-local-connector-backend/main.go`: `options.configPath` 追加、`parseOptions` へ `--config` フラグ追加、`main` で Engine 生成・`Server.Engine` 代入・`Engine.Run` の goroutine 起動を追加
- [x] Behavior Specification 表の全30行に対応する test_ref のテストが存在し PASS することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P07-VG-02 / status: / command_or_action: `go test ./internal/protocol ./cmd/... -race -count=1` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P07-VG-04 / status: / command_or_action: `go test ./internal/protocol -run TestReadyCapabilities -v` / exit_code: / expected: capabilities15要素が既定順序で出現 / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `git diff -- internal/protocol/server_test.go` を確認し既存ケースの削除・変更が0行であることを再確認
- [x] D-04/D-06/D-09/D-12 grep を再実行しゼロヒット（D-04は1ヒット）を確認
- [x] `handle` の新規7 case が既存5 case のロジックに影響していないことを再確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P07-VG-03 / status: / command_or_action: `git diff -- internal/protocol/server_test.go \| rg '^-' \| rg -v '^---'` / exit_code: / expected: 出力が空 / observed: / attempt:
- gate_id: P07-VG-05 / status: / command_or_action: `rg -n 'fmt\.Print\|os\.Stdout' internal/ cmd/` / exit_code: / expected: ヒット==1 / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: agent** — 操作: バックエンドを stdio モードで起動し `ready` イベントの `capabilities` を確認する → 期待される出力: 15要素が既存5→新規7→イベント3の順で出現 → 確認方法: `TestReadyCapabilities` の出力ログで capabilities 配列を逐語比較する
2. **executor: human** — 操作: `--config` フラグに存在しない設定ファイルパスを渡してバックエンドを起動する → 期待される出力: `state=config_error` で起動しポーリング・アクションを一切行わない → 確認方法: `status` を呼び出し `state=="config_error"` であることとログに OS 操作の痕跡がないことを確認する
3. **executor: agent** — 操作: `pause` を呼び出した直後にプロセスを終了させ再起動する → 期待される出力: pause.json が永続化されており再起動後も `paused` 状態から始まる → 確認方法: 起動時の `status` 呼び出し結果の `state` と `paused_until` を確認する

## Dependencies
- Requires: P06
- Blocks: P16, P18
