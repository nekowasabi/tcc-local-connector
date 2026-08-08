---
task_id: "T-20260807-macos-menubar-connector"
title: "TaskChute Cloud 2 連動 macOS 常駐アプリ MVP（MenuBarExtra + Go バックエンド）"
status: planning
created: "2026-08-07"
scope:
  - internal/config/
  - internal/tcc2/
  - internal/state/
  - internal/ledger/
  - internal/rules/
  - internal/engine/
  - internal/logging/
  - internal/constants/
  - internal/protocol/server.go
  - cmd/tcc-local-connector-backend/main.go
  - macos/
  - scripts/
  - docs/
  - README.md
  - config.example.yml
  - go.mod
depends_on: []
risk_flags:
  - security
  - external_api
  - frontend
  - performance
quality_gate:
  command: "bash scripts/verify-all.sh"
  min_quality_score: "-"
commit_mode: manual
loop_ready: true
---
<!-- loop_ready: 検証ゲート契約（refs/make-plan-gates.md）準拠 -->

# Commander's Intent

## Purpose
TaskChute Cloud 2 の「いま実行中のタスク」を単一の真実源として、macOS 上のアプリ・プロセスの状態を継続的に期待状態へ整合させる。**完全な禁止ではなく継続的整合**、**通常終了のみ**、**取得できないときは制御を手放す**の 3 点を、レビュー任せでなく実装の物理構造（型・grep ゲート）として保証する。

## End State
`dist/TCCLocalConnector.app` を起動するとメニューバーに常駐し、`~/.config/tcc-local-connector/config.yml` のルールに従って 60 秒周期で GUI アプリの起動/通常終了と管理対象プロセスの起動/停止を行う。タスク取得が `DefaultFailureGraceSeconds` 失敗し続けたら制御を解除する。メニューから期限付き一時停止でき、スリープを跨いでも絶対時刻で自動復帰する。強制終了経路がコードベースに存在しない。`bash scripts/verify-all.sh` が緑。

## Key Tasks
1. Go バックエンドの純粋ロジック（設定・解析・状態機械・ルール評価・台帳）を先に完成させる（P01–P06）。OS 非依存テストで検証でき、緑になれば残るリスクは macOS 統合のみになる。
2. stdio RPC 契約を本計画で凍結し、Go 側（P07）と Swift 側（P08）を独立に進める。契約が decision-complete なため Swift は Go の完成を待たない。
3. 安全側の既定を実装の物理構造にする（P52 / Don'ts の grep 監査）。「SIGKILL を書かない」「部分適用できない型にする」「解析失敗と実行中なしを別の型にする」を機械照合で守る。

---

# ★ Constants（唯一の正・他は定数名で参照）

> このセクションが数値の単一ソース。process ファイルおよびコード・コメントでは定数名のみを参照し、数値リテラルを書かない。Go は `internal/constants/constants.go`、Swift は `macos/Sources/ConnectorCore/Constants.swift` に実体を置く。

## 既存（変更しない。既存パッケージの定義が単一ソース）

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `protocol.Version` | 1 | — | server.go:19。本 MVP で据え置き。機能差分は capabilities で交渉 |
| `protocol.DefaultMaxMessage` | 65536 | bytes | server.go:20。受信上限 64 KiB |
| `protocol.DefaultDrainTimeout` | 5 | seconds | server.go:21 |
| `tcc2.mcpProtocolVersion` | "2025-06-18" | — | probe.go:16。実測 F1 と一致 |
| `tcc2.maxResponseBytes` | 1048576 | bytes | probe.go:17。1 MiB |

## 設定・パス・権限

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `ConfigSchemaVersion` | 2 | — | 他値は `unsupported_config_version` |
| `ConfigRelPath` | ".config/tcc-local-connector/config.yml" | path | `$HOME` 相対 |
| `StateDirRelPath` | ".local/state/tcc-local-connector" | path | — |
| `PauseFileName` / `LedgerFileName` / `LogFileName` | "pause.json" / "managed-processes.json" / "backend.log" | — | 状態ディレクトリ配下 |
| `StateFileMode` | 0600 | file mode | 状態ファイル作成 mode |
| `ConfigForbiddenModeMask` | 0o022 | mask | group/other 書き込み可なら拒否 |
| `ConfigStrictModeMask` | 0o077 | mask | `allow_shell:true` 時の追加要求 |

## ポーリング・猶予

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `MinPollIntervalSeconds` / `DefaultPollIntervalSeconds` / `MaxPollIntervalSeconds` | 10 / 60 / 3600 | seconds | 下限・既定・上限 |
| `MinPollTimeoutSeconds` / `DefaultPollTimeoutSeconds` / `MaxPollTimeoutSeconds` | 1 / 20 / 120 | seconds | 加えて `timeout < interval` を強制 |
| `MinFailureGraceSeconds` / `DefaultFailureGraceSeconds` / `MaxFailureGraceSeconds` | 0 / 180 / 3600 | seconds | 超過で `release_controls` |
| `WakeReevaluateThresholdFactor` | 2 | 倍 | tick 遅延が interval×2 超でスリープ復帰扱い |
| `UserInfoTTLSeconds` | 21600 | seconds | `get_user` キャッシュ寿命 6h |
| `TaskChuteQueryDays` | 2 | days | `[d-1, d]`。Start of Day 符号不定の吸収 |

## 解析（実測由来・非契約インターフェース）

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `InProgressLinePrefix` | "- [In Progress] " | literal 16B | 実測 F2。オフセット 0 で完全一致 |
| `IDBlockDelimiter` | " [ID: " | literal 6B | 実測 F2/F6。LastIndex で右から探す |
| `DateHeaderPattern` | `^##\s+.*?(\d{4}-\d{2}-\d{2})\s*$` | regexp | 実測 F5 |
| `MetaHeadPattern` | `^\d{2}:\d{2}(,\|$)` | regexp | メタ括弧か名前の一部かの判定 |
| `TaskIDPattern` | `^task_[0-9a-f]{32}$` | regexp | 実測 F6 |
| `UserFieldPattern` | `^- \*\*([^*]+):\*\* (.*)$` | regexp | 実測 F8 |
| `CellarVersionPattern` | `(?:^\|/)Cellar/tcc2/([^/]+)/` | regexp | 実測 F11。CLI 版のベストエフォート解決 |
| `IDBlockKeys` | Section, Project, Mode, Routine, Tags | list | 実測 F6。未知キーは warnings |
| `KnownStatusTags` | Done, In Progress, Todo | list | 実測 F3。診断用 |
| `MaxRunningTasks` / `MaxTaskNameBytes` / `MaxErrorMessageBytes` | 32 / 512 / 200 | count / bytes / bytes | 解析の暴走ガード |
| `MaxRules` / `MaxActionsPerRule` | 100 / 20 | count | 設定の上限 |

