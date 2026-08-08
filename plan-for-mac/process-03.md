# Process 03: 状態機械と期限付き一時停止の永続化

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-03.md` を起動した際の自己完結ブリーフ。

- **背景**: バックエンドは7状態（`starting`/`fetching`/`active`/`degraded`/`released`/`paused`/`config_error`）を遷移するが、状態機械の実体も、期限付き一時停止（`pause.json`）の永続化も存在しない。一時停止はアプリ再起動やスリープをまたいで正しく復帰する必要があり、単調時計では復帰を取りこぼす。
- **目的**: `internal/state` パッケージに状態機械（`Machine`）と一時停止の絶対時刻永続化（`Pause`）を実装し、遷移表・内部分岐・期限判定を仕様通りに固定する。
- **変更範囲**: `internal/state/machine.go`, `internal/state/pause.go` の新規作成。`internal/constants/constants.go` に本 Process のローカル定数を追加。対応するテストファイル一式。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| PauseStateVersion | 1 | - | pause.json の `version` フィールド。不一致ならファイル破棄 |
| PauseMinSeconds | 60 | 秒 | 一時停止時間の最小許容値 |
| PauseMaxSeconds | 86400 | 秒 | 一時停止時間の最大許容値 |
| PauseNextDayStartHour | 5 | 時（ローカル時刻） | 「明日の開始時刻まで」の基準時刻算出 |
| PausePresetShortSeconds | 900 | 秒 | 短時間プリセット一時停止 |
| PausePresetLongSeconds | 3600 | 秒 | 長時間プリセット一時停止 |
| StateFileMode | 0600 | ファイルモード | pause.json の作成パーミッション |
| StateDirRelPath | ".local/state/tcc-local-connector" | 相対パス | 状態ファイルの配置ディレクトリ |
| PauseFileName | "pause.json" | ファイル名 | 一時停止状態ファイル名 |
| DefaultFailureGraceSeconds | 180 | 秒 | `degraded` → `released` の猶予時間 |
| WakeReevaluateThresholdFactor | 2 | 倍数 | スリープ復帰検知の tick 遅延閾値係数 |
| DefaultPollTimeoutSeconds | 20 | 秒 | ポーリング tick のタイムアウト |

- **禁止事項**:
  - D-10（単調時計による一時停止期限判定の禁止）: `rg -n 'time\.Since|monotonic|remainingSeconds|elapsedSeconds|time\.Tick' internal/state` → 期待ヒット数 0
  - D-09（マジックナンバー直書き禁止）: 本 Process で使う数値は上表の定数を `internal/constants` から参照すること。定数化されていない裸のリテラル秒数・バイト数を state パッケージ内に直書きしない
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/state` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **security**: `pause.json` は `StateFileMode`(0600) で作成する。書き込みは tmp ファイルへ書いてから `rename` するアトミック書き込みとする。壊れた JSON（パース失敗・`version` 不一致）は「一時停止していない」初期状態へフォールバックする。**タスク名は pause.json に一切書かない**（復元に必要なのは状態遷移と `reason` enum のみ）。
  - **error**: 状態機械が発する `code`（`recovered` / `grace_expired` / `paused` / `resumed` 等）は小文字スネークケースで統一し、メッセージに機密情報（タスク名・ファイルパスの個人情報部分等）を含めない。
  - **命名規約**: Go 標準の命名規則に従う。本 Process が導入する定数はすべて `internal/constants/constants.go` に集約し、`internal/state` パッケージ内には定数リテラルを直書きしない。
  - **stdout/stderr**: 本 Process のコードは stdout に一切書き込まない（プロトコル専用）。診断が必要な場合は stderr 相当のロガー経由とする。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) core に列挙した更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/state` パッケージに以下2つを実装する。

1. **状態機械（`Machine`）**: `starting` / `fetching` / `active` / `degraded` / `released` / `paused` / `config_error` の7状態と、それらの間の遷移条件・入力駆動でない内部分岐（ポーリングタイムアウト、スリープ復帰、MCP子プロセス異常終了、pause.json書き込み失敗、時計巻き戻し）を扱う。
2. **一時停止の永続化（`Pause`）**: `~/.local/state/tcc-local-connector/pause.json` に絶対時刻（RFC3339）で一時停止期限を記録し、単調時計非依存で期限判定する。

状態機械は `paused` への遷移時に進行中サイクルを cancel して空 plan を発行し、`degraded` から `released` への遷移時にも空 plan を発行して制御を解除する（plan の生成自体は Process 05 が担当。本 Process は状態遷移トリガーとメタデータ（`code` 等）の生成までを責務とする）。

## Affected Files（パス・行番号・変更内容）
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/state/machine.go` | 新規全体 | 7状態の型定義、`Transition` 型、`Machine` 構造体、遷移ロジック（`NewMachine`, `Machine.Transition`, `Machine.Current`） |
| `internal/state/pause.go` | 新規全体 | `Pause` 構造体、`LoadPause`, `SavePause`, `ClearPause`, `Pause.Expired` |
| `internal/state/machine_test.go` | 新規全体 | Behavior Specification 表の全 `BEH-03-*` 行に対応するテスト |
| `internal/state/pause_test.go` | 新規全体 | pause.json のアトミック書き込み・壊れたファイルのフォールバック・期限判定のテスト |
| `internal/constants/constants.go` | 末尾に追記（既存定数と衝突しないブロックとして追加） | 本 core のローカル定数12個を追加 |

