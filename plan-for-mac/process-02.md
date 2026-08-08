# Process 2: tcc2 MCP セッションと `[In Progress]` 解析

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-02.md` を起動した際の自己完結ブリーフ。

- **背景**: TaskChute Cloud 2 の MCP ツール `get_taskchute` は構造化応答を持たず（実測 F1）、テキスト解析が唯一の経路である。表示テキストの形式変更は「実行中タスク0件」という誤判定を招き、`task_name_not_contains` 系ルールが全発火して利用者のアプリが意図せず終了させられるリスク（PLAN-for-mac.md Risk R1）がある。
- **目的**: 右アンカー解析（ID ブロック→メタ括弧→名前）で `[In Progress]` 行を取り出し、「実行中タスクなし」と「解析失敗」を型レベルで区別する。2日範囲での `get_taskchute`/`get_user` 呼び出しと CLI 版のベストエフォート解決を実装する。既存 `probe.go` は完全無改変。
- **変更範囲**: `internal/tcc2/taskchute.go`, `internal/tcc2/parse.go`, `internal/tcc2/version.go`, `internal/tcc2/parse_test.go`, `internal/tcc2/taskchute_test.go`, `internal/tcc2/testdata/{get_taskchute_sample.txt,get_user_sample.txt,get_taskchute_error.json}`, `internal/constants/constants.go`（解析系定数の追記）
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |-------|-----|------|----------------------|
  | `InProgressLinePrefix` | "- [In Progress] "（16B） | literal | 実行中タスク行のオフセット0完全一致判定 |
  | `IDBlockDelimiter` | " [ID: "（6B） | literal | ID ブロックの右アンカー検出（`LastIndex`） |
  | `DateHeaderPattern` | `^##\s+.*?(\d{4}-\d{2}-\d{2})\s*$` | regexp | 見出し行から日付を抽出 |
  | `MetaHeadPattern` | `^\d{2}:\d{2}(,\|$)` | regexp | メタ括弧か名前の一部かの判定 |
  | `TaskIDPattern` | `^task_[0-9a-f]{32}$` | regexp | ID ブロック先頭位置引数の形式検証 |
  | `UserFieldPattern` | `^- \*\*([^*]+):\*\* (.*)$` | regexp | `get_user` markdown 太字フィールドの分解 |
  | `CellarVersionPattern` | `(?:^\|/)Cellar/tcc2/([^/]+)/` | regexp | CLI 版のベストエフォート解決 |
  | `IDBlockKeys` | Section, Project, Mode, Routine, Tags | list | 既知 ID ブロックキー。未知キーは warnings |
  | `KnownStatusTags` | Done, "In Progress", Todo | list | 形式変更検知のカナリア（StatusCounts） |
  | `MaxRunningTasks` | 32 | count | 実行中タスク行数の暴走ガード |
  | `MaxTaskNameBytes` | 512 | bytes | タスク名の切り詰め上限（ルーン境界） |
  | `MaxErrorMessageBytes` | 200 | bytes | `ParseError.Message` の上限 |
  | `TaskChuteQueryDays` | 2 | days | `[d-1, d]` の2日範囲クエリ |
  | `UserInfoTTLSeconds` | 21600 | seconds | `get_user` キャッシュ寿命（6h） |
  | `MCPWaitDelaySeconds` | 2 | seconds | `cmd.WaitDelay` |
  | `MCPStderrLimitBytes` | 4096 | bytes | stderr 切り捨て上限 |
  | `MCPMaxResponseBytes` | 1048576 | bytes | 応答上限（1 MiB） |
  | `tcc2.mcpProtocolVersion` | "2025-06-18" | — | 既存 probe.go と同値。新規実装でも同じ値を使う |