## アクション・一時停止・台帳・MCP

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `MinActionTimeoutSeconds` / `DefaultActionTimeoutSeconds` / `MaxActionTimeoutSeconds` | 1 / 30 / 300 | seconds | `command.run` |
| `MinGraceSeconds` / `DefaultAppStopGraceSeconds` / `DefaultProcessStopGraceSeconds` / `MaxGraceSeconds` | 1 / 10 / 10 / 120 | seconds | 通常終了の猶予 |
| `CommandRunMaxOutputBytes` | 65536 | bytes | stdout/stderr 上限 |
| `ActionIDFormat` | "%d-%d" | format | (cycle_id, seq) → "42-1" |
| `NotifyTitleMaxRunes` / `NotifyMessageMaxRunes` | 200 / 500 | runes | — |
| `PauseStateVersion` / `LedgerVersion` | 1 / 1 | — | 不一致はファイル破棄で安全側 |
| `PauseMinSeconds` / `PauseMaxSeconds` | 60 / 86400 | seconds | 0 秒停止の事故防止・上限 24h |
| `PausePresetShortSeconds` / `PausePresetLongSeconds` | 900 / 3600 | seconds | メニューの 15 分 / 1 時間 |
| `PauseNextDayStartHour` | 5 | hour (local) | 実測 F8 の Start of Day に一致 |
| `PSExecutablePath` / `PSArgsFormat` | "/bin/ps" / `-p <pid> -o lstart=,args=` | path / args | PID 再利用対策 |
| `MCPWaitDelaySeconds` / `MCPStderrLimitBytes` / `MCPMaxResponseBytes` | 2 / 4096 / 1048576 | seconds / bytes | 新 Session 用。既存 probe.go と同値 |

## 識別子パターン・ログ

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `RuleIDPattern` / `ProcessIDPattern` | `^[a-z0-9][a-z0-9-]{0,63}$` | regexp | 全設定内で一意 |
| `BundleIDPattern` | `^[A-Za-z0-9][A-Za-z0-9._-]*$` | regexp | アプリ名だけの指定を拒否 |
| `EnvKeyPattern` | `^[A-Za-z_][A-Za-z0-9_]*$` | regexp | — |
| `DefaultLogLevel` | "info" | — | debug/info/warn/error |
| `MinLogRetainDays` / `DefaultLogRetainDays` / `MaxLogRetainDays` | 1 / 14 / 365 | days | 起動時ローテーション |
| `LogRedactPatterns` | email, "Bearer ", "Token expires" | list | 実測 F8/F10 由来の秘密除去 |

## Swift（`macos/Sources/ConnectorCore/Constants.swift`）

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `supportedProtocolVersion` | 1 | — | `protocol.Version` と同値必須 |
| `requiredCapabilities` | status, reload_config, pause, resume, refresh_now, config_paths, report_actions | set (7) | 1 つでも欠けたら backendIncompatible |
| `backendReadyTimeoutSeconds` / `backendRequestTimeoutSeconds` | 10 / 30 | seconds | 起動待ちと通常要求を分離 |
| `backendRestartMaxAttempts` / `backendRestartBaseDelaySeconds` / `backendRestartDelayFactor` / `backendRestartMaxDelaySeconds` / `backendRestartWindowSeconds` | 5 / 1 / 2 / 30 / 600 | count / seconds | 待機列 1,2,4,8,16 |
| `backendDownReleaseGraceSeconds` | 180 | seconds | `DefaultFailureGraceSeconds` と同値必須 |
| `backendShutdownStdinGraceSeconds` / `backendShutdownTermGraceSeconds` | 5 / 3 | seconds | 自プロセスの子のみ回収 |
| `frontendMaxLineBytes` | 262144 | bytes | 受信側 256 KiB（送信上限とは別値） |
| `menuRefreshIntervalSeconds` / `appStopPollIntervalMilliseconds` | 5 / 250 | seconds / ms | UI 更新・終了確認 |
| `pauseNextDayStartHour` | 5 | hour | `PauseNextDayStartHour` と同値必須 |
| `bundleIdentifier` / `backendResourceName` | "jp.takets.tcc-local-connector" / "tcc-local-connector-backend" | — | Info.plist と一致必須 |
| `notifierRecentCapacity` | 20 | count | 通知降格時のリングバッファ |

## 計画運用定数（実行ハーネス向け・コードには現れない）

| 定数名 | 値 | 単位 | 備考 |
|-------|-----|------|------|
| `GATE_ID_FORMAT_PROCESS` / `GATE_ID_FORMAT_INTEGRATION` / `GATE_ID_FORMAT_FINAL` | `P{NN}-VG-{NN}` / `W{NN}-IG-{NN}` / `FINAL-VG-{NN}` | format | 計画内一意 |
| `GATE_TABLE_COLUMNS` | 12 | count | refs/make-plan-gates.md §3 の列順 |
| `AGGREGATE_KEY_FORMAT` | `{task_id}:{gate_id}` | format | gate-results.jsonl の一意キー |
| `TASK_RETRY_BUDGET` | 3 | count | 全ゲート共通。個別固定回数の記載を禁止 |
| `MAX_CONFLICT_MATRIX_ROWS` | 40 | count | 超過は Wave 再分割の信号 |
| `SYMBOL_LINE_TOLERANCE` | 5 | lines | 既存 3 ファイルは全文読取で行番号確定済み |
| `MIN_PARALLEL_DIFF_LINES` | 30 | lines | 未満は前後 Process へ統合可 |
| `LINTER_TIMEOUT_SEC` / `PREFLIGHT_GIT_CHECK_TIMEOUT_SEC` | 120 / 10 | seconds | — |
| `MICRO_ALLOCATE_MAX_WAVES` | 10 | count | 本計画は W01–W10 でちょうど上限 |
| `VISUAL_GATE_EVIDENCE_DIR` | "docs/screenshots/" | path | 手動スクリーンショットの証跡 |

---

# Scope

**対象**: `internal/{config,tcc2,state,ledger,rules,engine,logging,constants}/`（新規）／`internal/protocol/server.go`（capabilities 拡張・handle に 7 case 追加）／`cmd/tcc-local-connector-backend/main.go`（`--config` 追加・Engine 配線）／`macos/`（SwiftPM パッケージ・MenuBarExtra・BackendClient・.app バンドル）／`scripts/`（バンドル組立・監査・検証）／`docs/`・`README.md`・`config.example.yml`・`go.mod`

