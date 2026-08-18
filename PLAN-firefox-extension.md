---
task_id: "T-20260810-firefox-extension"
title: "Firefoxドメイン遮断拡張"
status: planning
created: "2026-08-10"
scope:
  - "既存タスクルールからFirefox通常ウィンドウ用のドメイン遮断ポリシーを生成する"
  - "Native Messaging HostとFirefox拡張をmacOSアプリへ導入する"
depends_on: []
risk_flags:
  - "native_messaging_boundary"
  - "fail_open_stale_state"
  - "firefox_permission_and_redirect"
  - "config_filename_compatibility"
quality_gate:
  command: "go test ./... -race -count=1 && swift test --package-path macos && web-ext lint --source-dir firefox-extension --no-config-discovery"
  min_quality_score: "-"
commit_mode: manual
loop_ready: true
light_mode: false
---
<!-- loop_ready: 実行時は各 process の自己完結ゲートと本書の最終ゲートを順に満たす。 -->

# Commander's Intent

## Purpose

タスクシュートの実行中タスクに応じて、指定ドメインへの寄り道をFirefox通常ウィンドウで遮断する。常駐デーモンを増やさず、既存のmacOS連携と設定利用者を壊さない。

## End State

`ensure[].type: browser.block` の `domains` を正規化・和集合化し、既存バックエンドが原子的なポリシーとバックエンド鮮度を更新し、Swift側がアプリ所有者ハートビートを書き出す。Firefox接続中だけ起動するNative Messaging Hostが両方を読み、拡張が対象メインフレームを拡張内説明ページへ同期リダイレクトする。アプリ停止、バックエンド停止、設定不備、タスクなし、Host切断では許可へ戻る。

## Key Tasks

- 設定・検証・ルール計画へ `browser.block` とドメイン正規化を追加する。
- 既存NDJSONプロトコルを変更せず、ポリシー状態とハートビートを出力する。
- Native Messagingの4バイト長フレームとHostマニフェストを実装する。
- `webRequestBlocking`、通常ウィンドウ限定、説明ページ、再接続を実装する。
- 同梱、導入、削除、受入検証、既存回帰を文書化する。

---

# ★ Constants（唯一の正）

| 定数名 | 値 | 単位 | 備考 |
|---|---|---|---|
| `BrowserBlockActionType` | `browser.block` | 文字列 | `ensure`専用の公開アクション型 |
| `BrowserBlockDomainsKey` | `domains` | 文字列 | YAMLキー |
| `BrowserPolicyVersion` | `1` | 整数 | 状態ファイルとHostメッセージ |
| `BrowserPolicyFileName` | `firefox-browser-policy.json` | ファイル名 | 原子的に置換 |
| `BrowserOwnerHeartbeatFileName` | `firefox-owner-heartbeat.json` | ファイル名 | 所有者の生存確認 |
| `BrowserStateDirRelative` | `TCCLocalConnector/BrowserPolicy` | 相対パス | macOS標準Application Support配下 |
| `FirefoxNativeHostName` | `jp.takets.tcc_local_connector.firefox` | 識別子 | Hostマニフェスト名 |
| `FirefoxExtensionID` | `firefox-domain-blocker@tcc-local-connector.takets.jp` | 識別子 | `allowed_extensions`と一致 |
| `NativeMessageHeaderBytes` | `4` | bytes | Native Messaging長さヘッダー |
| `NativeMessageMaxPayloadBytes` | `65536` | bytes | アプリ側の受信・送信上限 |
| `BrowserPolicyMaxDomains` | `128` | domains | ポリシー上限 |
| `BrowserDomainMaxBytes` | `253` | bytes | DNS名上限 |
| `BrowserOwnerHeartbeatIntervalSeconds` | `5` | seconds | Swift側の更新間隔 |
| `BrowserBackendHeartbeatIntervalSeconds` | `5` | seconds | バックエンドが同じ有効ポリシーを再発行する間隔 |
| `BrowserLivenessTTLSeconds` | `15` | seconds | ポリシーと所有者心拍の双方に適用。期限切れは許可 |
| `NativeHostPollIntervalMilliseconds` | `1000` | milliseconds | Hostの状態監視間隔 |
| `NativeReconnectInitialSeconds` | `1` | seconds | 拡張の再接続待機 |
| `NativeReconnectMaxSeconds` | `30` | seconds | 再接続待機の上限 |
| `BrowserPrivateWindowPolicy` | `not_allowed` | manifest値 | プライベートウィンドウ対象外 |
| `BrowserFailurePolicy` | `fail_open` | 文字列 | 欠損・破損・切断時の共通方針 |
| `BlockedPagePath` | `blocked.html` | 拡張パス | 遮断時の説明ページ |
| `BlockedPageReason` | `現在のタスクにより、このドメインは遮断中です。` | 表示文言 | タスク名は表示しない |
| `FirefoxCompatibilityBaseline` | `対象macOSのFirefox ESR` | 対応範囲 | P05で実機版を記録する |

