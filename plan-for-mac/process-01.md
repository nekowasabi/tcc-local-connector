# Process 1: YAML 設定スキーマと検証

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-01.md` を起動した際の自己完結ブリーフ。

- **背景**: TaskChute Cloud 2 連動 macOS 常駐アプリ MVP は `config.yml` 一枚を「実行中タスク → macOS 操作」の唯一の設定源とする。`config.yml` は「本人が書いたコード」と同等の力（起動するアプリ・実行するコマンド）を持つため、内容の正しさ以前に「このファイルを信用してよいか」を内容を読む前に判定する必要がある。
- **目的**: `config.yml` を strict decode（未知キー拒否）し、既定値展開・15種の検証規則・所有者/権限検証（TOCTOU 対策込み）を実装し、`len(errors)>0 ⇒ *Config==nil` を型レベルで保証して部分適用を物理的に不能にする。
- **変更範囲**: `internal/config/config.go`, `internal/config/validate.go`, `internal/config/permissions.go`, `internal/constants/constants.go`, `config.example.yml`, `go.mod`, `internal/config/config_test.go`, `internal/config/validate_test.go`
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |-------|-----|------|----------------------|
  | `ConfigSchemaVersion` | 2 | — | `version` フィールドの唯一の許容値 |
  | `ConfigRelPath` | ".config/tcc-local-connector/config.yml" | path | `DefaultPath()` の `$HOME` 相対解決先 |
  | `ConfigForbiddenModeMask` | 0o022 | mask | 段階1（所有者・権限違反）の group/other 書き込み判定 |
  | `ConfigStrictModeMask` | 0o077 | mask | `allow_shell:true` 時の追加権限要求 |
  | `MinPollIntervalSeconds` / `DefaultPollIntervalSeconds` / `MaxPollIntervalSeconds` | 10 / 60 / 3600 | seconds | `polling.interval_seconds` の範囲検証・既定値 |
  | `MinPollTimeoutSeconds` / `DefaultPollTimeoutSeconds` / `MaxPollTimeoutSeconds` | 1 / 20 / 120 | seconds | `polling.timeout_seconds` の範囲検証・既定値 |
  | `MinFailureGraceSeconds` / `DefaultFailureGraceSeconds` / `MaxFailureGraceSeconds` | 0 / 180 / 3600 | seconds | `polling.failure_grace_seconds` の範囲検証・既定値 |
  | `MaxRules` | 100 | count | `rules` 配列長の上限 |
  | `MaxActionsPerRule` | 20 | count | `ensure`/`on_enter`/`on_exit` 各配列長の上限 |
  | `MinActionTimeoutSeconds` / `DefaultActionTimeoutSeconds` / `MaxActionTimeoutSeconds` | 1 / 30 / 300 | seconds | `command.run.timeout_seconds` |
  | `MinGraceSeconds` / `DefaultAppStopGraceSeconds` / `DefaultProcessStopGraceSeconds` / `MaxGraceSeconds` | 1 / 10 / 10 / 120 | seconds | `app.stop`/`process.stop` の `grace_seconds` |
  | `NotifyTitleMaxRunes` / `NotifyMessageMaxRunes` | 200 / 500 | runes | `notify.title`/`notify.message` の長さ上限 |
  | `MinLogRetainDays` / `DefaultLogRetainDays` / `MaxLogRetainDays` | 1 / 14 / 365 | days | `logging.retain_days` |
  | `RuleIDPattern` / `ProcessIDPattern` | `^[a-z0-9][a-z0-9-]{0,63}$` | regexp | `rules[].id` / `process_id` の識別子検証 |
  | `BundleIDPattern` | `^[A-Za-z0-9][A-Za-z0-9._-]*$` | regexp | `bundle_id` の識別子検証 |
  | `EnvKeyPattern` | `^[A-Za-z_][A-Za-z0-9_]*$` | regexp | `process.start.env` のキー検証 |
  | `DefaultLogLevel` | "info" | — | `logging.level` の既定値 |

- **禁止事項**: 該当する Don'ts のみ抜粋。
  - D-03 シェル文字列連結の禁止 — `rg -n '"/bin/sh"|"-c"|bash -c|zsh -c|sh -c' internal/config` → **期待 0 件**（P01 は allow_shell ガードを持つコマンド実行そのものを実装しない）
  - D-06 秘密情報の出力禁止 — `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal/config` → **期待 0 件**
  - D-07 バンドルID・絶対パスの直書き禁止 — `rg -n 'com\.tinyspeck|com\.amazon\.Lassen|/opt/homebrew|/Users/takets' internal/config --glob '!**/testdata/**'` → **期待 0 件**
  - D-09 マジックナンバー直書き禁止 — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/config` → **期待 0 件**（`internal/constants/constants.go` 自身と `*_test.go` は対象外）
  - D-11 検証エラー時の部分適用禁止 — `pattern: n/a`（構造的性質のため grep 不能）。代替照合: `rg -c 'errors\s*\)\s*>\s*0' internal/config/config.go` が **1 件以上**、かつ `TestLoadInvalidReturnsNilConfig` が pass
  - D-12 TODO/FIXME 等の残存禁止 — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal/config` → **期待 0 件**

- **適用される横断方針（インライン展開）**:
  - **security**: `config.yml` は「本人が書いたコード」と同等に扱う。防御対象は本人以外による書き換えと本人の誤記。所有者 UID 一致・group/other 書き込み不可・通常ファイル・`O_NOFOLLOW` で fd を取り fd 経由で検証してから同じ fd から読む（TOCTOU 対策）。
  - **error**: 「解析失敗」と「実行中タスクなし」を型レベルで区別する（P01 では「権限違反」と「スキーマ違反」を型レベルで区別する: 前者は `RPCError`、後者は `errors []ValidationError`）。エラーは小文字スネークケースの code を持ち、メッセージに機密情報を含めない。
  - **validation**: 検証エラーは最初の1件で打ち切らず全件収集する。`len(errors)>0` なら `*Config` に必ず nil を返し、部分適用を型で防ぐ。
  - **命名規約**: Go は標準的な camelCase/PascalCase。エクスポートは PascalCase。定数は `internal/constants/constants.go` に集約。
  - **stdout はプロトコル専用**、診断ログは stderr（P01 自体は標準出力を扱わないが、後続 Process が呼び出す前提を崩さない）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) core に列挙した更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

`config.yml` は本アプリの唯一の設定入力であり、`task_source.executable`・`process.start.executable`・`command.run.executable` を通じて任意のバイナリを起動できる力を持つ。したがって検証は「内容が正しいか」の前段に「このファイルを信用してよいか」という所有者・権限検証を独立した段階として置く（2段階分岐）。

段階1（所有者・権限検証）はファイル内容を1バイトも読む前に、シンボリックリンク解決・通常ファイル確認・所有者 UID 一致・group/other 書き込み不可を `O_NOFOLLOW` で開いた fd 経由で判定する（TOCTOU 対策: 検証と読み取りを同一 fd で行う）。違反時は `errors[]` ではなく RPCError `insecure_permissions` を返し、内容の妥当性とは独立に拒否する。

段階2（パース・スキーマ検証）は YAML の strict decode（`yaml.Decoder.KnownFields(true)`）と 15 種の検証規則を実行する。検証エラーは最初の1件で打ち切らず全件収集し、`len(errors)>0` のときは `config.Load` が `*Config` に必ず `nil` を返す（型で部分適用を不能にする）。

本 Process は既存パッケージを一切変更しない新規パッケージ（`internal/config/`）であり、`go.mod` への `gopkg.in/yaml.v3` 追加も本 Process の責務とする。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| `internal/config/config.go` | 新規 | `Config` 構造体群（`Polling`/`TaskSource`/`Safety`/`Logging`/`Rule`/`Match`/`Action`）、`Load(path string) (*Config, []ValidationError, error)` 相当のエントリポイント、`DefaultPath() (string, error)` |
| `internal/config/validate.go` | 新規 | `ValidationError` 型、`Validate(*rawConfig) (*Config, []ValidationError)`、15種のルールごとの `validateXxx` ヘルパ |
| `internal/config/permissions.go` | 新規 | `CheckPermissions(path string) (*os.File, error)` — 所有者 UID・mode マスク・`O_NOFOLLOW` による fd 取得と検証 |
| `internal/constants/constants.go` | 新規 | P01 で使う定数群（上記ローカル定数表を実体化） |
| `config.example.yml` | 新規 | 動作するサンプル。bundle_id はプレースホルダ、絶対パスは `<YOUR_PATH>`（PLAN-for-mac.md Docs to Update 条件） |
| `go.mod` | 変更 | `gopkg.in/yaml.v3` を唯一の外部依存として追加（本 Process のみが編集） |
| `internal/config/config_test.go` | 新規 | `Load` の正常系・異常系テスト |
| `internal/config/validate_test.go` | 新規 | 15種の検証エラーコードを網羅する `TestValidationErrorCodes` |

## Symbol Targets

```yaml
file: internal/config/config.go
symbols:
  - {name: Config, kind: struct, body_start_line: 1, body_end_line: 40, line_hint: 1}
  - {name: Polling, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 10}
  - {name: TaskSource, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 25}
  - {name: Safety, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 40}
  - {name: Logging, kind: struct, body_start_line: 1, body_end_line: 10, line_hint: 50}
  - {name: Rule, kind: struct, body_start_line: 1, body_end_line: 15, line_hint: 60}
  - {name: Match, kind: struct, body_start_line: 1, body_end_line: 8, line_hint: 80}
  - {name: Action, kind: struct, body_start_line: 1, body_end_line: 20, line_hint: 90}
  - {name: Load, kind: func, body_start_line: 1, body_end_line: 40, line_hint: 120}
  - {name: DefaultPath, kind: func, body_start_line: 1, body_end_line: 15, line_hint: 165}