**対象外（理由付き）**:
- Windows 11 / WSL2 / C# WPF 常駐アプリ・`wsl.exe` 経由起動 — 本タスクで明示的にスコープ外指定
- Go 本体への GOOS 分岐 / build tag 追加 — 既存 Go 本体に分岐は一切なく、macOS 単独対象では投機的複雑性になる
- `scripts/verify-wsl-backend.py` / `docs/wsl-verification-2026-08-01.md` / `RESEARCH-wsl.md` の改変 — WSL スコープ外の既存資産。温存する
- ブラウザ URL 誘導・WebExtension・Native Messaging — RESEARCH.md §17.2 で別コンポーネントと決定済み。設定に現れたら `unsupported_action` で明示拒否
- tmux ランチャー・nvim RPC 終了 — §17.1 の MVP 列挙外。msgpack-RPC と tmux セッション管理という別の依存面を増やす。`process.start`/`process.stop` で代替
- 外部プロセスの一般的終了・強制終了・プロセス名だけによる終了・端末/tmux 全体終了・他利用者のプロセス操作 — §7.2/§7.3/§16.2/§17.3 で除外。設定で有効化しようとしたら検証エラー
- OS レベルの完全な起動禁止（Endpoint Security / MDM）・TLS 傍受プロキシ・Network Extension・hosts/DNS・既定ブラウザ仲介 — §10/§17.3 で個人用 MVP には過剰と結論済み
- Developer ID 署名・公証・Mac App Store 配布・App Sandbox・自動更新・複数利用者配布 — §2.2 の調査対象外項目。ad-hoc 署名のみ
- AppleScript / System Events による UI 操作 — §15.2。Automation/アクセシビリティ権限を要求しない設計制約
- 直接 API（HTTP）による TaskChute 取得・`tcc2` 資格情報の複製 — §12.3/§4.5。認証は `tcc2 login` に委ねる
- `.xcodeproj` / `xcodebuild` / XCUITest — pbxproj は差分レビュー不能。SwiftPM で `swift test` が使える
- golangci-lint / swiftlint / swift-format — 実測 F12 で**未導入**。存在しないコマンドを検証手順に書けば計画が実行不能になる
- `protocol.Version` の 2 への引き上げ — 既存 5 メソッドの入出力を変えないため破壊的変更がない
- `failure_policy: hold_controls`・ルール単位の failure_policy 上書き・`match` の正規表現 — §12.4 が制御解除を推奨。2 値実装はテスト対象を倍増させるだけ
- UDS / Named Pipe / HTTP 待受・CLI からの第2クライアント接続 — §5.2「初期版から HTTP 待受を導入する必要はない」
- `Est`/`Act` の時間値パース・`Tags` の利用・残り 18 の MCP ツール — ルール評価はタスク名のみを条件とする

---

# Assumptions / Open Questions（外部挙動に影響する未決定事項）

| 種別 | 項目 | 内容 | 外部挙動への影響 | 解決方針 |
|------|------|------|----------------|----------|
| Open Question | OQ-1 `NSRunningApplication.terminate()` の TCC 挙動 | macOS 26 で Automation 権限を要求するか未実測 | 大: 無言失敗なら「終了したつもりで終了していない」 | P09 実装者が VM-2 で判定。**失敗時の吸収は設計済み** — `grace_seconds` 後に `isTerminated==false` なら必ず `refused/quit_refused` を返す |
| Open Question | OQ-2 ad-hoc 署名下の通知/ログイン項目 | `UNUserNotificationCenter` と `SMAppService.mainApp.register()` の成否が未実測 | 中: 通知が出せない／ログイン時起動不可（既定 OFF のため機能の正しさには影響しない） | P09 実装者が VM-1 で判定。**結果によらず実装は変わらない** — 通知は `Notifier.recent` へ降格、アイコンを第一の伝達手段とする |
| Open Question | OQ-3 `Start of Day: -05:00:00` の符号 | 「05:00 に日が始まる」か「オフセット −5h」か判定材料なし | 小（対策済み）: `TaskChuteQueryDays`=2 によりどちらの解釈でも取りこぼさない | 00:00–05:00 帯で人が 1 回 `running_tasks[].date` を確認し記録 |
| Open Question | OQ-4 TaskChute のポーリング制限 | 推奨間隔・レート制限が未確認 | 中: `MinPollIntervalSeconds` での長時間運用は保証できない | 既定 60 秒のまま出荷。README に「10 秒まで下げられるが利用条件は未確認」と明記。実装は変わらない |
| Open Question | OQ-5 トークン期限切れ時の MCP 挙動 | 期限切れ時に `isError` か異常終了か未確認 | 小: 毎サイクル起動終了の構造により「長時間起動中の更新」問題は回避済み | どちらも `degraded` へ収束。`isError` の text に 401/unauthorized を含む場合のみ `auth_expired` に分類（ベストエフォート） |
| Assumption | A-1 `/bin/ps -o lstart=` の一意性 | `lstart`＋`args` 完全一致が偶然成立する確率は無視できる | 小 | 決定済み。`ps` 実行失敗時は「自分のものではない」側へ倒す |
| Assumption | A-2 `MenuBarExtra` の常駐動作 | SDK 内に実在を確認済み。macOS 26 での実動作は VM-1 で確認 | 中 | 不足時は `NSStatusItem` へ移行/併用可（impl_freedom に明記済み） |
| Assumption | A-3 `yaml.v3` の `KnownFields(true)` | 未知キー検出に使用 | 小 | P01 の `TestUnknownField` で即判明。代替実装は 30 行程度 |
| Assumption | A-4 実行中タスク 32 件超は実運用でない | 超過は解析失敗→猶予→`released`。制御が緩む方向で安全側 | 小 | 決定済み。暴走ガードであり運用閾値ではない |

> RPC 契約・パーサ仕様・config スキーマ・状態機械・一時停止永続化・台帳・通信断時の Swift 挙動は**すべて確定済み**。実装者が新たに決める外部挙動の判断は残っていない。

---

# Required Sections（risk_flags 連動）

| Flag | 必須セクション | 反映先 |
|------|---------------|--------|
| security | Security & Authorization | P01（設定の出自検証）／P04（PID 同一性）／P50（秘密除去）／P52（実行時二重ガード） |
| external_api | External Deps & Fallback | P02（tcc2 テキスト非契約・センチネル・黄金 fixture）／P06（猶予と制御解除） |
| frontend | Frontend Constraints | P08（BackendClient・Swift 6 並行性）／P09（MenuBarExtra・NSWorkspace） |
| performance | Performance Notes | P06（single-flight・1万サイクルのリーク 0）／P02（MCP 都度起動のコスト） |