> 数値・識別子をProcessへ転記するときは、この表の定数名を参照する。正本を変更した場合は全Processを再生成する。

# Scope

**対象**:

- macOS上のFirefox通常ウィンドウ。
- `ensure`内の `browser.block`、ドメイン単位の設定、正規化、重複除去、ソート。
- 既存バックエンドからの状態出力、Native Messaging Host、Firefox拡張、説明ページ。
- `dry_run`時の非遮断と予定通知、アプリ・Host・設定異常時のフェイルオープン。
- Native Hostの同梱・利用者単位マニフェスト導入・削除・受入検証。

**対象外（理由付き）**:

- Firefoxプライベートウィンドウ — 実装を増やさず `incognito: not_allowed` で対象外にする。
- URLパス、正規表現、ワイルドカード、ポート、IP単位 — 初期要件はドメイン単位に限定する。
- HTTPサーバー、Unixソケット、launchd、systemd、常駐デーモン — Native Messaging Hostを接続時だけ起動する。
- Chrome、Safari、Firefox Android、Windows、Linux — 今回の実装対象をmacOS Firefoxに限定する。
- moneyballの短時間バイパス、タスク名・完全URLの保存 — 要件とプライバシー境界に含めない。
- 既存の `protocol.Server` と `Version=1` の変更 — macOSクライアント互換性を維持する。
- 既存の未追跡 `.claude/`、`PLAN.md`、`plan/`、`docs/requirements/` の書換え — 今回の計画生成対象外として保持する。

# Assumptions / Open Questions

| 種別 | 項目 | 内容 | 外部挙動への影響 | 解決方針 |
|---|---|---|---|---|
| Assumption | `config.yaml`表記 | 現行の正規パス `~/.config/tcc-local-connector/config.yml` をYAML設定の正本として維持する。別名自動探索は追加しない。 | 既存設定を壊さず、新アクションは同じ設定へ追加する。 | 実装前にドキュメントで明記し、文字どおりの別ファイル名が必要なら別要件にする。 |
| Assumption | ルール合成 | 複数の有効 `browser.block` は優先度上書きではなく和集合にする。 | どの一致ルールでも指定ドメインを遮断する。 | Process 01の純粋関数テストで固定する。 |
| Assumption | 状態期限 | ポリシーまたは所有者心拍が `BrowserLivenessTTLSeconds` を超えたら即時に空集合を送る。 | 最大期限経過後はフェイルオープンになる。 | Process 02〜04の契約と実機試験で固定する。 |
| Assumption | 表示情報 | 説明ページには正規化済みホスト名だけを表示し、完全URL・タスク名・識別子を渡さない。 | 利用者への説明とプライバシーを両立する。 | Process 05のUI契約で固定する。 |

# Implementation Steps

| Process | 内容 | 依存 | Disjoint | 主な完了条件 |
|---|---|---|---|---|
| P01 | 定数、設定、ドメイン検証、ルールからの和集合 | - | y | 無効入力拒否と既存アクション回帰 |
| P02 | ポリシー発行、5秒再発行、Engine出力 | P01 | y | 原子性・破損時許可・dry_run |
| P03 | Swift所有者ハートビート、アプリ終了時の失効 | P02 | y | 正常応答だけが生存更新になる |
| P04 | Native Messaging Host、フレーム、状態監視 | P02 | y | stdout汚染なし、切断時許可 |
| P05 | Firefox拡張、ドメイン照合、説明ページ | P03, P04 | y | 通常窓遮断・private非作用・再接続 |
| P06 | Host同梱、マニフェスト導入・削除 | P04, P05 | y | 絶対パス・ID・権限の検査 |
| P07 | 結合検証、文書、ロールバック、受入 | P01-P06 | n | 自動ゲートと実機受入の完了 |