patch_only: false
disjoint_guarantee: true
disjoint_guarantee_evidence: "新規パッケージ internal/config/ 配下のみを編集。go.mod は本計画内で P01 のみが編集する制約（PLAN-for-mac.md Wave Progress Map W01 注記）。他の W01 内 4 Process（P02/P03/P04/P08）はこのファイル群を触らない。"
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/config/validate.go
symbols:
  - {name: ValidationError, kind: struct, body_start_line: 1, body_end_line: 6, line_hint: 1}
  - {name: Validate, kind: func, body_start_line: 1, body_end_line: 60, line_hint: 10}
  - {name: validateRule, kind: func, body_start_line: 1, body_end_line: 40, line_hint: 75}
  - {name: validateAction, kind: func, body_start_line: 1, body_end_line: 60, line_hint: 120}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/config/permissions.go
symbols:
  - {name: CheckPermissions, kind: func, body_start_line: 1, body_end_line: 45, line_hint: 1}
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean, go_build_ok]
---
file: internal/constants/constants.go
symbols:
  - {name: ConfigSchemaVersion, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 1}
  - {name: DefaultPollIntervalSeconds, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 5}
  - {name: MinPollIntervalSeconds, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 6}
  - {name: MaxPollIntervalSeconds, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 7}
  - {name: DefaultFailureGraceSeconds, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 10}
  - {name: MaxRules, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 15}
  - {name: MaxActionsPerRule, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 16}
  - {name: RuleIDPattern, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 20}
  - {name: BundleIDPattern, kind: const, body_start_line: 1, body_end_line: 1, line_hint: 21}
