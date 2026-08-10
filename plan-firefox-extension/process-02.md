# P02: ブラウザポリシー状態・Engine連携・policy発行

## Implementation Brief

`internal/browserpolicy` にFirefox policyの正本型と原子的発行処理を追加し、`internal/engine` の `RunCycleNow`、`Reload`、`Pause`、`Resume` にpolicy発行を接続する。Engineはpolicyを構造検証して書く／更新するだけで、heartbeatを読み込まず、dual TTLを判定せず、Host向けeffective readを実装しない。Host専用のeffective readはP04の責務とする。

正本は `firefox-browser-policy.json` と `firefox-owner-heartbeat.json` である。policyは `version:int`、`generation:int`、`enforce:bool`、`dry_run:bool`、`domains:string[]`、`planned_domains:string[]`、`updated_at:RFC3339Nano UTC` の7必須キーだけを持つ。owner heartbeatは `version:int`、`updated_at:RFC3339Nano UTC` の2必須キーだけを持ち、`generation`を持たない。policy generationだけを単調増加させる。

## 変更ファイルと行番号

| ファイル | 行番号・対象 | 変更内容 |
|---|---|---|
| `internal/browserpolicy/policy.go` | 新規・全体 | policy型、構造検証、ドメイン集合の正規化、fingerprint、policy generationを定義する |
| `internal/browserpolicy/store.go` | 新規・全体 | policyの原子的書込み、空policy発行、書込み失敗時の現行正本保全を実装する。heartbeatは読み込まない |
| `internal/browserpolicy/policy_test.go` | 新規・全体 | 7キーschema、型、集合、generation、dry-run、fail-open用空policy、原子書込みを検証する |
| `internal/engine/engine.go` | `RunCycleNow`、`Reload`、`Pause`、`Resume` および既存Engine初期化 | policy発行、5秒再発行、dry-run通知、異常時の空policyを接続する。heartbeat読込みとHost判定は追加しない |
| `internal/engine/engine_test.go` | 既存サイクル・pause/resume周辺 | policy発行、5秒更新、dry-run通知、grace超過、`release_controls`、設定不正、書込み失敗を検証する |
| `internal/rules/plan.go` | 既存 `Plan.Actions` 境界 | `browser.block`を混ぜないことを確認する。実装変更はしない |
| `internal/protocol/server.go` | 既存 `Version`・書込み境界 | NDJSON/protocol version 1を変更しないことを確認する。実装変更はしない |

## Symbol Targets

- `internal/browserpolicy/policy.go`: `Policy`、`NormalizeDomains`、`MergeDomains`、`Fingerprint`、`Validate`、policy generation処理
- `internal/browserpolicy/store.go`: `Store`、`Publish`、`PublishEmpty`、`writeAtomic`
- `internal/engine/engine.go`: policy store保持領域、`New`、`Reload`、`RunCycleNow`、`Pause`、`Resume`、`ReportActions`
- `internal/rules/plan.go`: `BuildPlan`と既存 `Plan.Actions` の境界
- `internal/protocol/server.go`: `Version`と既存NDJSON書込み

## ローカル定数

| 定数名 | 採用値 | 用途 |
|---|---:|---|
| `BrowserPolicyVersion` | `1` | policyとheartbeatの版 |
| `BrowserPolicyFileName` | `firefox-browser-policy.json` | policy正本ファイル名 |
| `BrowserOwnerHeartbeatFileName` | `firefox-owner-heartbeat.json` | owner heartbeat正本ファイル名。P02は参照対象名だけを保持し、読み込まない |
| `BrowserBackendHeartbeatIntervalSeconds` | `5` | backendによるpolicy再発行間隔 |
| `BrowserOwnerHeartbeatIntervalSeconds` | `5` | P02は使用しない参照対象 |
| `BrowserLivenessTTLSeconds` | `15` | Hostだけが両正本の `updated_at` に適用するTTL。P02の実装判定には使わない |
| `BrowserPolicyMaxDomains` | `128` | policy配列の上限 |
| `BrowserFailurePolicy` | `fail_open` | 異常時に空policyを発行する方針 |

その他の既存境界値は変更しない。Swift側の間隔定数をP02のEngine実装から参照しないため、`BrowserOwnerHeartbeatIntervalSeconds` は仕様上の参照対象として記録するだけで、P02の実装対象外とする。

## 前提条件

- 上流で検証済みの `ensure.browser.block` から、重複を除いた辞書順の予定集合を受け取る。`on_enter`と`on_exit`はP02の入力にしない。
- policyの `domains` と `planned_domains` は検証済みのDNS名集合であり、URL、path、port、wildcard、IP、非ASCII IDN、空ラベルは上流で拒否される。
- owner heartbeatはSwift所有者が2キーだけで更新する。Engineはheartbeatを読み込まず、heartbeatとのgeneration一致を要求しない。
- Engineはpolicy JSONの7キーと型を検証して発行する。policyの `updated_at` はRFC3339Nano UTCで更新するが、TTLの有効判定はHostに委譲する。
- policyの保管場所は既存のApplication Support配下のBrowserPolicyディレクトリとする。既存 `Plan.Actions`、NDJSON/protocol version 1、既存通知経路は維持する。