**DAG**: `P01 → P02 → {P03, P04} → P05 → P06 → P07`

# Conflict Matrix

| Process | Symbol targets（file:symbol） | Disjoint | Evidence |
|---|---|---|---|
| P01 | `internal/constants/constants.go:*`; `internal/config/config.go:Action`; `internal/config/validate.go:validateAction`; `internal/rules/plan.go:BuildPlan` | true | LSP調査結果。P01専有 |
| P02 | `internal/browserpolicy/*`; `internal/engine/engine.go:RunCycleNow` | true | 新規パッケージ＋Engine境界。P01完了後 |
| P03 | `macos/.../MenuController.swift:synchronize`; `macos/.../BrowserPolicyHeartbeat.swift:*` | true | Swift側専有 |
| P04 | `cmd/tcc-firefox-native-host/*` | true | 新規コマンド専有 |
| P05 | `firefox-extension/*` | true | 新規拡張専有 |
| P06 | `scripts/make-app-bundle.sh`; `scripts/install-firefox-native-host.sh`; `scripts/uninstall-firefox-native-host.sh` | true | 導入資材専有 |
| P07 | `scripts/verify-firefox-extension.sh`; `README.md`; `MANUAL.md`; `docs/protocol-v1.md` | false | P01/P06完了後の統合・文書担当。直列実行 |

# Wave Progress Map

| Wave | Processes | Depends on | Disjoint | Status |
|---|---|---|---|---|
| W01 | P01 | - | y | - [x] completed |
| W02 | P02 | W01 | y | - [x] completed |
| W03 | P03, P04 | W02 | y（状態契約を共有するため確認後並列） | - [x] completed |
| W04 | P05 | W03 | y | - [x] completed |
| W05 | P06 | W04 | y | - [x] completed |
| W06 | P07 | W05 | n（直列） | - [ ] pending: 対象Firefox ESRの手動受入記録 |

**Overall**: 6/7 completed

# Behavior Specification

| 入力・現状態 | 出力 | post_state / invariant |
|---|---|---|
| 有効タスク、`dry_run=false`、`X.COM.`と`youtube.com` | 正規化済みポリシーを出力し、Hostが拡張へ送る | `x.com`、`youtube.com` と配下サブドメインだけを遮断 |
| 複数有効ルール | 重複なし・辞書順のドメイン集合 | 優先度上書きなし。集合を置換する |
| 対象タスクなし、ルールなし | `enforce=false`、空集合 | 既存遮断を消去し通常閲覧を許可 |
| 構文不正、権限不正、状態ファイル欠損・破損 | 空集合相当、診断はログ／stderr | `BrowserFailurePolicy=fail_open` |
| `dry_run=true`、予定集合が非空かつ前回から変化 | 遮断集合は空。既存 `notify` に固定タイトルと予定ドメインを1回渡す | Firefoxは遮断しない。重複通知は集合fingerprintで抑制 |
| `dry_run=true`、予定集合が空または不変 | 通知なし、遮断集合は空 | Firefoxは遮断しない |
| pause中、取得失敗のgrace超過、release_controls | `enforce=false` の空集合を出力 | 既存のアプリ制御解除方針と同じく通常閲覧を許可 |
| アプリ停止、バックエンド停止、ポリシーまたは所有者心拍の期限切れ | Hostが空集合を送る | ポリシーと所有者心拍の**両方**が `BrowserLivenessTTLSeconds` 内の場合だけ遮断 |
| Native Host切断・接続失敗 | 拡張が即時空化して再接続を待つ | 再接続成功まで許可、古いPortの遅延更新は無視 |
| Host再接続 | 現在の有効スナップショットを初回送信 | 最新世代だけを適用 |
| private window | 拡張が注入されない | `incognito=not_allowed`。本機能は作用しない |
| 対象メインフレームへの遷移 | `webRequest.onBeforeRequest` が `blocked.html?host=...` へ同期リダイレクト | 元ページを表示せず、サブリソースは処理しない |