patch_only: false
disjoint_guarantee: true
disjoint_guarantee_evidence: "constants.go は新規ファイル。P01 が定義する定数群と P02 が定義する定数群（解析系）はキー名が重複しないため同一ファイルへの追記でも disjoint（PLAN-for-mac.md Conflict Matrix には現れないが、両 Process とも新規追加のみで既存行を変更しないため衝突しない）。"
pre_flight_checks: [git_clean]
---
file: config.example.yml
patch_only: false
disjoint_guarantee: true
pre_flight_checks: [git_clean]
---
file: go.mod
patch_only: false
disjoint_guarantee: true
disjoint_guarantee_evidence: "PLAN-for-mac.md Wave Progress Map W01 注記: go.mod は P01 のみが編集する制約。"
pre_flight_checks: [git_clean]
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P01-VG-01 | red | test | agent | true | `go test ./internal/config -run TestLoad -count=1` | P01 の task_delta | exit != 0 かつ出力に `undefined: config.Load` または `FAIL` を含む | GoalEvidence（exit_code と該当行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-RED,SCOPE-01,DONT-01 |
| P01-VG-02 | green | test | agent | true | `go test ./internal/config -race -count=1` | P01 の task_delta | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-GREEN,SCOPE-01,DONT-01 |
| P01-VG-03 | green | conformance | agent | true | `go test ./internal/config -run TestValidationErrorCodes -v` | P01 の task_delta | 出力に検証エラーコード15種すべてが1回以上出現（`rg -c` で 15） | GoalEvidence（`rg -c` の出力数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-GREEN,SCOPE-01,DONT-01 |
| P01-VG-04 | refactor | grep | agent | true | `rg -c 'errors\s*\)\s*>\s*0' internal/config/config.go` | P01 の task_delta | ヒット >= 1（`len(errors)>0 ⇒ cfg==nil` の実装存在） | GoalEvidence（`rg -c` の出力数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-10 | TEST-GREEN,SCOPE-01,DONT-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 01
- gate_ids: [P01-VG-01, P01-VG-02, P01-VG-03, P01-VG-04]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-01.appendix.md（実行時に Read しない）