- **禁止事項**: 該当する Don'ts のみ抜粋。
  - D-06 秘密情報の出力禁止 — `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal/tcc2` → **期待 1 件**（`ResolveAuthStatus` 内の `"Logged in as"` 判定用リテラルのみ。値は保持・出力しない）
  - D-07 バンドルID・絶対パスの直書き禁止 — `rg -n 'com\.tinyspeck|com\.amazon\.Lassen|/opt/homebrew|/Users/takets' internal/tcc2 --glob '!**/testdata/**'` → **期待 0 件**
  - D-08 `in-progress count` への依存禁止 — `rg -ni 'in-progress count|in_progress_count' internal/tcc2` → **期待 0 件**（実測 F4 で出力に存在しないことを確認済み）
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/tcc2` → **期待 0 件**（`constants.go` 自身と `*_test.go` は対象外）
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/tcc2` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: 本 Process は `config.yml` を扱わないが、`ResolveAuthStatus` が扱う `tcc2 status` の出力にはメールアドレスが含まれる（実測 F10）。ログ・status に載せない義務を D-06 grep で担保する。
  - **error**: 「解析失敗」と「実行中タスクなし」を型レベルで区別する。`err != nil` の有無だけで呼び出し側が判別できる設計にし、フィールドのゼロ値判定に依存させない。エラーは小文字スネークケースの code（`tcc2_api_error`/`empty_content`/`unrecognized_format`/`too_many_running_tasks`）を持ち、メッセージに機密情報を含めない。
  - **validation**: 本 Process には設定検証はないが、ID ブロックの形式不一致（`task_id_format`）や未知キー（`unknown_id_key:*`）は `warnings` に積んで処理を継続する（打ち切らない）。
  - **命名規約**: Go は標準的な camelCase/PascalCase。エクスポートは PascalCase。定数は `internal/constants/constants.go` に集約。
  - **stdout はプロトコル専用**、診断ログは stderr（本 Process は子プロセスの stdio を直接扱うが、親プロセス自身の stdout には書き込まない）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) core に列挙した更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

`get_taskchute` の MCP 応答は `content:[{type:"text",text:...}]` のみでテキスト解析以外の経路がない（実測 F1）。本 Process は「実測事実」節に列挙した F1〜F11 に矛盾しない実装のみを許容する仕様であり、特に F4（`in-progress count` という行は存在しない）と R1（形式変更で「実行中0件」に静かに誤判定するリスク）への対策として、`unrecognized_format` センチネル判定を最重要要素として実装する。

解析は右アンカー方式（行末から ID ブロック→メタ括弧→名前の順に剥がす）を採用する。タスク名に全角括弧・鍵括弧・`[` を含む行が実測で存在するため、左からの `Contains`/`Split` は必ず壊れる。`InProgressLinePrefix` はバイトオフセット0からの完全一致のみを判定基準とし、`strings.Contains` や `TrimSpace` 後の緩い一致は用いない。

「実行中タスクなし」（`- [` 行はあるが In Progress 行が0件）と「解析失敗」（`- [` 行そのものが0件、`isError:true`、空応答、暴走的な行数超過）は型レベルで区別する。判定1〜4は `(TaskChuteResult{}, error)` を、判定5〜6は `(TaskChuteResult{...}, nil)` を返し、呼び出し側は `err != nil` の有無だけで分岐できる。

MCP セッションは毎サイクル起動→終了する設計（常駐しない）とし、既存 `probe.go` のコードは一切変更・共有しない。`taskchute.go` は独立した実装として新規に書く（`probe.go` は「一発起動して一発診断して終わる」設計、`taskchute.go` は「セッションを開いて複数回 Call する」設計でライフサイクルが根本的に異なるため）。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/tcc2/taskchute.go` | 新規 | `Session`/`Open`/`Session.Call`/`Session.Close`/`FetchTaskChute`/`FetchUser`/`UserInfo`/`ToolResult` |
| `internal/tcc2/parse.go` | 新規 | `RunningTask`/`TaskChuteResult`/`ParseError`/`ParseTaskChuteText`/`parseUserText`/`splitIDBlock`/`splitMetaGroup` |
| `internal/tcc2/version.go` | 新規 | `ResolveCLIVersion`/`ResolveAuthStatus` |
| `internal/tcc2/parse_test.go` | 新規 | `ParseTaskChuteText`/`parseUserText` の正常系・異常系（6分岐の網羅） |
| `internal/tcc2/taskchute_test.go` | 新規 | `Session`/`FetchTaskChute`/`FetchUser` の正常系・異常系 |
| `internal/tcc2/testdata/get_taskchute_sample.txt` | 新規 | 実測フォーマットに準拠した黄金 fixture（Done/In Progress/Todo 混在） |
| `internal/tcc2/testdata/get_user_sample.txt` | 新規 | `get_user` markdown 太字形式の fixture |
| `internal/tcc2/testdata/get_taskchute_error.json` | 新規 | `isError:true` の MCP エラー応答 fixture |
| `internal/constants/constants.go` | 追記 | P02 の解析系定数を追加（P01 が定義した定数への追記のみ。既存行は変更しない） |

## Symbol Targets

```yaml
file: internal/tcc2/taskchute.go
symbols:
  - {name: Session, kind: struct, body_start_line: 1, body_end_line: 15, line_hint: 1}
  - {name: Open, kind: func, body_start_line: 1, body_end_line: 35, line_hint: 20}
  - {name: Session.Call, kind: method, body_start_line: 1, body_end_line: 30, line_hint: 60}
  - {name: Session.Close, kind: method, body_start_line: 1, body_end_line: 10, line_hint: 95}
  - {name: FetchTaskChute, kind: func, body_start_line: 1, body_end_line: 40, line_hint: 110}
  - {name: FetchUser, kind: func, body_start_line: 1, body_end_line: 25, line_hint: 155}
  - {name: UserInfo, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 185}
  - {name: ToolResult, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 195}