ドメイン一致は `host === rule || host.endsWith("." + rule)`。設定値はASCII小文字化、末尾ドット除去、ASCIIのDNS LDHラベル検証を行い、空ラベル、URL・パス・ポート・ワイルドカード・IP・非ASCII IDNを拒否する。IDNはpunycode値を設定する。

Native MessagingのHost→拡張メッセージは次の形に固定する。`type=policy`、`version=BrowserPolicyVersion`、`generation`、`enforce`、`dry_run`、`domains`、任意の`planned_domains`だけを含める。拡張→Hostの初回メッセージは `type=hello` と `version`。未知の型・版・上限超過・古いgenerationは無視または空集合として扱い、Hostのstdoutには診断を出さない。

## 状態ファイルJSON契約（正本）

- `BrowserPolicyFileName` は必須キー `version: integer`、`generation: integer`、`enforce: boolean`、`dry_run: boolean`、`domains: string[]`、`planned_domains: string[]`、`updated_at: RFC3339Nano UTC string` を持つ。`generation` はbackendだけが単調増加させる。
- `BrowserOwnerHeartbeatFileName` は必須キー `version: integer`、`updated_at: RFC3339Nano UTC string` だけを持つ。Swiftはgenerationを書かない。
- Hostは両ファイルのversionが `BrowserPolicyVersion` と一致し、両方のupdated_atが未来でなく `BrowserLivenessTTLSeconds` 内である場合だけ `enforce=true` のdomainsを採用する。その他は空集合にする。
- `dry_run=true`、`enforce=false`、domains空、未知キー・型不正・上限超過・generation後退は拡張へ空集合を送る。未知キーは無視せず、契約外の必須値不備としてfail-openにする。
- `updated_at` はbackendが `BrowserBackendHeartbeatIntervalSeconds` ごとに同じ有効集合で更新するため、チェック間隔が長くてもbackend停止を検知できる。拡張は時刻を再検証せず、Hostから受けた有効なpolicyだけを適用する。

# Success Criteria

- **SC-01**: `x.com` と `sub.x.com` は遮断され、`x.com.evil.test` と無関係なドメインは許可される。
- **SC-02**: 大文字・末尾ドットを正規化し、無効なドメイン形式を設定検証で拒否する。
- **SC-03**: 複数ルールの集合、`ensure`限定、`dry_run`非遮断が自動テストで確認できる。
- **SC-04**: 状態ファイルの原子性・破損と、バックエンド停止＋Swift心拍生存、Swift停止＋バックエンド生存、pause、取得失敗grace超過、release_controlsの全組合せで、ポリシーと所有者心拍の論理積が崩れた後に許可へ戻る。判定比較は`<=`とし、最悪遅延は `BrowserLivenessTTLSeconds + NativeHostPollIntervalMilliseconds` 以下（16.000秒以下）とする。
- **SC-05**: Native Messagingの長さフレーム、上限、接続・切断・再接続を確認できる。
- **SC-06**: 通常窓の説明ページ遷移、private非作用、権限・ID・マニフェストを確認できる。
- **SC-07**: 既存の `app.*`、`notify`、`process.*`、`command.run`、NDJSON通信と既存テストが回帰しない。
- **SC-08**: 導入、削除、更新、ロールバックを文書だけで再現できる。

# Deep Review

**候補比較**: 状態スナップショット＋Host変換層を採用する。Hostが設定・tcc2を直接評価する案は評価二重化とアプリ停止時の遮断残留を招くため不採用。HTTP／launchd／systemd案は常駐経路を増やすため不採用。拡張が設定を直接読む案はWebExtensionの権限境界を越えるため不採用。バックエンド鮮度とSwift所有者心拍の二重livenessを採用し、どちらか一方が停止しても許可へ戻す。

**Devil's Advocate**: 状態ファイル＋二重livenessは複雑性を増やすが、アプリ停止時・バックエンド停止時の許可という要件を満たすための明示的な所有者境界である。15秒TTLは遮断を緩めるが、フェイルオープン要件では古い遮断を残す方が重大な違反である。同期 `webRequest` はFirefox版差異のリスクがあるため `FirefoxCompatibilityBaseline` で実機試験を必須にする。