> multi_id / data_migration / backwards_incompatible は対象外。単一利用者・単一アカウントであり、全状態ファイルが新規（移行対象なし）、`protocol.Version` 据え置きで既存契約を 1 バイトも変えないため。

# Decision-Complete チェック（15観点）

| 観点 | 状態 | 記載先 |
|------|------|--------|
| Goal / Success Criteria | 記載済 | Commander's Intent + Acceptance Criteria |
| Scope / Non-goals | 記載済 | Scope セクション（対象外 40 項目を理由付きで集約） |
| Existing Behavior（変えないもの） | 記載済 | Don'ts D-01/D-02、P07/P02 の無改変保証（git diff で機械照合） |
| Public Interfaces / Contracts | 記載済 | P07 の Behavior Specification（RPC 12 メソッド・4 イベント・完全スキーマ） |
| Data Model / State | 記載済 | P03/P04 の pre_state / post_state（pause.json / managed-processes.json） |
| Behavior / Edge Cases | 記載済 | P02 の解析 6 分岐、P05 の競合解決、P06 の猶予境界 |
| Error Handling | 記載済 | P07 のエラーコード 19 種、P02 の ParseError 4 種 |
| Security / Privacy | 記載済 | P01（出自検証 5 条件）／P50（秘密除去）／P52（実行時ガード） |
| Performance / Scalability | 記載済 | P06（single-flight・TestTenThousandCycles で goroutine/FD 増分 0） |
| Concurrency / Async | 記載済 | P06（single-flight）／P08（Swift 6 strict concurrency・actor 境界） |
| Migration / Compatibility | 記載済 | 全状態ファイルが新規。version 不一致は破棄して安全側。protocol.Version 据え置き |
| Observability | 記載済 | P50（構造化ログ・retain_days）／P07（status のメトリクス） |
| Testing | 記載済 | P10–P18 + Verification Gates 全 60 件 |
| Rollout / Operations | 記載済 | P200 README（dry_run からの段階投入・既知の制約・runbook） |
| Maintainability | 記載済 | P100–P102（静的解析・定数単一ソース監査）／P300（分離妥当性の LOC 再評価） |

---

# Progress Map

| Process | Title | Status | Disjoint | Type | File |
|---------|-------|--------|----------|------|------|
| 01 | YAML 設定スキーマと検証 | ☑ completed | y | 変換 | [→ plan-for-mac/process-01.md](plan-for-mac/process-01.md) |
| 02 | tcc2 MCP セッションと `[In Progress]` 解析 | ☑ completed | y | both | [→ plan-for-mac/process-02.md](plan-for-mac/process-02.md) |
| 03 | 状態機械と期限付き一時停止の永続化 | ☑ completed | y | react | [→ plan-for-mac/process-03.md](plan-for-mac/process-03.md) |
| 04 | 管理対象プロセス台帳 | ☑ completed | y | both | [→ plan-for-mac/process-04.md](plan-for-mac/process-04.md) |
| 05 | ルール評価と plan 生成 | ☑ completed | y | 変換 | [→ plan-for-mac/process-05.md](plan-for-mac/process-05.md) |
| 06 | ポーリングエンジン | ☑ completed | y | react | [→ plan-for-mac/process-06.md](plan-for-mac/process-06.md) |
| 07 | protocol 拡張と main 配線 | ☑ completed | n | react | [→ plan-for-mac/process-07.md](plan-for-mac/process-07.md) |
| 08 | Swift 骨格・.app バンドル・BackendClient | ☑ completed | y | react | [→ plan-for-mac/process-08.md](plan-for-mac/process-08.md) |
| 09 | macOS アクション実行と MenuBarExtra UI | ☑ completed | y | react | [→ plan-for-mac/process-09.md](plan-for-mac/process-09.md) |
| 10 | 設定検証テスト網羅 | ☑ completed | y | - | [→ plan-for-mac/process-10.md](plan-for-mac/process-10.md) |
| 11 | 解析黄金テスト | ☑ completed | y | - | [→ plan-for-mac/process-11.md](plan-for-mac/process-11.md) |
| 12 | 一時停止・スリープテスト | ☑ completed | y | - | [→ plan-for-mac/process-12.md](plan-for-mac/process-12.md) |
| 13 | 台帳・PID 再利用テスト | ☑ completed | y | - | [→ plan-for-mac/process-13.md](plan-for-mac/process-13.md) |
| 14 | ルール評価テスト | ☑ completed | y | - | [→ plan-for-mac/process-14.md](plan-for-mac/process-14.md) |
| 15 | エンジンテスト | ☑ completed | y | - | [→ plan-for-mac/process-15.md](plan-for-mac/process-15.md) |
| 16 | RPC 契約テスト | ☑ completed | n | - | [→ plan-for-mac/process-16.md](plan-for-mac/process-16.md) |
| 17 | Swift ユニットテスト | ☑ completed | y | - | [→ plan-for-mac/process-17.md](plan-for-mac/process-17.md) |
| 18 | Go E2E テスト | ☑ completed | y | - | [→ plan-for-mac/process-18.md](plan-for-mac/process-18.md) |
| 50 | 構造化ログと保持日数 | ☑ completed | n | both | [→ plan-for-mac/process-50.md](plan-for-mac/process-50.md) |
| 51 | dry_run の全経路貫通 | ☑ completed | n | 変換 | [→ plan-for-mac/process-51.md](plan-for-mac/process-51.md) |
| 52 | 安全ゲートの実行時再確認 | ☑ completed | n | react | [→ plan-for-mac/process-52.md](plan-for-mac/process-52.md) |
| 100 | Go 静的解析とレース | ☑ completed | n.a. | - | [→ plan-for-mac/process-100.md](plan-for-mac/process-100.md) |
| 101 | Swift 6 厳格並行性とビルド警告ゼロ | ☑ completed | n.a. | - | [→ plan-for-mac/process-101.md](plan-for-mac/process-101.md) |
| 102 | 定数単一ソース化監査 | ☑ completed | n.a. | - | [→ plan-for-mac/process-102.md](plan-for-mac/process-102.md) |
| 200 | README.md 新規 | ☑ completed | y | - | [→ plan-for-mac/process-200.md](plan-for-mac/process-200.md) |
| 201 | docs/protocol-v1.md 新規 | ☑ completed | y | - | [→ plan-for-mac/process-201.md](plan-for-mac/process-201.md) |
| 202 | docs/config-schema.md 新規 | ☑ completed | y | - | [→ plan-for-mac/process-202.md](plan-for-mac/process-202.md) |
| 203 | RESEARCH.md 追記 | ☑ completed | n | - | [→ plan-for-mac/process-203.md](plan-for-mac/process-203.md) |
| 300 | OODA 総括と再評価 | ☑ completed | n.a. | - | [→ plan-for-mac/process-300.md](plan-for-mac/process-300.md) |