## Implementation Notes

**config.yml スキーマ（完全版）**は以下（既定値・型・範囲を含む）。

```yaml
version: 2                      # int 必須。== 2 のみ。他は unsupported_config_version

polling:                        # object 任意 既定{}
  interval_seconds: 60          # int 任意 既定60  10<=v<=3600
  timeout_seconds: 20           # int 任意 既定20  1<=v<=120 かつ v < interval_seconds
  failure_grace_seconds: 180    # int 任意 既定180 0<=v<=3600
  failure_policy: release_controls  # string 任意 既定release_controls。enum: release_controls のみ

task_source:                    # object 必須
  type: tcc2_mcp                # string 任意 既定tcc2_mcp。enum: tcc2_mcp のみ
  executable: /opt/homebrew/bin/tcc2  # string 必須。絶対パス・存在・実行ビット必須
  args: [mcp]                   # []string 任意 既定["mcp"]。各要素は非空
  view_id: null                 # string|null 任意 既定null。null なら get_taskchute に view_id を付与しない

safety:                         # object 任意 既定{}
  dry_run: false                # bool 任意 既定false
  allow_shell: false            # bool 任意 既定false。true なら設定ファイルに mode&0o077==0 を追加要求
  allow_external_process_control: false  # bool 任意 既定false。true は検証エラー forbidden_safety_flag
  allow_force_terminate: false  # bool 任意 既定false。true は検証エラー forbidden_safety_flag

logging:                        # object 任意 既定{}
  level: info                   # string 任意 既定info。enum: debug|info|warn|error
  retain_days: 14               # int 任意 既定14。1<=v<=365

rules:                          # []Rule 任意 既定[]。要素数 <= MaxRules(100)
  - id: rss-mode                # string 必須。^[a-z0-9][a-z0-9-]{0,63}$、全ルール横断で一意
    priority: 100               # int 任意 既定0。0<=v<=1000
    match:                      # object 任意 既定{}。省略/空なら常に一致
      task_name_contains: [RSS]      # []string 任意 既定[]。各要素非空。リスト内 OR
      task_name_not_contains: []     # []string 任意 既定[]。各要素非空。AND-NOT
    ensure: []                  # []Action 任意 既定[]。要素数 <= MaxActionsPerRule(20)
    on_enter: []                # 同上
    on_exit: []                 # 同上
```

**アクション種別6種**:
- `app.start`: `bundle_id`(string 必須, `BundleIDPattern`)
- `app.stop`: `bundle_id`(同上), `grace_seconds`(int 任意 既定`DefaultAppStopGraceSeconds`, `MinGraceSeconds`..`MaxGraceSeconds`)
- `process.start`: `process_id`(string 必須, `ProcessIDPattern` 全ルール横断で一意), `executable`(string 必須 絶対パス・存在・実行ビット), `args`([]string 任意 既定[]), `working_dir`(string 任意 既定"" 非空なら絶対パス・存在するディレクトリ), `env`(map[string]string 任意 既定{} キーは `EnvKeyPattern`)
- `process.stop`: `process_id`(必須 同一設定内の `process.start` に存在する id), `grace_seconds`(任意 既定`DefaultProcessStopGraceSeconds`, `MinGraceSeconds`..`MaxGraceSeconds`)
- `command.run`: `executable`(必須 絶対パス・存在・実行ビット), `args`(任意 既定[]), `timeout_seconds`(任意 既定`DefaultActionTimeoutSeconds`, `MinActionTimeoutSeconds`..`MaxActionTimeoutSeconds`), `shell`(bool 任意 既定false。true は `allow_shell:true` 必須。違反は `shell_not_allowed`)
- `notify`: `title`(string 必須 1..`NotifyTitleMaxRunes`文字), `message`(string 必須 1..`NotifyMessageMaxRunes`文字), `level`(任意 既定info, enum info|warn|error)
- `browser.redirect` は検証エラー `unsupported_action`（静かに無視せず明示拒否）