patch_only: false
disjoint_guarantee: true
disjoint_guarantee_evidence: "新規3ファイル（taskchute.go/parse.go/version.go）のみを追加。既存 internal/tcc2/probe.go の全シンボルを一切変更しないことが evidence。P02-VG-05 の git diff --stat が空出力であることで機械照合する。"
pre_flight_checks: [git_clean, "symbol_exists:internal/tcc2/probe.go:Probe", go_build_ok]
---
file: internal/tcc2/parse.go
symbols:
  - {name: RunningTask, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 1}
  - {name: TaskChuteResult, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 15}
  - {name: ParseError, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 27}
  - {name: ParseTaskChuteText, kind: func, body_start_line: 1, body_end_line: 80, line_hint: 40}
  - {name: parseUserText, kind: func, body_start_line: 1, body_end_line: 30, line_hint: 125}
  - {name: splitIDBlock, kind: func, body_start_line: 1, body_end_line: 25, line_hint: 160}
  - {name: splitMetaGroup, kind: func, body_start_line: 1, body_end_line: 20, line_hint: 190}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/tcc2/version.go
symbols:
  - {name: ResolveCLIVersion, kind: func, body_start_line: 1, body_end_line: 20, line_hint: 1}
  - {name: ResolveAuthStatus, kind: func, body_start_line: 1, body_end_line: 30, line_hint: 25}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/constants/constants.go
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "P01 が定義済みの定数への追記のみ（新規定数の追加）。既存行を変更しないため P01 と衝突しない。"
pre_flight_checks: [git_clean]
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P02-VG-01 | red | test | agent | true | `go test ./internal/tcc2 -run TestParseTaskChuteText -count=1` | P02 の task_delta | exit != 0 | GoalEvidence（exit_code と該当行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04 | TEST-RED,SCOPE-01,DONT-01 |
| P02-VG-02 | green | test | agent | true | `go test ./internal/tcc2 -race -count=1` | P02 の task_delta | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04 | TEST-GREEN,SCOPE-01,DONT-01 |
| P02-VG-03 | green | conformance | agent | true | `go test ./internal/tcc2 -run 'TestParse_(NoRunningTask\|UnrecognizedFormat)' -v` | P02 の task_delta | exit == 0 かつ2ケースとも PASS（「実行中なし」と「形式不明」が別分岐であることの照合） | GoalEvidence（PASS 行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04 | TEST-GREEN,SCOPE-01,DONT-01 |
| P02-VG-04 | refactor | grep | agent | true | `rg -ni 'in-progress count' internal/` | P02 の task_delta | ヒット == 0 | GoalEvidence（`rg` の出力有無を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04 | TEST-GREEN,SCOPE-01,DONT-01 |
| P02-VG-05 | refactor | conformance | agent | true | `git diff --stat internal/tcc2/probe.go` | P02 の task_delta | 出力が空（probe.go 無改変） | GoalEvidence（コマンド出力の有無を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04 | TEST-GREEN,SCOPE-01,DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 02
- gate_ids: [P02-VG-01, P02-VG-02, P02-VG-03, P02-VG-04, P02-VG-05]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-02.appendix.md（実行時に Read しない）

## Implementation Notes