**Type 列凡例**: `変換`=transformation / `react`=reactive / `both`=両面 / `-`=behavior_scope:false
**Disjoint 列凡例**: `y`=disjoint_guarantee:true（wave 並列可） / `n`=false（既存シンボル改変あり・serial 必須） / `n.a.`=読み取り専用

**DAG**: `{01,02,03,04,08}→{05,09}→06→07→{10,11,12,13,14,15,17}→{16,18}→50→51→52→{100,101,102}→{200,201,202,203}→300`
**DAG凡例**: `{A,B}` = 並列実行可能、`A→B` = A完了後にB実行
**Overall**: ☑ 29/29 completed、◐ 0/29 partial（本 Cycle で扱った工程のみ更新）

---

# Wave Progress Map

| Wave | Processes | Depends on Wave | Disjoint | Status |
|------|-----------|------------------|----------|--------|
| W01 | P01, P02, P03, P04, P08 | - | y（`go.mod` は P01 のみが編集する制約付き） | ☑ completed |
| W02 | P05, P09 | W01 | y | ☑ completed |
| W03 | P06 | W02 | n.a.（単独） | ☑ completed |
| W04 | P07 | W03 | n（既存 server.go / main.go を改変。単独実行） | ☑ completed |
| W05 | P10, P11, P12, P13, P14, P15, P17 | W04 | y | ☑ completed |
| W06 | P16, P18 | W05 | y | ☑ completed |
| W07 | P50, P51, P52 | W06 | n（3 者とも `internal/engine/engine.go` を触るため **P50→P51→P52 の直列実行必須**） | ☑ completed |
| W08 | P100, P101, P102 | W07 | y（読み取り専用） | ☑ completed |
| W09 | P200, P201, P202, P203 | W08 | y | ☑ completed |
| W10 | P300 | W09 | n.a.（単独） | ☑ completed |

**Wave凡例**: 同一 Wave 内は並列実行可能（W07 を除く）。並列度の上限は W01 で 5、W05 で 7。

---

# Execution Contract

<!-- sync: make-plan-gates -->
> 機械可読な正本は refs/make-plan-gates.md §1 の `execution_contract` スキーマに従う。Markdown 表はその投影であり二重管理しない。

```yaml
execution_contract:
  schema_version: 2
  plan_revision: 1
  task_id: "T-20260807-macos-menubar-connector"
  loop_ready: true
  objective: "MenuBarExtra 常駐アプリが実行中タスクに応じて macOS のアプリ・プロセス状態を継続整合させ、取得不能時は制御を解除する"
  goal_condition_projection: "bash scripts/verify-all.sh が exit 0、かつ VM-1..VM-5 の human シナリオが記録済み、かつ強制終了経路が BackendClient.hardKill の 1 箇所のみ"
  non_goals: ["Windows/WSL", "ブラウザURL誘導", "強制終了", "外部プロセス一般終了", "tmux/nvim", "署名公証", "App Store配布"]
  constraints:
    - "既存5メソッドと probe.go 全シンボルを無改変に保つ"
    - "stdout はプロトコル専用・診断は stderr"
    - "Go の外部依存は gopkg.in/yaml.v3 のみ"
    - "未導入ツール（golangci-lint/swiftlint/swift-format/xcodebuild）を検証手順に書かない"
  success_criteria:
    - {id: SC-01, statement: "実行中タスク取得後60秒以内に status.state==active となり running_tasks の task_id が TaskIDPattern に一致する"}
    - {id: SC-02, statement: "ルール切替時に on_enter/on_exit が各1回だけ実行され、連続5サイクルで追加発火0件"}
    - {id: SC-03, statement: "取得失敗が DefaultFailureGraceSeconds 継続で released へ遷移し以後 plan.actions が空配列になる"}
    - {id: SC-04, statement: "isError:true 時に parse_ok==false となり running_tasks が空配列に置換されず前回値を保持する"}
    - {id: SC-05, statement: "pause.json に絶対時刻が永続化され、プロセス強制終了と再起動を跨いで一時停止が維持される"}
    - {id: SC-06, statement: "PID 再利用状況で process.stop がシグナルを送らず台帳から削除し notify を出す"}
    - {id: SC-07, statement: "バックエンド再起動が待機列1,2,4,8,16で最大5回、6回目は起動しない"}
    - {id: SC-08, statement: "protocol_version 不一致時に再起動せず OS 操作を一切行わない"}
    - {id: SC-09, statement: "dry_run:true で副作用APIが呼ばれず notify のみ出る"}
    - {id: SC-10, statement: "禁止 safety フラグまたは unsupported_action を含む設定で config_error となり一切操作しない"}
  policies:
    - {id: TEST-RED, statement: "対象アサーションが想定理由で失敗する（環境・構文・import エラーは Red 合格にしない）"}
    - {id: TEST-GREEN, statement: "対象テストが成功する"}
    - {id: SCOPE-01, statement: "変更を Affected Files 内に限定する"}
    - {id: DONT-01, statement: "Don'ts D-01..D-15 を新規導入しない"}
    - {id: QUALITY-01, statement: "bash scripts/verify-all.sh が exit 0"}
  gates: "各 process の Verification Gates 表（全60件）+ Integration Gates + Final Gates"
  required_gates: "各 process の green フェーズゲート全件 + W01-IG-01..W10-IG-01 + FINAL-VG-01..FINAL-VG-04"
  completion_rule: "全 required_gates が pass かつ必須条件に Unverified がない"
  await_input_rule: "OQ-1/OQ-2 の実機判定結果が必要な場合、人の確認を得れば安全に再開できる"
  safe_stop_rule: "TASK_RETRY_BUDGET 枯渇、取得不能な権限、破壊的判断、安全な継続不能"
  change_set_source: git_task_delta
  evidence_projection: evaluator_visible_summary
```

---

# Conflict Matrix

> 行はファイル単位で集約する（symbol 単位では `MAX_CONFLICT_MATRIX_ROWS` を超えるため）。同一ファイルを 2 つ以上の Process が触る行のみ Disjoint=false になる。