**Fact / Inference / Hypothesis**: Fact＝既存はNDJSON、Firefox Native Messagingは4バイト長、macOS executorは既知のアクションだけを受ける。Inference＝`browser.block`を既存 `Plan.Actions`へ混ぜると互換性を壊す。Hypothesis＝状態ファイル境界と二重livenessなら、Hostを常駐デーモン化せずフェイルオープンを保証できる。HypothesisはP02〜P07の自動・実機検証で確定する。

**Pre-mortem**: (1)バックエンド停止後もSwift心拍で遮断が残る→ポリシー鮮度を論理積にし、両停止組合せを試験、(2)Host接続不能→マニフェストの名前・絶対パス・ID検査、(3)元ページが一瞬見える→同期リダイレクト実機試験、(4)新設定で旧版が壊れる→導入順とロールバックを文書化、(5)移設後にHostパスが古い→導入スクリプトで毎回再生成する。

**三つの地平**: 短期は最小遮断とフェイルオープン、 中期はFirefox更新・アプリ更新の導入健全性、長期は他ブラウザや詳細ルールを別要件として再評価する。

# Security / Privacy / Operations

- Hostマニフェストは固定 `FirefoxExtensionID` のみ許可し、Host実行ファイルは利用者のみ書込み可、状態ディレクトリは `0700`、状態ファイルは `0600` とする。
- Native Hostのstdoutはフレーム専用、診断はstderr。URL、タスク名、TaskChute識別子は保存・ログ出力しない。
- リクエストフックではI/Oを行わず、メモリ上の最大 `BrowserPolicyMaxDomains` 件だけを比較する。
- 状態更新は一時ファイル→同期→rename、読込み不整合は許可として扱う。
- 導入順は「新アプリ→Hostマニフェスト→拡張→dry_run→少数ドメインの本番化」。ロールバックは拡張無効化、マニフェスト削除、設定除去で行う。

# Documentation Update Plan

- `config.example.yml` と `docs/config-schema.md`: `browser.block`、`domains`、正規化、`ensure`限定、既存 `config.yml` 正本。
- `README.md` と `MANUAL.md`: Firefox拡張導入、Hostマニフェスト、権限、dry_run、private対象外、フェイルオープン、削除手順。
- `docs/protocol-v1.md`: 既存NDJSONは変更せず、Native Messagingは別フレーミングであること。
- `scripts/verify-firefox-extension.sh`: 自動検証の入口と実機前提を記録する。
- 既存の `docs/requirements/firefox-domain-blocking.md` は参照元として保持し、計画生成では上書きしない。

# DONTs

- HTTP待受、launchd、systemd、常駐Native Hostを追加しない。
- `protocol.Server` の改行区切りJSON、版1、既存macOSクライアントを変更しない。
- `browser.block`をmacOS側の既存 `Plan.Actions`へ流さない。
- `dry_run`中、Host切断後、ハートビート期限切れ後に遮断集合を保持しない。
- private browsingを見える状態として扱わない、完全URL・タスク名を外部へ渡さない。
- 未追跡の既存計画・要件・`.claude`資材を上書きしない。

# Integration Gates and Final Gates

各Processのcoreは、Implementation Brief、Symbol Targets、ローカル定数、振る舞い仕様、機械判定可能なゲート、失敗時の扱い、証跡仕様を自己完結で持つ。appendixは背景・Why・手動確認だけを置き、実行のための必須参照にはしない。分冊生成が完了するまで `loop_ready` は実行可能とみなさない。

# Integration Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `W01-IG-01` | W01 | automated | CI | true | `go test ./internal/config ./internal/rules -race -count=1` | P01の設定・ルール差分 | 終了コード0、SC-01・SC-02・SC-03のテスト成功 | テスト出力と差分 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-01, SC-02, SC-03 | config_validation |
| `W02-IG-01` | W02 | automated | CI | true | `go test ./internal/browserpolicy ./internal/engine -race -count=1` | P02のpolicy・Engine差分 | 終了コード0、SC-03・SC-07を満たす | テスト出力 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-03, SC-07 | policy_atomicity |
| `W03-IG-01` | W03 | automated | CI | true | `swift test --package-path macos && go test ./cmd/tcc-firefox-native-host -race -count=1` | P03/P04のSwift・Host差分 | 両方終了コード0、SC-04・SC-05・SC-07を満たす | Swift/Go出力とstdout検査 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-04, SC-05, SC-07 | dual_liveness; native_messaging |
| `W04-IG-01` | W04 | automated | CI | true | `web-ext lint --source-dir firefox-extension --no-config-discovery` | P05の拡張差分 | 終了コード0、SC-01・SC-06を満たす | lint出力、manifest検査 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-01, SC-06 | firefox_permissions |
| `W05-IG-01` | W05 | automated | CI | true | `scripts/install-firefox-native-host.sh --check` | P06の同梱・導入差分 | Host実行ファイル、絶対path、権限、allowed_extensions一致 | 検査出力 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-06, SC-08 | native_host_installation |