**実測事実（推論でなく実測。これに矛盾する実装をしない）**:
- **F1**: `get_taskchute` の result は `content:[{type:"text",text:...}]` のみ。`structuredContent` は存在せず `outputSchema` は null。テキスト解析以外の経路はない。
- **F2**: タスク行の実形式 = `- [<Status>] <タスク名> (<HH:MM>[, Est: <dur>][, Act: <dur>]) [ID: <task_id>[, Section: <id>][, Project: <id>][, Mode: <id>][, Routine: <id>][, Tags: <…>]]`。実測例 `- [In Progress] openclaw / モデル修正 (15:46, Est: 15m) [ID: task_1ed58fc0173948c6a3051d7c37aaaa6a, Section: sec_…]`。行頭はバイトオフセット0から `- [`。タスク名に全角括弧・鍵括弧を含む行が実在（例: `- [Done] XXXX（ ；´ワ ｀；） (99:99, …)`）→ 左から `(` を探す実装は必ず壊れる。`]` の直後にさらに空白があり名前の一部として意味を持つ行が実在（`- [Done]  ------------------- XXXX (…)`）→ 左トリム禁止。
- **F3**: 状態タグ実測集合 = Done:111 / In Progress:1 / Todo:4。
- **F4**: `in-progress count: 1` という行は応答に存在しない。RESEARCH.md §4.2 の記述は調査者の集計値。`Progress` を含む行は2行のみで、うち1行は `- [Done] progress整理 @routine (15:26, Est` というタスク名に progress を含む Done 行（部分文字列マッチの誤検知源）。
- **F5**: 見出しは3種 — `## <語> <語> YYYY-MM-DD`（2日範囲クエリで2回出現）/ `### <セクション名>` / `### HHMM-HHMM (HH:MM-HH:MM)`。空行が34行ある。
- **F6**: ID ブロックのキー = 先頭はキー名を持たない位置引数（`task_` + 32桁小文字16進）、`Section`:116（全行）、`Project`:79、`Mode`:85、`Routine`:85、`Tags`:2。Section 以降の値の形は仮定せず文字列としてそのまま保持する。
- **F7**: 不正日付時 `isError: True`、text は `Error: API error (HTTP 400): {...}`。正常時は `isError` キー自体が存在しない。
- **F8**: `get_user` は `- **<ラベル>:** <値>` の markdown 太字形式。`Timezone: Asia/Tokyo`、`Start of Day: -05:00:00`、`Default View ID: view_<hex32>`、`Email` が返る（ログ・status に載せない義務）。
- **F10**: `tcc2 status` → `Logged in as: <EMAIL>` / `Token expires at: 2026-08-07 16:14:32` / exit=0。メールアドレスは捨てる。
- **F11**: `readlink -f /opt/homebrew/bin/tcc2` → `/opt/homebrew/Cellar/tcc2/0.0.21/bin/tcc2`。`tcc2 --version` は存在しない（`Error: unknown flag: --version`）。CLI版の取得は `CellarVersionPattern` の第1捕獲。Homebrew 以外の配置では `""` を返しフェッチ失敗にしない（診断値であって制御入力ではない）。

**解析仕様（完全版）**:

行頭リテラル `InProgressLinePrefix = "- [In Progress] "`（16バイト）。行末の `\r` を1個だけ除去した後、バイトオフセット0から完全一致。`strings.TrimSpace` してからのマッチは禁止。`strings.Contains(line,"In Progress")` は禁止。

```
// Why: 実測 F4 で `- [Done] progress整理 …` が存在。部分一致や "Progress" 検索は
// この Done 行を実行中タスクとして誤検出する。
```

`- [` で始まるが In Progress でない行は、読み飛ばす前に `StatusCounts[tag]++` する（形式変更検知に使う）。

**剥がし手順（右アンカー方式）**。前提: `rest = line[16:]`

手順A — ID ブロック切り出し: `rest` が `]` で終わるなら `i = strings.LastIndex(rest, " [ID: ")`（最後の出現）。`i>=0` なら `idBlock = rest[i+6:len(rest)-1]`, `rest = rest[:i]`。そうでなければ `idBlock=""`。