| Process | Symbols (file:symbol) | Disjoint | Confidence | Evidence |
|---------|----------------------|----------|------------|----------|
| 01 | internal/config/*, internal/constants/constants.go, go.mod, config.example.yml | true | low | 新規パッケージ。go.mod は P01 のみが編集 |
| 02 | internal/tcc2/{taskchute,parse,version}.go | true | medium | 既存 probe.go 全シンボルを無改変（P02-VG-05 の git diff で照合） |
| 03 | internal/state/{machine,pause}.go | true | low | 新規パッケージ |
| 04 | internal/ledger/{ledger,verify,manage}.go | true | low | 新規パッケージ |
| 05 | internal/rules/{evaluate,plan}.go | true | low | 新規。P01/P02 の型を import のみ |
| 06 | internal/engine/{engine,cycle,status}.go | true | low | 新規パッケージ |
| 07 | internal/protocol/server.go:{Server,Serve,handle,Emit,EngineAPI}, internal/protocol/params.go, cmd/.../main.go:{main,options,parseOptions} | **false** | **medium** | 既存シンボルへの改変あり（server.go L51-58/L68-80/L105-111/L231-279、main.go L18-69 は全文読取で確定） |
| 08 | macos/Sources/ConnectorCore/{Protocol,LineFramer,BackendClient,PlanExecutor,AppStore,Constants}.swift, macos/Package.swift, macos/Resources/Info.plist, scripts/{make-app-bundle,dev-run}.sh | true | low | 新規ディレクトリ。Go ツリーと交差なし。**StatusIcon.swift は P09 が作るため P08 の symbol_targets から除外**（ファイル単位で非交差） |
| 09 | macos/Sources/TCCLocalConnector/*, macos/Sources/ConnectorCore/StatusIcon.swift | true | low | P08 とはファイル単位で非交差（ConnectorCore/StatusIcon.swift は P09 のみが新規作成）。**SwiftPM のテストターゲットは実行可能ターゲットを import できないため、テスト対象の StatusIcon.describe をライブラリターゲット側に置く必要がある**（P09-VG-01 の前提） |
| 10–15, 17 | 各パッケージの *_test.go（config/tcc2/state/ledger/rules/engine/Swift） | true | low | patch_only:true。自パッケージのテストのみ |
| 16 | internal/protocol/server_test.go | **false** | medium | 既存テストファイルへ追記（既存ケース削除 0 行を照合） |
| 18 | internal/engine/e2e_test.go, internal/engine/testdata/fake-tcc2.sh | true | low | 新規 |
| 50 | internal/logging/*, internal/engine/engine.go, internal/protocol/server.go | **false** | low | Logger を engine と protocol の双方へ注入 |
| 51 | internal/engine/engine.go, internal/ledger/manage.go, macos/.../MacPlanExecutor.swift | **false** | low | **P50/P52 と engine.go で衝突** |
| 52 | internal/engine/engine.go, internal/ledger/manage.go | **false** | low | **P50/P51 と engine.go で衝突。W07 は直列必須** |
| 100–102 | n/a（読み取り専用） | n/a | high | 検証のみ |
| 200 / 201 / 202 | README.md / docs/protocol-v1.md / docs/config-schema.md | true | high | それぞれ別ファイル |
| 203 | RESEARCH.md | **false** | high | 既存ファイルへ追記（削除行 0 を照合） |
| 300 | scripts/loc-report.sh, docs/macos-verification-<date>.md | n/a | low | 新規 |

**注記**: Disjoint=false の Process（07 / 16 / 50 / 51 / 52 / 203）は worktree isolation または serial 実行が必要。特に W07（50→51→52）は `internal/engine/engine.go` の三重衝突により**直列実行が必須**。

---

# Integration Gates

<!-- sync: make-plan-gates -->
> wave 境界の合流検証。列定義は refs/make-plan-gates.md §3 を正とする。execution_contract.gates の投影であり二重管理しない。

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---------|-------|------|----------|----------|---------|-------------|---------------|---------------|----------------|----------------|-------------|
| W01-IG-01 | green | test | agent | true | `go build ./... && go test ./internal/{config,tcc2,state,ledger} -race -count=1 && swift build --package-path macos` | W01 の task_delta 和集合 | exit==0 | GoalEvidence（exit_code と最終行を逐語） | failure_class:build/test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04,SC-05,SC-06 | SCOPE-01,QUALITY-01 |
| W02-IG-01 | green | test | agent | true | `go test ./internal/rules -race -count=1 && swift test --package-path macos` | W02 の task_delta 和集合 | exit==0 | 同上 | 同上 | SC-02 | SCOPE-01 |
| W03-IG-01 | green | test | agent | true | `go test ./internal/engine -race -count=1` | W03 の task_delta | exit==0 | 同上 | 同上 | SC-03,SC-09 | SCOPE-01 |
| W04-IG-01 | green | conformance | agent | true | `go test ./... -race -count=1 && git diff -- internal/protocol/server_test.go \| rg '^-' \| rg -v '^---'` | W04 の task_delta | exit==0 かつ第2コマンドの出力が空 | 同上 | 同上 | SC-08 | SCOPE-01,DONT-01 |
| W05-IG-01 | green | test | agent | true | `go test ./... -race -count=1 -cover && swift test --package-path macos` | W05 の task_delta 和集合 | exit==0 かつ config>=85.0% / tcc2>=85.0% / rules>=90.0% | 同上 | 同上 | SC-01,SC-02,SC-05,SC-06 | QUALITY-01 |
| W06-IG-01 | green | test | agent | true | `go test ./internal/{protocol,engine} -race -count=10` | W06 の task_delta 和集合 | exit==0。E2E 出力に ready/plan/report_actions accepted/paused/resumed の5マーカー全出現 | 同上 | 同上 | SC-01,SC-03,SC-07 | QUALITY-01 |
| W07-IG-01 | green | test | agent | true | `go test ./... -race -count=1 && swift test --package-path macos` | W07 の task_delta 和集合（直列3 Process） | exit==0 | 同上 | 同上 | SC-09,SC-10 | DONT-01 |
| W08-IG-01 | green | quality | agent | true | `bash scripts/verify-go.sh && bash scripts/verify-swift.sh && bash scripts/forbidden-audit.sh` | 全 task_delta | 3コマンドすべて exit==0 | 同上 | 同上 | SC-01..SC-10 | QUALITY-01,DONT-01 |
| W09-IG-01 | green | conformance | agent | true | `go test ./internal/protocol -run TestCapabilitiesMatchDocs && git diff --numstat RESEARCH.md \| awk '{print $2}'` | W09 の task_delta 和集合 | exit==0 かつ第2コマンドの出力が `0` | 同上 | 同上 | SC-01 | SCOPE-01 |
| W10-IG-01 | green | review | agent | true | `bash scripts/loc-report.sh` | 全 task_delta | 出力に go_nontest_loc と swift_nontest_loc が数値で含まれ、1200 行閾値での判定が明記される | 同上 | failure_class:review, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01..SC-10 | QUALITY-01 |

---

# Acceptance Criteria（要約）

**機能要件**: SC-01〜SC-10（実行中タスク取得と task_id 形式／`on_enter`・`on_exit` の一回性／猶予経過での制御解除／解析失敗と実行中なしの区別／一時停止のプロセス跨ぎ永続化／PID 再利用時の非シグナル／再起動バックオフ上限／版不一致時の操作停止／dry_run の副作用ゼロ／禁止設定での config_error）。詳細は各 process の Behavior Specification。

**品質・安全**: `go build`/`go vet`/`staticcheck`/`go test -race -count=1`/`-count=10` すべて exit 0。`swift build -c release` が warning 0 件、`swift test` が exit 0。`bash scripts/forbidden-audit.sh` が exit 0。既存 `server_test.go` の削除行 0 かつ `probe.go` の diff 空。`plutil -lint` が OK で `LSUIElement` が true、backend が同梱・実行可能、ad-hoc 署名を報告。E2E ログにメール・トークンが 0 件。1 万サイクル後の goroutine/FD 増分 0。

**ドキュメント**: `README.md`／`docs/protocol-v1.md`／`docs/config-schema.md`／`config.example.yml` の 4 点が存在。protocol-v1.md のメソッド集合が `ready.capabilities` 15 要素と完全一致（機械照合）。config-schema.md に「config.yml を書き換えられる利用者は本アプリの権限で任意コードを実行できる」を明記。RESEARCH.md は追記のみ（削除行 0）。未導入 linter の記載 0 件。

---

# Docs to Update

| パス | 更新内容 | 必須条件 |
|------|---------|----------|
| `README.md` | 概要／macOS 前提／ビルド手順（go build → swift build → make-app-bundle.sh）／設定の作り方／ad-hoc 署名による Gatekeeper 警告の回避手順／ログイン時起動の有効化（既定 OFF）／既知の制約（強制終了なし・URL誘導なし・最大60秒の是正遅延・tmux/nvim 未対応・Windows 未対応）／障害時 runbook | 必須（P200）。未導入 linter を書かない |
| `docs/protocol-v1.md` | 全 12 メソッド・4 イベントの JSON スキーマ／capabilities 15 要素と確定順序／エラーコード全 19 種 | 必須（P201）。`TestCapabilitiesMatchDocs` がパースできる形式 |
| `docs/config-schema.md` | 全キー・型・既定値・範囲／アクション 6 種／検証エラー 15 種／権限違反とスキーマ違反の 2 段階分岐／**任意コード実行の明記** | 必須（P202） |
| `config.example.yml` | 動作するサンプル。bundle_id はプレースホルダ、絶対パスは `<YOUR_PATH>` | 必須（P01）。実バンドル ID を含まない |
| `RESEARCH.md` | **追記のみ** — F4（`in-progress count` は出力文字列でない）／`tcc2 --version` 不在／実応答フォーマット（F2/F3/F5/F6）／Timezone と Start of Day（F8）／`tcc2 status` 形式（F10）／本 MVP は macOS のみ | 必須（P203）。削除行 0 |
| `docs/macos-verification-<date>.md` | VM-1〜VM-5 の結果／§18 段階4・6 の達成状況／OQ-1〜OQ-3 の判定結果 | 条件付き必須: P09 完了かつ手動検証を実施したとき |
| `docs/screenshots/vm1-menu.png` 等 | VM-1 / VM-5 の手動撮影画像 | 条件付き必須: `visual_scope:true` の P09 のゲート証跡 |
| `docs/wsl-verification-2026-08-01.md` / `RESEARCH-wsl.md` / `scripts/verify-wsl-backend.py` | — | 対象外: Windows/WSL はスコープ外。既存資産として温存し 1 行も変更しない |
| `.serena/` / `CHANGELOG.md` / `LICENSE` / `docs/adr/` | — | 対象外: Serena 設定は成果物でない／初回リリース前で変更履歴の対象がない／配布計画がない／設計判断は Why コメントと本計画書で記録済み |
| `.gitignore` | `dist/` の追加（.app 成果物をコミットしないため） | 条件付き: P08 実装時に判断 |

---

# Don'ts（禁止事項）

各項目に検出 grep を併記する。`scripts/forbidden-audit.sh` がこの順で実行し、1 つでも不一致なら exit 1。

- **D-01 強制終了の禁止** — `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal cmd macos/Sources scripts` → **期待 1 件**（`BackendClient.hardKill` のみ。自プロセスの子の回収は例外）
- **D-02 プロセス名だけによる終了の禁止** — `rg -n '\bpkill\b|\bkillall\b|runningApplications\(\)' internal cmd macos/Sources` → **期待 0 件**
- **D-03 シェル文字列連結の禁止** — `rg -n '"/bin/sh"|"-c"|bash -c|zsh -c|sh -c' internal cmd` → **期待 1 件**（`allow_shell` ガード内のみ）
- **D-04 stdout へのプロトコル外書き込み禁止** — `rg -n 'fmt\.Print|os\.Stdout|println\(' internal cmd` → **期待 1 件**（main.go の `NewServer` 引き渡しのみ）
- **D-05 Windows/WSL 向けコードの追加禁止** — `rg -n 'wsl\.exe|GOOS=windows|go:build windows|NotifyIcon|PowerShell|wslpath' internal cmd macos/Sources scripts/make-app-bundle.sh scripts/dev-run.sh` → **期待 0 件**（既存 verify-wsl-backend.py と docs は検査対象外）
- **D-06 秘密情報の出力禁止** — `rg -n 'Logged in as|\bEmail\b|Bearer|password|secret|credential' internal macos/Sources` → **期待 2 件**（判定用と redact パターン定義のみ。値を保持・出力しない）
- **D-07 バンドルID・絶対パスの直書き禁止** — `rg -n 'com\.tinyspeck|com\.amazon\.Lassen|/opt/homebrew|/Users/takets' internal cmd macos/Sources --glob '!**/testdata/**'` → **期待 0 件**
- **D-08 `in-progress count` への依存禁止** — `rg -ni 'in-progress count|in_progress_count' internal cmd macos/Sources` → **期待 0 件**（実測 F4 で出力に存在しないことを確認済み）
- **D-09 マジックナンバー直書き禁止** — `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/{engine,rules,state,ledger,config}` → **期待 0 件**（constants.go 自身と *_test.go は対象外）
- **D-10 単調時計による一時停止期限判定の禁止** — `rg -n 'time\.Since|monotonic|remainingSeconds|elapsedSeconds|time\.Tick' internal/state` → **期待 0 件**（スリープ跨ぎ復帰のため絶対時刻比較のみ）
- **D-11 設定検証エラー時の部分適用の禁止** — `pattern: n/a`（構造的性質のため grep 不能）。代替照合: `rg -c 'errors\s*\)\s*>\s*0' internal/config/config.go` が **1 件以上**、かつ P10 の `TestLoadInvalidReturnsNilConfig` が pass。最終判断は `FINAL-VG-04` の意図一致レビューへ送る
- **D-12 「後で決める」型の未決事項をコードに残すことの禁止** — `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal cmd macos/Sources scripts` → **期待 0 件**
- **D-13 ゲートへの固定リトライ回数の埋め込み禁止** — `pattern: n/a`（ゲート表は Markdown でコード grep 対象外）。代替照合: `rg -n 'retry.*[0-9]+\s*回|retries?\s*[:=]\s*[0-9]' PLAN-for-mac.md plan-for-mac/` が **0 件**。曖昧表現の混入は `FINAL-VG-04` のレビューへ送る
- **D-14 `.xcodeproj`/`xcodebuild`/XCUITest の導入禁止** — `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' . --glob '!docs/**' --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **期待 0 件**
- **D-15 未導入 linter の記載禁止** — `rg -n 'golangci-lint|swiftlint|swift-format' . --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **期待 0 件**（実測 F12 で未導入）

---

# Risks

| リスク | 対策 |
|--------|------|
| **R1 `tcc2` の表示テキスト形式変更** — 構造化応答が存在せず（実測 F1）テキスト以外の経路がない。解析が壊れて「実行中0件」と誤判定すると `not_contains` ルールが全発火し、**エラーにならずに利用者のアプリが意図せず終了させられる** | ①`unrecognized_format` センチネル（非空行があるのに `- [` 行が 0 件なら解析失敗）。実測 F3 で Done 111/Todo 4 行あり、通常の1日で 0 件になることは事実上ない ②黄金 fixture テスト（P11-VG-02 が StatusCounts を照合） ③未知 ID キーを warnings に積む ④cli_version と server_version を status に常時表示 ⑤行頭リテラルを定数 1 箇所に集約 |
| **R2 ad-hoc 署名下の通知/ログイン項目（未検証）** — `UNUserNotificationCenter` と `SMAppService.mainApp.register()` の成否が未実測 | ①通知を最初からベストエフォート実装にし、失敗時は `Notifier.recent`（容量 `notifierRecentCapacity`）へ積んでメニュー内「最近の警告」に必ず表示 ②メニューバーアイコンの状態変化を第一の伝達手段にする ③SMAppService 失敗をメニューに明示（既定 OFF なので既定動作は壊れない） ④VM-1 で人が確認し記録 |
| **R3 `Start of Day` の符号解釈不定** — 単日クエリだと 00:00–05:00 帯で取りこぼし、判定5（正常系）を通って**静かに誤動作**する | ①`TaskChuteQueryDays`=2 の範囲クエリを既定にし、どちらの解釈でも正しい日を含む構造にする（失敗モードを構造的に消す） ②`## YYYY-MM-DD` 見出しで各エントリに日付を帰属させ `running_tasks[].date` として露出 ③00:00–05:00 帯で人が 1 回だけ確認し記録 |

---

# Final Gates

<!-- sync: make-plan-gates -->
> 列定義は refs/make-plan-gates.md §3/§5 を正とする。execution_contract.gates の投影。

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---------|-------|------|----------|----------|---------|-------------|---------------|---------------|----------------|----------------|-------------|
| FINAL-VG-01 | green | quality | agent | true | `bash scripts/verify-all.sh` | 全 task_delta | exit==0 かつ最終行が `ALL AUTOMATED VERIFICATION PASSED` | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:quality, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | QUALITY-01,SCOPE-01,DONT-01 |
| FINAL-VG-02 | green | conformance | agent | true | `bash scripts/forbidden-audit.sh && git diff --stat internal/tcc2/probe.go && git diff -- internal/protocol/server_test.go \| rg '^-' \| rg -v '^---'` | 全 task_delta | 第1が exit==0、第2・第3の出力がいずれも空 | 同上 | 同上 | SC-08 | DONT-01,SCOPE-01 |
| FINAL-VG-03 | green | manual | human | true | VM-1〜VM-5 を実施し `docs/macos-verification-<date>.md` に記録 | P09 の task_delta | 記録ファイルに VM-1..VM-5 の全項目が `[x]` または「未達(理由)」で分類され、未分類 0 件。OQ-1/OQ-2/OQ-3 の判定結果が記載されている | GoalEvidence（記録ファイルのパスと分類件数） | failure_class:manual, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-05 | QUALITY-01 |
| FINAL-VG-04 | green | review | agent | true | 反証レビュー: D-11（部分適用の抜け道）・D-13（pass_criteria の曖昧表現）・Left to Implementation 禁則違反・Behavior Spec の post_state トレーサビリティを反証する証拠を探す | 全 task_delta | 構造化出力 `{conforms:true, violations:[]}`。violations が空でない場合は fail | GoalEvidence（構造化出力を逐語） | failure_class:review, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | DONT-01,SCOPE-01 |

> 全必須 `SC-01`〜`SC-10` に `FINAL-VG-01` と `FINAL-VG-04` が到達する。

---

# Verification

**Manual**: VM-1（メニュー表示・human）／VM-2（終了拒否・human）／VM-3（スリープ跨ぎ復帰・human）／VM-4（起動検出即時是正・human）／VM-5（バックエンド強制停止と制御解除・agent＋表示のみ human）。詳細は各 process の Manual Verification。
**Automated**: `bash scripts/verify-all.sh`（= verify-go.sh → verify-swift.sh → make-app-bundle.sh → plutil -lint → forbidden-audit.sh）。実行時間の見積りは 5 分未満（`go test -count=10` が支配的）。
**（任意）Rebuild test 観点**: 本計画と process ファイルのみを文脈なしで実装者/AI に渡して再生成したとき、RPC 契約・パーサ分岐・config 検証 15 種・状態遷移が一致するか。不一致箇所は暗黙の決定の記録漏れシグナル。
**（任意）解釈一意性テスト**: 独立した複数の実装者に同じ計画を渡し、`[In Progress]` 解析の 6 分岐と「解析失敗 vs 実行中なし」の型区別が同一に実装されるか。divergence した箇所が曖昧さのシグナル。
</content>