# Final Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `FINAL-01` | final | automated | CI | true | `go test ./... -race -count=1 && swift test --package-path macos` | 全Go/Swift実装差分 | 終了コード0、SC-03・SC-04・SC-07を満たす | CI端末ログ、終了コード、テスト件数 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-03, SC-04, SC-07 | regression |
| `FINAL-02` | final | mixed | maintainer | true | `bash scripts/verify-firefox-extension.sh` | P01〜P07の統合成果物 | 自動検査成功、通常窓・切断・停止・private・対象Firefox ESRの手動項目を記録 | 受入記録、対象ESR版、各ケース結果 | `retry_budget_source=standard_gate_policy; stop_and_report` | SC-01, SC-02, SC-04, SC-05, SC-06, SC-08 | acceptance; rollback |

失敗時は変更を完了扱いにせず、該当Processへ戻って原因と証跡を更新する。再試行回数は共通ゲート方針に従い、固定回数を計画へ埋め込まない。

# Remaining Tasks

## RT-01: 既存 `internal/tcc2` テストの `broken pipe` 解消

- `status`: completed
- `scope`: Firefox W06の総合回帰を安定化するため、テストfixtureだけを最小修正した。
- `resolved_gate`: `FINAL-01`、および `scripts/verify-firefox-extension.sh` の `w06-go-regression`。
- `reproduction`: `go test ./internal/tcc2 -race -count=1`
- `resolution`: fake MCPプロセスが `initialize` 応答の後に `notifications/initialized` を受信するまでstdinを保持するようにし、初期化プロトコル上正しい書込みとの競合を解消した。
- `baseline_evidence`: `Open` は `initialize` 応答後に `notifications/initialized` を送るが、旧fake MCPは直後にstdinを閉じていたため、実装ではなくfixtureのプロトコル不整合で再現した。
- `suggested_investigation`: テスト用fake MCPプロセスが `initialize` 応答後すぐ終了する一方、`Open` が続けて `notifications/initialized` をstdinへ書く競合を確認し、テストfixtureまたは既存MCPセッション処理の責務境界で修正する。
- `verification`: `go test ./internal/tcc2 -race -count=20`、`go test ./... -race -count=1`、および `bash scripts/verify-firefox-extension.sh` が終了コード0。

Firefox ESR 140.13.0による通常窓遮断、Host切断、17秒後の期限切れ、古いgeneration、正本復帰、private非作用の受入項目は実施済みであり、RT-01には含めない。

# Research Basis

- 現行コード: `internal/config/config.go:57`、`internal/config/validate.go:125`、`internal/rules/plan.go:67`、`internal/protocol/server.go:21`、`macos/Sources/TCCLocalConnector/MenuController.swift:115`。
- 参考実装: `/Users/takets/repos/moneyball/browser-extension/firefox/background.js:16,95`。
- Firefox公式: [Native messaging](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Native_messaging)、[Native manifests](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Native_manifests)、[onBeforeRequest](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/webRequest/onBeforeRequest)、[incognito](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/manifest.json/incognito)。

# Generation Contract

- 正本: 本ファイルの `★ Constants`、DAG、Success Criteria、Verification Contract。
- 分冊: `plan-firefox-extension/process-01.md`〜`process-07.md` と各 `.appendix.md`。
- ★ Constants、ゲート、横断方針、Process境界を変更した場合は、`$make-plan --deep --prefix firefox-extension` で分冊を再生成する。
- 本計画は新規モードで生成した。既存の `PLAN.md`、`plan/`、要件書は変更しない。