**検証エラーコード15種**（`P01-VG-03` が照合する一覧）:
1. `unsupported_config_version` — version != 2
2. `missing_required_field` — task_source / task_source.executable / rules[].id / アクション必須キー欠落
3. `unknown_field` — `yaml.Decoder.KnownFields(true)` が検出した未知キー
4. `unknown_action_type` — type が6種の enum 外
5. `unsupported_action` — type == "browser.redirect"
6. `duplicate_rule_id` — rules[].id の重複
7. `duplicate_process_id` — process.start の process_id 重複
8. `value_out_of_range` — 各数値・文字列長の範囲外。rules/actions の上限超過も含む
9. `timeout_exceeds_interval` — timeout_seconds >= interval_seconds
10. `unsupported_value` — failure_policy != release_controls / type != tcc2_mcp / level enum 外
11. `executable_not_found` — 非絶対パス / 不存在 / 実行ビットなし / ディレクトリ
12. `invalid_identifier` — bundle_id / rules[].id / process_id / env キーがパターン不一致
13. `shell_not_allowed` — shell:true かつ allow_shell:false
14. `forbidden_safety_flag` — allow_force_terminate:true または allow_external_process_control:true
15. `rule_conflict_same_priority` — 同一 priority の2ルールが同一 bundle_id に app.start と app.stop（または同一 process_id に process.start と process.stop）を指定

エラーオブジェクト: `{"code":"duplicate_rule_id","path":"rules[1].id","message":"…"}`。path はドット/添字記法（例: `rules[1].ensure[0].bundle_id`）。

**検証失敗時の2段階分岐**（重要）:

段階1 — 所有者・権限違反（内容を1バイトも読まない）: ①`EvalSymlinks` でリンク解決 ②`Lstat` で通常ファイル確認 ③`stat.Uid != os.Getuid()` なら拒否 ④`mode & 0o022 != 0` なら拒否 ⑤内容を読んだ後 `allow_shell==true` が判明した場合 `mode & 0o077 != 0` なら**設定を破棄して**拒否。
→ RPCError `insecure_permissions` を返す。`errors[]` は使わない。起動時は `state=config_error` でポーリングもアクションも行わない。

```
// Why: 権限違反を errors[] でなく RPCError にする。errors[] は「設定内容の問題」で
// 利用者が編集すれば解決するが、権限違反は「このファイルを信用してよいか」の問題で
// 内容を読む前に止まる。両者を同じ配列に混ぜると「読んだ上での判定」と誤解される。
```

段階2 — パース・スキーマ違反: YAML パース失敗 → `errors=[{code:"parse_error",path:"",message:"…"}]`。スキーマ違反 → 15種を**全件収集**（最初の1件で打ち切らない）。
→ `{ok:false, applied:false, errors:[…全件…]}`。

**適用ポリシー**: 起動時に権限違反/スキーマ違反/ファイル不在 → `state=config_error`（**空設定での既定動作をしない**）。`reload_config` で違反 → **直前の有効設定を維持**、state 不変、`notify{config_invalid}`。reload 成功 → 新設定適用、`state→fetching`、即時サイクル、**消滅したルールの `on_exit` を1回だけ発火**（実際の発火は P05/P06 の責務。P01 は `Config` の再読込み契約のみを保証する）。

```
// Why: 起動時にファイル不在なら空設定で動くのでなく config_error にする。空設定=全ルール
// 無効=何も制御しない状態だが UI 上は「正常」に見え、利用者はツールが働いていると思い込む。
```

**部分適用の物理的不能化**: `config.Load` は `len(errors)>0` のとき `*Config` に必ず `nil` を返す。呼び出し側は `cfg, errs, err := config.Load(path)` の3値のうち `cfg != nil` を「適用可能」の唯一の判定基準にできる（`errs` の長さを別途確認する必要がない設計）。

## Behavior Specification
System Type: transformation