## 実装手順

1. `Policy`を7キーの厳密なJSON型として定義し、未知キー、欠損、null、型不正、RFC3339Nano UTCでない時刻を拒否する。owner heartbeat型は契約を記録するだけで、P02のEngineから読み込まない。
2. 予定集合を重複除去・辞書順化し、`BrowserPolicyMaxDomains`またはドメイン長の上限違反を発行エラーにする。暗黙のURL解析やwildcard展開は行わない。
3. policyを発行するたびにpolicy側のgenerationだけを前回値から1増加させ、`updated_at`を更新する。同じ予定集合の再発行でも両方を更新し、heartbeat世代との一致は検証しない。
4. `writeAtomic`で同一ディレクトリの一時ファイルへ書き、ファイルsync、rename、ディレクトリsyncの順に完了させる。途中失敗では現行正本を壊さない。
5. `Engine.New`でpolicy storeを準備し、`RunCycleNow`成功時に通常は `domains=planned_domains=予定集合`、dry-run時は必ず `domains=[]`かつ `planned_domains=予定集合` を発行する。
6. backendの5秒timerで同じpolicyを再発行し、`updated_at`とpolicy generationを更新する。heartbeat読込み、両TTL判定、Host向けeffective readは追加しない。
7. dry-runの予定集合が非空で、前回fingerprintから変化した場合だけ、固定タイトル `ブラウザ遮断（dry-run）` のnotifyを1回発行する。同一集合、空集合への変化、設定なしでは通知しない。
8. Pause、取得失敗grace超過、`release_controls`、設定不正、書込み失敗では `enforce=false`、`domains=[]`、`planned_domains=[]` の空policyを扱う。notify失敗はpolicy発行の判定を変えない。
9. `Plan.Actions`に `browser.block` を追加せず、既存NDJSON/protocol version 1の形式を変更しないことをrules/protocolテストで固定する。

## Behavior Specification

### 正本schema

policy `firefox-browser-policy.json` は次の7キーだけを持つ。

```json
{
  "version": 1,
  "generation": 42,
  "enforce": true,
  "dry_run": false,
  "domains": ["example.com"],
  "planned_domains": ["example.com"],
  "updated_at": "2026-08-10T12:34:56.123456789Z"
}
```

owner heartbeat `firefox-owner-heartbeat.json` は次の2キーだけを持つ。P02はこのファイルを読まない。

```json
{
  "version": 1,
  "updated_at": "2026-08-10T12:34:56.123456789Z"
}
```

### Engine発行

| 条件 | 発行policy |
|---|---|
| 通常の取得・評価成功 | `enforce=true`、`dry_run=false`、`domains=予定集合`、`planned_domains=予定集合` |
| dry-run | `enforce=false`、`dry_run=true`、`domains=[]`、`planned_domains=予定集合` |
| タスクなし・ルールなし・設定なし | `enforce=false`、`dry_run=false`、両配列空 |
| Pause、取得失敗grace超過、`release_controls`、設定不正、書込み失敗 | `enforce=false`、`dry_run=false`、両配列空 |

policy発行ごとに `generation` は単調増加し、`updated_at` は更新される。heartbeatの読み込み、heartbeatとの世代一致、両正本の15秒TTL、Host向けeffective readはP02のBehaviorではない。HostだけがP04で両 `updated_at` を15秒TTL判定する。

### dry-run通知

- 予定集合が非空で、前回fingerprintと異なるときだけ固定タイトル `ブラウザ遮断（dry-run）` を1回通知する。
- 同一集合の周期再発行では通知しない。
- 非空集合から空集合への変化、空集合の再発行、設定なしでは通知しない。
- 通知に失敗してもpolicyはdry-runのまま発行し、`domains`を有効化しない。

### 境界

- `browser.block`は既存 `Plan.Actions` に入れない。
- 既存NDJSON/protocol version 1と既存通知経路の形式を変えない。
- P04がpolicyとheartbeatを読み、構造・時刻・generationを含むHost向けeffective readを担当する。P02はその判定を重複実装しない。

## Correctness Criteria