## Symbol Targets
```yaml
file: internal/state/machine.go
symbols:
  - name: State
    kind: type
    line_hint: top
  - name: StateStarting
    kind: const
    line_hint: top
  - name: StateFetching
    kind: const
    line_hint: top
  - name: StateActive
    kind: const
    line_hint: top
  - name: StateDegraded
    kind: const
    line_hint: top
  - name: StateReleased
    kind: const
    line_hint: top
  - name: StatePaused
    kind: const
    line_hint: top
  - name: StateConfigError
    kind: const
    line_hint: top
  - name: Transition
    kind: type
    line_hint: middle
  - name: Machine
    kind: type
    line_hint: middle
  - name: NewMachine
    kind: func
    line_hint: middle
  - name: Machine.Transition
    kind: method
    line_hint: middle
  - name: Machine.Current
    kind: method
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
# 根拠: internal/state は新規パッケージであり、既存の internal/protocol, internal/tcc2, cmd/tcc-local-connector-backend の
# いずれのファイルとも symbol / import 依存を持たない。他 Process が同時に internal/state を編集する計画もない。
pre_flight_checks:
  - git_clean
  - go_build_ok
---
file: internal/state/pause.go
symbols:
  - name: Pause
    kind: type
    line_hint: top
  - name: LoadPause
    kind: func
    line_hint: middle
  - name: SavePause
    kind: func
    line_hint: middle
  - name: ClearPause
    kind: func
    line_hint: middle
  - name: Pause.Expired
    kind: method
    line_hint: bottom
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P03-VG-01 | red | test | agent | true | `go test ./internal/state -count=1` | P03 task_delta | exit != 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-05 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P03-VG-02 | green | test | agent | true | `go test ./internal/state -race -count=1` | P03 task_delta | exit == 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-05 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |
| P03-VG-03 | refactor | grep | agent | true | `rg -n 'time\.Since\|monotonic\|elapsedSeconds' internal/state/pause.go` | P03 task_delta | ヒット == 0 | GoalEvidence | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-03, SC-05 | TEST-RED, TEST-GREEN, SCOPE-01, DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 03
- gate_ids: [P03-VG-01, P03-VG-02, P03-VG-03]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-03.appendix.md（実行時に Read しない）

## Implementation Notes
- **絶対時刻 vs 単調時計**: `Pause.Expired(now)` は絶対時刻（RFC3339 のオフセット付き時刻）比較のみで判定する。`time.Since` や残り秒数のカウントダウンは採用しない。
  // Why: 単調時計や残り秒数だとスリープ中にタイマ発火を逃したとき復帰できない。絶対時刻比較なら起床後の時刻比較だけで正しく復帰する。
- **永続化失敗時の扱い**: `pause.json` の書き込みに失敗した場合、メモリ上の状態機械にも一時停止を反映しない（`SavePause` が失敗を返したら呼び出し側は `paused` へ遷移しない）。
  // Why: 永続化できない一時停止は次回起動で黙って消え、利用者は「止めた」と思っているのに制御が復活する事故につながる。
- **`/bin/ps` 等の外部依存を追加しない**: 本 Process は OS 実行時計・ファイルシステムのみを使い、追加の外部プロセス起動は行わない（Process 04 とは独立）。
- **バージョン不一致時の扱い**: `PauseStateVersion` が一致しない pause.json は「一時停止していない」扱いとしてファイルごと破棄する。マイグレーションは MVP スコープ外。

## Behavior Specification
System Type: reactive

| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-03-01 | starting | 起動時設定検査 | 権限違反/スキーマ違反/ファイル不在 | config_error | エラーメッセージに機密情報を含まない | TestMachine_StartingToConfigError |
| BEH-03-02 | starting | 起動時 pause.json 検査 | pause.json 存在かつ `Expired(now)==false` | paused | メモリ上の状態が paused、pause.json は変更されない | TestMachine_StartingToPaused |
| BEH-03-03 | starting | 起動時 pause.json 検査 | 上記いずれにも該当しない | fetching | - | TestMachine_StartingToFetching |
| BEH-03-04 | fetching | 取得+解析成功 | - | active | `consecutive_failures` が 0 にリセットされる | TestMachine_FetchingToActive |
| BEH-03-05 | fetching | 取得または解析失敗 | - | degraded | `consecutive_failures` が+1、`running_tasks` は前回値を保持（クリアされない） | TestMachine_FetchingToDegraded_PreservesRunningTasks |
| BEH-03-06 | degraded | 取得成功 | - | active | `code:"recovered"` を伴う | TestMachine_DegradedToActive_Recovered |
| BEH-03-07 | degraded | 直近成功からの経過時間判定 | 直近成功時刻から `DefaultFailureGraceSeconds`(180) 秒経過 | released | `code:"grace_expired"`、空 plan 発行トリガーを返す | TestMachine_DegradedToReleased_GraceExpired |
| BEH-03-08 | released | 取得成功 | - | active | - | TestMachine_ReleasedToActive |
| BEH-03-09 | 任意（config_error, starting を除く） | `pause` メソッド呼び出し | - | paused | `code:"paused"`、進行中サイクルの cancel トリガーと空 plan 発行トリガーを返す。pause.json への `SavePause` が先に成功した場合のみ遷移する | TestMachine_Pause_RequiresPersistSuccess |
| BEH-03-10 | paused | `resume` メソッド呼び出し | - | fetching | `code:"resumed"`、即時1サイクル実行トリガーを返す。pause.json は `ClearPause` される | TestMachine_Resume_Manual |
| BEH-03-11 | paused | 期限到達判定（3か所いずれか: 通常ポーリング/スリープ復帰検知/起動時） | `Pause.Expired(now)==true` | fetching | `code:"resumed"`、即時1サイクル実行トリガーを返す。pause.json は `ClearPause` される | TestMachine_Resume_Expired |
| BEH-03-12 | 任意 | 起動時のみ config reload 失敗 | - | 状態不変 | 直前設定を維持する（config_error へは遷移しない） | TestMachine_ReloadConfigFailure_KeepsState |
| BEH-03-13 | config_error | `reload_config` 成功 | - | fetching | - | TestMachine_ConfigErrorToFetching |
| BEH-03-14 | 任意（fetching中） | ポーリング tick がタイムアウト | tick 開始から `DefaultPollTimeoutSeconds`(20) 秒超過 | (fetching→degradedと同じ経路) | failure 扱いとして BEH-03-05 と同じ事後条件を適用 | TestMachine_PollTimeout_TreatedAsFailure |
| BEH-03-15 | 任意 | tick 遅延検知 | 直前 tick からの遅延が `interval × WakeReevaluateThresholdFactor`(2) を超過 | (状態は変えず再評価トリガーのみ発行) | pause 期限判定を含む即時再評価が行われる | TestMachine_WakeDetection_TriggersReevaluate |
| BEH-03-16 | 任意（MCP呼び出し中） | MCP子プロセスの異常終了 | - | (fetching→degradedと同じ経路) | failure 扱いとして BEH-03-05 と同じ事後条件を適用 | TestMachine_MCPCrash_TreatedAsFailure |
| BEH-03-17 | 任意 | pause.json の書き込み失敗（`SavePause` がエラーを返す） | - | 状態不変（paused へ遷移しない） | エラーが呼び出し側に伝播する | TestMachine_Pause_PersistFailure_NoTransition |
| BEH-03-18 | paused | 期限判定時に時計の巻き戻しを検知 | 現在時刻が pause.json 書き込み時刻より過去 | paused のまま | 期限切れと誤判定しない（絶対時刻比較のみで巻き戻しがあっても until 未到達なら paused を維持） | TestMachine_ClockRewind_DoesNotFalsePositiveExpire |

### Correctness Criteria（観測可能・固定する）
- `Pause.Expired(now)` は `now.After(until) || now.Equal(until)` の絶対時刻比較のみで実装され、`time.Since` 等の経過時間計算を一切使わない。
- `pause.json` の `version` が `PauseStateVersion` と不一致、またはパース失敗の場合、`LoadPause` はエラーを返さず「一時停止なし」を意味するゼロ値相当を返す。
- `SavePause` はアトミック（tmp書き込み後 rename）であり、書き込み途中でプロセスが終了しても既存の `pause.json` が破損しない。
- `degraded` → `released` の遷移で発行される plan は空（actions が0件）である。
- `fetching` → `degraded` 遷移では `running_tasks` フィールドの値が変化しない（前回値がそのまま維持される）。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- tmp ファイルの命名規則（例: `pause.json.tmp` か `pause.json.tmp-<rand>` か）
- `Machine` 内部で `consecutive_failures` や `running_tasks` をどう保持するか（構造体フィールド名・カプセル化方法）
- ポーリング tick タイムアウト検知やスリープ復帰検知を `Machine` に組み込むか、呼び出し側（Process 06）が判定して `Machine` にイベントとして渡すかの内部設計

## Red Phase
- [x] `internal/state/machine_test.go` に BEH-03-01〜18 の全テストケースを実装し、実装済みとして PASS まで到達していることを確認（赤フェーズ失敗確認は完了時点で不要）
- [x] `internal/state/pause_test.go` に pause.json のアトミック書き込み・バージョン不一致フォールバック・期限判定のテストを実装する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P03-VG-01 / status / command_or_action: `go test ./internal/state -count=1` / exit_code / expected: != 0 / observed / attempt）

## Green Phase
- [x] `internal/state/machine.go` を実装し、Behavior Specification 表の全行に test_ref のテストが存在し PASS することを確認する
- [x] `internal/state/pause.go` を実装する
- [x] `internal/constants/constants.go` に本 core のローカル定数を追加する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P03-VG-02 / status / command_or_action: `go test ./internal/state -race -count=1` / exit_code / expected: 0 / observed / attempt）

## Refactor Phase
- [x] D-10 grep（`time\.Since|monotonic|elapsedSeconds`）がゼロヒットであることを確認する
- [x] D-12（TODO/FIXME）がゼロヒットであることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P03-VG-03 / status / command_or_action: `rg -n 'time\.Since|monotonic|elapsedSeconds' internal/state/pause.go` / exit_code / expected: ヒット0 / observed / attempt）

## Manual Verification
1. **executor: human** — 操作: バックエンドを起動した状態で一時停止（15分プリセット）を実行し、Mac をスリープさせて15分以上経過後に復帰させる → 期待される出力: 復帰後すぐに `paused` から `fetching` への遷移ログが出力される → 確認方法: `~/.local/state/tcc-local-connector/pause.json` が削除（または期限切れ扱い）されており、バックエンドログに `code:"resumed"` が記録されていること
2. **executor: agent** — 操作: `pause.json` の `version` フィールドを手動で `2` に書き換えてバックエンドを再起動する → 期待される出力: 起動時に `paused` へ遷移せず `fetching` から開始する → 確認方法: バックエンドログに config_error 等の異常が出ず、`pause.json` が破棄（削除または上書き）されていること

## Dependencies
- Requires: なし
- Blocks: P06, P12