```
// Why: LastIndex（右から）を使う。タスク名に `[` を含む行が実在するため左から探すと
// 名前中の `[` を ID ブロック開始と誤認する。ID ブロックは必ず行末なので右から探せば一意。
```

手順B — ID ブロック分解: `parts = strings.Split(idBlock, ", ")`。`parts[0]` が位置引数=task_id。`TaskIDPattern` 不一致なら `task_id=""` かつ `warnings += "task_id_format"`（ここで失敗させない。名前一致によるルール評価は続行できる）。`parts[1:]` は `strings.Cut(p, ": ")` で分解し Section/Project/Mode/Routine を取得、Tags は捨てる、未知キーは `warnings += "unknown_id_key:"+k`。

手順C — メタ括弧切り出し: `rest` が `)` で終わるなら `i = strings.LastIndex(rest, " (")`。`meta = rest[i+2:len(rest)-1]` が `MetaHeadPattern` に一致する場合のみ `start = meta[:5]`, `metaRaw = meta`, `rest = rest[:i]`。一致しなければ `rest` を変更しない（その括弧はタスク名の一部）。

```
// Why: 括弧を無条件に剥がさず中身の形で判定する。実測でタスク名自体が括弧を含む行が
// 多数あるため、判定しないと名前を削り取る。Est/Act は文字列 metaRaw として保持するのみ
// で時間へ変換しない。
```

手順D — 名前確定: `name = strings.TrimRight(rest, " ")`（TrimRight のみ。TrimLeft/TrimSpace は禁止）。空なら `warnings += "empty_name"`。`MaxTaskNameBytes`(512) でルーン境界切り詰め。

```
// Why: 実測で `] ` の直後にさらに空白があり名前の一部として意味を持つ行が存在。左トリム
// すると task_name_contains の照合結果が変わる。
```

手順E — 日付帰属: `DateHeaderPattern` に一致した行で currentDate を更新し各エントリに付与。見出しより前の行は `date=""`。

**「実行中なし」と「解析失敗」の分離判定**（本仕様で最重要）:

| 判定順 | 条件 | 結果 | 後段 |
|---|---|---|---|
| 1 | `result.isError == true` | `ParseError{code:"tcc2_api_error"}` | フェッチ失敗。猶予カウンタ+1。parse_ok=false。running_tasks は前回値を保持（空にしない） |
| 2 | type=="text" の要素が0件、または連結後 text=="" | `ParseError{code:"empty_content"}` | 同上 |
| 3 | 非空行が1行以上あるが `- [` で始まる行が0件 | `ParseError{code:"unrecognized_format"}` | 同上。表示文言変更の検知センチネル |
| 4 | In Progress 行が `MaxRunningTasks`(32) 超 | `ParseError{code:"too_many_running_tasks"}` | 同上 |
| 5 | `- [` 行が1件以上あり In Progress 行が0件 | 正常 `TaskChuteResult{RunningTasks:[]}`, error==nil | 「実行中タスクなし」として通常評価。parse_ok=true |
| 6 | 上記以外 | 正常 `TaskChuteResult{RunningTasks:[...]}`, error==nil | 通常評価 |

型レベルでの区別: 判定1〜4は `(TaskChuteResult{}, error)` を返し、判定5〜6は `(TaskChuteResult{...}, nil)` を返す。呼び出し側が `err != nil` を見るだけで区別できる設計にする。フィールドのゼロ値判定に依存させない。

```
// Why: 判定3がないと、tcc2 の表示文言が変わったとき「毎回実行中タスク0件」と静かに
// 誤判定し、not_contains 系ルールが全て有効になって利用者のアプリが意図せず終了させら
// れる。Done/Todo 行が1つでもあれば「フォーマットは生きている」と言えるのでカナリアに
// 使う。実測で Done 111/Todo 4 行あり、通常の1日で0件になることは事実上ない。
```

**複数件時**: 全件を出現順で返す（並べ替えない）。上限32。ルール評価は全件を条件へ渡す。マッチ意味論: `task_name_contains` = いずれかの実行中タスク名がいずれかの部分文字列を含めば真（OR×OR）、`task_name_not_contains` = どのタスク名もどの部分文字列も含まなければ真（AND-NOT）。

**日付範囲クエリ（2日範囲）**: リクエスト `{"name":"get_taskchute","arguments":{"start_date":"<d-1>","end_date":"<d>","view_id":"<config の view_id が非nullのときのみ>"}}`。

`d` の算出: `tz` = get_user の Timezone、`startOfDayOffset` = Start of Day を秒に変換（`-05:00:00` → 18000秒）、`d = time.Now().In(tz).Add(-startOfDayOffset).Format("2006-01-02")`、`d-1` = そこから24時間引いた日付。get_user 取得失敗時のフォールバック: `tz=time.Local`, `startOfDayOffset=0`, `warnings += "user_info_fallback"`。フェッチ失敗にしない。

```
// Why: 単日 [d,d] でなく2日範囲 [d-1,d] にする。理由1: `Start of Day: -05:00:00` の符号
// 解釈が実測から一意に決まらず、誤ると 00:00-05:00 帯で静かに取りこぼす（エラーになら
// ない失敗モード）。2日範囲なら解釈を誤っても取りこぼさない。理由2: RESEARCH.md §4.2 で
// 前日〜当日範囲の動作を実測済み。理由3: コストは191行/2日（100KiB未満）で上限1MiBに
// 対し十分な余裕。
```

日付の再計算タイミング: ①各ポーリング tick の冒頭 ②スリープ復帰検知時（get_user キャッシュも破棄して再取得）③`UserInfoTTLSeconds`(6h) 経過時。

**MCP セッション設計**: `exec.CommandContext(ctx, executable, "mcp")` で起動し、`initialize` → `notifications/initialized` → `tools/call` を stdio 上で交換。`cmd.WaitDelay = MCPWaitDelaySeconds`(2s)、応答上限 `MCPMaxResponseBytes`(1MiB)、stderr は `MCPStderrLimitBytes`(4096) で切り捨て。毎サイクル起動→終了する（常駐しない）。

```
// Why: probe.go の exchange/readResponse をリファクタして共有せず taskchute.go に同等の
// 実装を新規に書く。probe.go は「一発起動して一発診断して終わる」設計、taskchute.go は
// 「セッションを開いて複数回 Call する」設計でライフサイクルが根本的に違う。共有化する
// と probe.go を壊すリスクを負う一方、共有できるのは30行程度の JSON 読み書きだけで
// 割に合わない。
// Why: MCP を都度起動にする。トークン更新時の長時間起動 MCP サーバーの挙動という不確実
// 性ごと消せる。リーク源にならず、異常時は次周期で自然回復する。
```

**CLI 版・認証状態の解決**: `ResolveCLIVersion` は `readlink -f` 相当（`filepath.EvalSymlinks`）で実体パスを得て `CellarVersionPattern` の第1捕獲を返す。不一致なら `""`。`ResolveAuthStatus` は `tcc2 status` を実行し exit==0 かつ `Logged in as: ` 行の存在で `authenticated:true`。`Token expires at:` はローカル時刻形式 `YYYY-MM-DD HH:MM:SS`。メールアドレスは捨てる（D-06 の grep で担保。`"Logged in as"` は判定にのみ使い値を保持しない）。

## Behavior Specification
System Type: both（Session は子プロセス起動を伴う reactive、ParseTaskChuteText/parseUserText は純関数の transformation）

### Transformation（ParseTaskChuteText / parseUserText / ResolveCLIVersion）

| behavior_id | 入力 | 出力 | pre_state | post_state | invariants | test_ref |
|---|---|---|---|---|---|---|
| BEH-02-01 | `get_taskchute_sample.txt`（Done/In Progress/Todo 混在、実測相当） | `(TaskChuteResult{RunningTasks:[1件]}, nil)` | — | `StatusCounts` に Done/In Progress/Todo すべて計上 | 判定6（正常） | `TestParseTaskChuteText_Golden` |
| BEH-02-02 | `- [` 行はあるが In Progress 行が0件のテキスト | `(TaskChuteResult{RunningTasks:[]}, nil)` | — | `error==nil` | 判定5。「実行中なし」は正常系 | `TestParse_NoRunningTask` |
| BEH-02-03 | `- [` 行が1件も無いテキスト（非空） | `(TaskChuteResult{}, error{code:unrecognized_format})` | — | `error!=nil` | 判定3。表示文言変更のセンチネル | `TestParse_UnrecognizedFormat` |
| BEH-02-04 | `get_taskchute_error.json` 相当（`isError:true`） | `(TaskChuteResult{}, error{code:tcc2_api_error})` | — | `error!=nil` | 判定1 | `TestParse_APIError` |
| BEH-02-05 | text 要素が0件 or 連結後空文字列 | `(TaskChuteResult{}, error{code:empty_content})` | — | `error!=nil` | 判定2 | `TestParse_EmptyContent` |
| BEH-02-06 | In Progress 行が33件（上限超） | `(TaskChuteResult{}, error{code:too_many_running_tasks})` | — | `error!=nil` | 判定4 | `TestParse_TooManyRunningTasks` |
| BEH-02-07 | タスク名に `[` と全角括弧を含む In Progress 行 | 名前が全角括弧・`[` を含んだまま保持される | — | ID ブロックは正しく右アンカーで分離 | 左からの `Contains`/`Split` を使わない | `TestParse_BracketInName` |
| BEH-02-08 | `] ` の直後に追加の空白があるタスク名の行 | 名前の先頭空白が保持される | — | `TrimRight` のみ適用 | 左トリム禁止 | `TestParse_LeadingSpacePreserved` |
| BEH-02-09 | ID ブロックの task_id が `TaskIDPattern` 不一致 | `task_id=""` かつ `warnings` に `task_id_format` を含む | — | 解析全体は失敗しない | 名前一致評価は継続可能 | `TestParse_InvalidTaskIDFormat` |
| BEH-02-10 | ID ブロックに未知キー `Foo: bar` を含む | `warnings` に `unknown_id_key:Foo` を含む | — | 解析全体は失敗しない | — | `TestParse_UnknownIDKey` |
| BEH-02-11 | `get_user_sample.txt`（Timezone/Start of Day/Email 等を含む） | `UserInfo{Timezone:"Asia/Tokyo", StartOfDay:"-05:00:00", ...}` | — | Email フィールドは構造体に保持されるが呼び出し側でログに出さない契約 | `UserFieldPattern` で全フィールド分解 | `TestParseUserText_Golden` |
| BEH-02-12 | `readlink -f` 相当が `/opt/homebrew/Cellar/tcc2/0.0.21/bin/tcc2` を返す環境 | `ResolveCLIVersion` が `"0.0.21"` を返す | — | — | `CellarVersionPattern` の第1捕獲 | `TestResolveCLIVersion_Homebrew` |
| BEH-02-13 | Homebrew 以外の配置パス | `ResolveCLIVersion` が `""` を返し error は発生しない | — | フェッチ失敗にしない（診断値） | — | `TestResolveCLIVersion_NonHomebrew` |

### Reactive（Session / FetchTaskChute / FetchUser の外部依存失敗分岐）

| behavior_id | 現状態 | イベント | ガード | 次状態 | 事後条件 | test_ref |
|---|---|---|---|---|---|---|
| BEH-02-14 | Session 未起動 | `Open(ctx, executable, args)` 呼び出し | executable が実行可能 | Session 起動済み（initialize/initialized 完了） | `Session.Call` が使用可能 | `TestOpen_Success` |
| BEH-02-15 | Session 未起動 | `Open` 呼び出し | executable が存在しない/実行不可 | Session 生成失敗 | `error` を返し子プロセスは残らない | `TestOpen_ExecutableNotFound` |
| BEH-02-16 | Session 起動済み | `Call` 呼び出し中に応答が `MCPMaxResponseBytes` 超過 | — | Session はエラー状態 | `error` を返す。応答は破棄されメモリに保持しない | `TestCall_ResponseTooLarge` |
| BEH-02-17 | Session 起動済み | `Call` がタイムアウト（`ctx` キャンセル） | `cmd.WaitDelay=MCPWaitDelaySeconds` 経過 | Session は `Close` 可能な状態 | 子プロセスがリークしない（`TestTenThousandCycles` 相当の縮小版で確認） | `TestCall_ContextTimeout` |
| BEH-02-18 | `FetchTaskChute` 呼び出し前 | get_user キャッシュが `UserInfoTTLSeconds` 未経過 | キャッシュ有効 | キャッシュを再利用 | `get_user` を呼ばない | `TestFetchTaskChute_UsesCachedUserInfo` |
| BEH-02-19 | `FetchTaskChute` 呼び出し前 | get_user キャッシュが `UserInfoTTLSeconds` 経過 | TTL 切れ | 新規に `get_user` を呼ぶ | キャッシュが更新される | `TestFetchTaskChute_RefreshesExpiredUserInfo` |
| BEH-02-20 | `FetchTaskChute` 呼び出し前 | `get_user` 自体が失敗 | — | `tz=time.Local`, `startOfDayOffset=0` にフォールバック | `warnings` に `user_info_fallback` を含み `FetchTaskChute` はフェッチ失敗にしない | `TestFetchTaskChute_UserInfoFallback` |
| BEH-02-21 | Session 起動済み・毎サイクル | サイクル完了 | — | Session は必ず `Close` される（常駐しない） | 次サイクルで新規 Session が起動される | `TestFetchTaskChute_ClosesSessionPerCycle` |

### Correctness Criteria（観測可能・固定する）
- `unrecognized_format` は「非空行が1行以上あるが `- [` 行が0件」の場合にのみ発生し、「`- [` 行はあるが In Progress が0件」では発生しない（BEH-02-02 と BEH-02-03 の分岐が独立していることを `P02-VG-03` が照合）。
- `err != nil` の判定だけで「フェッチ失敗」と「正常（実行中0件含む）」を区別できる（`RunningTasks` の長さや内容のゼロ値には依存しない）。
- `internal/tcc2/probe.go` は本 Process の変更後も `git diff --stat` が空である（`P02-VG-05`）。
- `in-progress count` という文字列パターンに一切依存しない（`P02-VG-04`）。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `splitIDBlock`/`splitMetaGroup` 内部のローカル変数名（`rest`/`idBlock`/`meta` 等の命名は例示であり厳密な変数名は実装者裁量）。
- JSON-RPC のリクエスト ID 採番方式（連番かランダムか）。
- stderr 切り捨てバッファの内部実装（リングバッファか単純スライスか）。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（上記3点以外はすべて本文中で確定済み）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/tcc2/testdata/` に3つの fixture（`get_taskchute_sample.txt`/`get_user_sample.txt`/`get_taskchute_error.json`）を実測フォーマットに準拠して作成
- [x] `internal/tcc2/parse_test.go` に `TestParseTaskChuteText`（未実装の `ParseTaskChuteText` を呼ぶ最小ケース）を作成。テストケースに docblock を付与し正常系（BEH-02-01/02/06相当）／異常系（BEH-02-03/04/05相当）に分類し、各メソッドが何を担保するかを記述
- [x] `internal/tcc2/taskchute_test.go` に `Session`/`FetchTaskChute`/`FetchUser` の骨格テストを作成
- [x] `go test ./internal/tcc2` を実行してテスト実行結果を確認する

✅ **Phase Complete**（GoalEvidence）
- gate_id: P02-VG-01 / status: / command_or_action: `go test ./internal/tcc2 -run TestParseTaskChuteText -count=1` / exit_code: / expected: exit!=0 / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/constants/constants.go` に P02 の解析系定数を追記（P01 の既存行を変更しない）
- [x] `internal/tcc2/parse.go` の `RunningTask`/`TaskChuteResult`/`ParseError`/`ParseTaskChuteText`/`parseUserText`/`splitIDBlock`/`splitMetaGroup` を実装
- [x] `internal/tcc2/taskchute.go` の `Session`/`Open`/`Call`/`Close`/`FetchTaskChute`/`FetchUser`/`UserInfo`/`ToolResult` を実装
- [x] `internal/tcc2/version.go` の `ResolveCLIVersion`/`ResolveAuthStatus` を実装
- [x] Behavior Specification 表の全21行（transformation 13行 + reactive 8行）に対応する test_ref のテストが存在し PASS することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P02-VG-02 / status: / command_or_action: `go test ./internal/tcc2 -race -count=1` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P02-VG-03 / status: / command_or_action: `go test ./internal/tcc2 -run 'TestParse_(NoRunningTask|UnrecognizedFormat)' -v` / exit_code: / expected: 2ケースともPASS / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `in-progress count` 等の脆い文字列マッチが紛れ込んでいないか再確認（D-08 grep 再実行）
- [x] `probe.go` を誤って変更していないか再確認（`git diff --stat internal/tcc2/probe.go`）
- [x] メールアドレス等の秘密情報が構造体を越えてログ出力に漏れていないか確認（D-06 grep 再実行）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P02-VG-04 / status: / command_or_action: `rg -ni 'in-progress count' internal/` / exit_code: / expected: ヒット0件 / observed: / attempt:
- gate_id: P02-VG-05 / status: / command_or_action: `git diff --stat internal/tcc2/probe.go` / exit_code: / expected: 出力が空 / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: agent** — 操作: `tcc2` が未インストールの環境変数を模した実行可能ファイル不在パスで `Open` を呼ぶ → 期待される出力: `error` が返り Session は起動しない → 操作後のデータ状態の確認方法: プロセスリスト（`ps`）に子プロセスが残っていないことをテスト内で確認する。
2. **executor: human** — 操作: 実機で `tcc2 mcp` が実際に起動する環境で `FetchTaskChute` を1回実行し、実際の `[In Progress]` タスクと突き合わせる → 期待される出力: `RunningTasks` に実機の実行中タスク名が含まれる → 操作後のデータ状態の確認方法: `docs/macos-verification-<date>.md` に実測結果を記録する（PLAN-for-mac.md OQ-3 の 00:00–05:00 帯確認と合わせて実施可能）。
3. **executor: agent** — 操作: `get_taskchute_error.json` fixture を使い `isError:true` 応答を模擬して `FetchTaskChute` を呼ぶ → 期待される出力: `error{code:tcc2_api_error}` が返り前回の `RunningTasks` が保持される → 操作後のデータ状態の確認方法: 呼び出し前後で `RunningTasks` スライスの中身が変化していないことをテストでアサートする。

## Dependencies
- Requires: なし
- Blocks: P05, P11