| behavior_id | 入力 | 出力 | pre_state | post_state | invariants | test_ref |
|---|---|---|---|---|---|---|
| BEH-01-01 | 所有者不一致のファイルパス | `error(insecure_permissions)` | ファイル存在・UID不一致 | `*Config==nil`、内容未読 | 内容を1バイトも読まない | `TestCheckPermissions_OwnerMismatch` |
| BEH-01-02 | group/other 書込可能なファイル (`mode&0o022!=0`) | `error(insecure_permissions)` | mode 違反 | `*Config==nil` | 同上 | `TestCheckPermissions_WorldWritable` |
| BEH-01-03 | シンボリックリンク経由のパス | `error(insecure_permissions)` | リンク解決後も通常ファイル判定を通す | `*Config==nil` | `O_NOFOLLOW` で fd 取得しリンク自体を開かない | `TestCheckPermissions_Symlink` |
| BEH-01-04 | 不正な YAML 構文 | `errors=[{code:parse_error}]` | 構文エラー | `*Config==nil` | パースエラーは1件のみ返す（全件収集は対象外） | `TestLoad_ParseError` |
| BEH-01-05 | `version: 1` | `errors` に `unsupported_config_version` を含む | version 不一致 | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-06 | `task_source` キー欠落 | `errors` に `missing_required_field` を含む | 必須キー欠落 | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-07 | 未知キー `foo: bar` を含む YAML | `errors` に `unknown_field` を含む | strict decode 有効 | `*Config==nil` | `KnownFields(true)` が効いている | `TestValidationErrorCodes` |
| BEH-01-08 | `type: browser.redirect` のアクション | `errors` に `unsupported_action` を含む | — | `*Config==nil` | 静かに無視しない | `TestValidationErrorCodes` |
| BEH-01-09 | 同一 `rules[].id` が2件 | `errors` に `duplicate_rule_id` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-10 | `process.start` の `process_id` が2件重複 | `errors` に `duplicate_process_id` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-11 | `interval_seconds: 5`（下限未満） | `errors` に `value_out_of_range` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-12 | `timeout_seconds >= interval_seconds` | `errors` に `timeout_exceeds_interval` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-13 | `failure_policy: hold_controls` | `errors` に `unsupported_value` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-14 | `executable: relative/path` | `errors` に `executable_not_found` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-15 | `bundle_id: "  "`（パターン不一致） | `errors` に `invalid_identifier` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-16 | `shell:true` かつ `allow_shell:false` | `errors` に `shell_not_allowed` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-17 | `allow_force_terminate:true` | `errors` に `forbidden_safety_flag` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-18 | 同一 priority の2ルールが同一 bundle_id に app.start/app.stop | `errors` に `rule_conflict_same_priority` を含む | — | `*Config==nil` | — | `TestValidationErrorCodes` |
| BEH-01-19 | 複数の異なる違反を同時に含む YAML | `errors` に該当する全コードが1回ずつ以上含まれる | — | `*Config==nil` | 最初の1件で打ち切らない（全件収集） | `TestLoad_CollectsAllErrors` |
| BEH-01-20 | 完全に妥当な `config.example.yml` 相当の内容、権限も適正 | `(*Config, nil, nil)` | — | `*Config != nil`、既定値が展開済み | `errors` は空 | `TestLoad_ValidConfigSucceeds` |
| BEH-01-21 | 妥当だが `polling` セクション省略 | `(*Config, nil, nil)`、`Polling` に既定値（60/20/180/release_controls）が展開される | — | `*Config.Polling == defaults` | 既定値展開はゼロ値ではなく明示定数由来 | `TestLoad_DefaultsApplied` |

### Correctness Criteria（観測可能・固定する）
- `len(errors) > 0` の全ての戻り経路で `*Config == nil`（`TestLoadInvalidReturnsNilConfig` が全 BEH-01-05〜19 を横断して照合）。
- 権限違反は `error` として返り `errors []ValidationError` には現れない（`errors` は空 or nil）。
- 検証エラーは複数同時発生時にすべて収集される（`BEH-01-19`）。
- 15種の検証エラーコードは `internal/config/validate.go` 内の文字列リテラルとして1箇所ずつのみ定義される（コード重複定義の禁止）。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- `rawConfig`（YAML unmarshal 用の中間構造体）を設けるか `Config` に直接 unmarshal するかの実装方法。
- `validateXxx` ヘルパの分割粒度（フィールド単位か検証規則単位か）。
- エラー収集に使う内部スライスの変数名。