- **P02-CC-01**: policyが7必須キーだけ、heartbeatが2必須キーだけを指定型で表現し、heartbeatに `generation` を要求または出力しない。
- **P02-CC-02**: Engineがpolicyを構造検証して原子的に発行し、policy generationだけを単調増加させる。heartbeat読込みと世代一致要求がない。
- **P02-CC-03**: 通常時は両配列が予定集合、dry-run時は必ず `domains=[]`かつ `planned_domains=予定集合` になる。
- **P02-CC-04**: 5秒再発行で `updated_at` とpolicy generationが更新され、非空かつfingerprint変化時だけ固定notifyが1回発行される。空集合変化・不変・設定なしは通知しない。
- **P02-CC-05**: Pause、取得失敗grace超過、`release_controls`、設定不正、書込み失敗ではfail-openの空policyになる。
- **P02-CC-06**: `browser.block`が `Plan.Actions` に入らず、既存NDJSON/protocol version 1が変わらない。
- **P02-CC-07**: 原子的書込みは一時ファイル、ファイルsync、rename、ディレクトリsyncの順で行い、途中失敗で現行正本を壊さない。
- **P02-CC-08**: Engineはheartbeatと両TTLを判定せず、Host向けeffective readを実装しない。Host判定はP04に一元化する。

## Left to Implementation

- `internal/browserpolicy`の具体的なファイル分割、mutex粒度、時計注入型。
- Engineの既存実行コンテキストへ5秒timerを接続する具体的なtick実装。
- 書込み失敗時に現行正本を維持しつつ空policyへ遷移する具体的なエラー伝播。
- Swift owner heartbeatの更新実装。P02は変更しない。
- P04のHost effective read。P02は変更しない。

## Verification Gates

| gate_id | command | pass_criteria | evidence_spec | failure_policy | criterion_refs |
|---|---|---|---|---|---|
| P02-GATE-01 | `go test ./internal/browserpolicy -run 'TestPolicy|TestStore' -count=1` | schema、型、集合、generation、dry-run用policy、fail-open空policy、原子書込みの対象テストが成功する | 終了コード、対象テスト名、PASS行を記録する | 最初の失敗シンボルだけをP02対象内で修正し再実行する | P02-CC-01, P02-CC-02, P02-CC-03, P02-CC-04, P02-CC-05, P02-CC-07 |
| P02-GATE-02 | `go test ./internal/engine -run 'TestRunCycleNow|TestEngine_Pause|TestEngine_Resume|TestEngine_DryRun|TestEngine_ReleaseControls' -race -count=1` | 発行、5秒更新、dry-run通知、pause/resume、grace超過、`release_controls`、設定不正が成功する | policy実体、notify回数、終了コード、PASS行を記録する | Engineとpolicy発行の接続だけを切り分ける | P02-CC-03, P02-CC-04, P02-CC-05 |
| P02-GATE-03 | `go test ./internal/rules ./internal/protocol -run 'TestPlan_NoBrowserBlockInActions|Test' -race -count=1` | `Plan.Actions` と既存NDJSON/protocol version 1の回帰がない | 各パッケージのPASS行と終了コードを記録する | rules/protocolの実装変更はせず、P02の漏れを修正する | P02-CC-06 |
| P02-GATE-04 | `go test ./... -race -count=1` | 全Goテストが成功する | 最終成功行と終了コードを記録する | P02起因か既存回帰かを切り分け、無関係な範囲へ広げない | P02-CC-01, P02-CC-02, P02-CC-03, P02-CC-04, P02-CC-05, P02-CC-06, P02-CC-07, P02-CC-08 |
| P02-GATE-05 | `rg -n 'LoadEffective|isFresh|heartbeat.*generation|dual TTL|effective read' internal/browserpolicy internal/engine` | P02実装にHost専用のheartbeat読込み、dual TTL、effective readが存在しない | 一致なしの出力と終了コードを保存する | 一致箇所をP02対象から除去し、P04境界だけを文書化する | P02-CC-08 |
| P02-GATE-06 | `git diff --name-only -- plan-firefox-extension/process-02.md plan-firefox-extension/process-02.appendix.md && git status --short` | 本作業の変更対象がP02のcore/appendixだけである | ファイル一覧と未追跡既存資材を記録する | 指定外ファイルを変更しない | P02-CC-06, P02-CC-08 |

## 生成情報

- 担当: P02
- 題名: ブラウザポリシー状態・Engine連携・policy発行
- 作業場所: `/Users/takets/repos/tcc-local-connector`
- 更新対象: `plan-firefox-extension/process-02.md`、`plan-firefox-extension/process-02.appendix.md`
- 実装対象: `internal/browserpolicy/policy.go`、`internal/engine/engine.go`の `RunCycleNow`/`Reload`/`Pause`/`Resume`、既存rules/protocolテスト
- 変更対象外: 親PLAN、他process、実装コード、要件書、未追跡 `.claude/`、`PLAN.md`、`plan/`、`docs/requirements/`
- 再生成条件: policy schema、generation、5秒発行、dry-run通知、fail-open、原子書込み、P02の対象シンボルまたは検証ゲートを変更した場合