> 禁則: API shape・データ形式・エラー挙動・retry/timeout/rollback・表示文言・validation 条件・migration/security 方針・acceptance criteria を Left to Implementation に残さない（本 Process では上記3点のみが実装者の裁量）。

## Red Phase: テスト作成と失敗確認
- [x] ブリーフィング確認
- [x] `internal/config/config_test.go` に `TestLoad`（存在しない `config.Load` を呼ぶ最小ケース）を作成
- [x] `internal/config/validate_test.go` に `TestValidationErrorCodes`（15種のエラーコードをそれぞれ発生させる不正 YAML fixture を用意し、各テストケースに docblock で正常系/異常系を分類し、担保する検証規則を1行で記述）を作成
- [x] テストを実行して失敗することを確認（`config.Load` 未定義によるコンパイルエラーまたは FAIL）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P01-VG-01 / status: / command_or_action: `go test ./internal/config -run TestLoad -count=1` / exit_code: / expected: exit!=0 かつ `undefined: config.Load` または `FAIL` / observed: / attempt:

## Green Phase: 最小実装と成功確認
- [x] `internal/constants/constants.go` に本 Process のローカル定数を追加
- [x] `internal/config/permissions.go` の `CheckPermissions` を実装（`O_NOFOLLOW` で fd 取得 → `Fstat` で UID・mode を検証 → 同一 fd から読む）
- [x] `internal/config/config.go` の構造体群と `Load`/`DefaultPath` を実装
- [x] `internal/config/validate.go` の `Validate`・15種の `validateXxx` を実装し、`len(errors)>0` の全経路で `nil` を返すガードを一箇所に集約
- [x] `go.mod` に `gopkg.in/yaml.v3` を追加
- [x] `config.example.yml` を作成（bundle_id はプレースホルダ、絶対パスは `<YOUR_PATH>`）
- [x] Behavior Specification 表の全21行に対応する test_ref のテストが存在し PASS することを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P01-VG-02 / status: / command_or_action: `go test ./internal/config -race -count=1` / exit_code: / expected: exit==0 / observed: / attempt:
- gate_id: P01-VG-03 / status: / command_or_action: `go test ./internal/config -run TestValidationErrorCodes -v` / exit_code: / expected: 15コード全出現 / observed: / attempt:

## Refactor Phase: 品質改善
- [x] `validateXxx` ヘルパ間の重複（範囲チェックの繰り返し等）を整理
- [x] エラーメッセージに機密情報（絶対パスの本人ホームディレクトリ名等）が漏れていないかを再確認（D-06/D-07 grep 再実行）
- [x] `len(errors)>0 ⇒ cfg==nil` の実装が1箇所に集約されていることを確認

✅ **Phase Complete**（GoalEvidence）
- gate_id: P01-VG-04 / status: / command_or_action: `rg -c 'errors\s*\)\s*>\s*0' internal/config/config.go` / exit_code: / expected: ヒット>=1 / observed: / attempt:

## Manual Verification
> Unverified 報告規定: executor: human のシナリオのみが残った場合、自律ループでは実行済みと見なさず status: unverified として報告する。

1. **executor: agent** — 操作: 実際に `chmod 664` した config.yml を用意し `CheckPermissions` を呼ぶ → 期待される出力: `error` が返り `insecure_permissions` を含む → 操作後のデータ状態の確認方法: プロセスがファイル内容を読んでいないことを、読み取り前にエラーが返ることのテストログで確認する。
2. **executor: agent** — 操作: `os.Symlink` で config.yml へのシンボリックリンクを作成しそのリンクパスを `CheckPermissions` に渡す → 期待される出力: `error(insecure_permissions)` またはリンク解決後の実体パスに対する検証が実施される → 操作後のデータ状態の確認方法: テストで `EvalSymlinks` 呼び出し後のパスが検証対象になっていることをアサートする。
3. **executor: agent** — 操作: `shell:true` かつ `allow_shell:true` だが mode が `0o644`（`0o077` マスクに抵触）の config.yml を検証する → 期待される出力: 内容を読んだ後に `insecure_permissions` で拒否される → 操作後のデータ状態の確認方法: `*Config==nil` かつエラーが RPCError 型であることをテストで確認する。

## Dependencies
- Requires: なし
- Blocks: P05, P10
