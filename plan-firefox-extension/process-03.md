# P03 Swift所有者ハートビート

## Implementation Brief

macOSアプリがbackendから正常で新鮮な`status`応答を受け取ったときだけ、共有heartbeatを原子的に更新する。heartbeatはSwift所有者の生存を示す2項目JSONであり、policyの発行、policyの有効判定、backendのliveness判定は本processの責務に含めない。

実装は既存のNDJSON/protocol version 1、backend応答処理、`Plan.Actions`を変更しない。heartbeatの共有先はApplication Support配下の`TCCLocalConnector/BrowserPolicy`とする。HTTP、常駐サービス、`launchd`、`systemd`は追加しない。

## 変更ファイルと行番号

このprocessの実装対象はSwift実装2ファイルとSwiftテスト2ファイルの計4ファイルである。本再生成で変更する計画ファイルは次の2ファイルだけであり、実装コードとテストコードはこのprocessでは変更しない。

- 実装 `macos/Sources/TCCLocalConnector/MenuController.swift:106-113` — `quit`でheartbeatを停止または削除してからbackend終了を開始する。
- 実装 `macos/Sources/TCCLocalConnector/MenuController.swift:135-147,160-177` — `synchronize`と`consumeBackendMessages`で、正常な`status`応答の受理後だけheartbeat更新を許可する。
- 実装 `macos/Sources/TCCLocalConnector/BrowserPolicyHeartbeat.swift:1-end` — heartbeatの厳密なJSON、共有パス、原子的更新、停止・削除を実装する新規ファイル。
- テスト `macos/Tests/TCCLocalConnectorTests/MenuControllerTests.swift:1-end` — status成功・失敗・停止・pause・quitの連携を検証する。
- テスト `macos/Tests/ConnectorCoreTests/BrowserPolicyHeartbeatTests.swift:1-end` — heartbeatのJSON形、間隔、原子的更新、停止・削除、失敗時挙動を検証する新規ファイル。

計画ファイルの変更対象は`plan-firefox-extension/process-03.md`と`plan-firefox-extension/process-03.appendix.md`だけである。親PLAN、他process、実装コード、要件書、未追跡の`.claude/`、`PLAN.md`、`plan/`、`docs/requirements/`は変更しない。行番号は着手時点の基準であり、実装後に実行行を再確認する。

## Symbol Targets

- `MenuController.synchronize()`
- `MenuController.consumeBackendMessages()`
- `MenuController.quit()`
- `BrowserPolicyHeartbeat`
- `BrowserPolicyHeartbeatTests`
- `MenuControllerTests`

## ローカル定数

次の名前、値、単位を契約として固定する。Swift側は`BrowserOwnerHeartbeatIntervalSeconds`だけをheartbeat更新間隔として参照し、`BrowserBackendHeartbeatIntervalSeconds`を参照しない。

| 定数 | 値 | 使用責務 |
|---|---:|---|
| `BrowserPolicyVersion` | `1` | policyとheartbeatの契約版（heartbeat JSONの`version`値） |
| `BrowserPolicyFileName` | `firefox-browser-policy.json` | backendが発行するpolicy名。Swiftは書き換えない |
| `BrowserOwnerHeartbeatFileName` | `firefox-owner-heartbeat.json` | Swift所有者heartbeat名 |
| `BrowserBackendHeartbeatIntervalSeconds` | `5`秒 | backendのpolicy再発行間隔。Swift側では参照しない |
| `BrowserOwnerHeartbeatIntervalSeconds` | `5`秒 | Swift heartbeat更新間隔 |
| `BrowserLivenessTTLSeconds` | `15`秒 | Hostがpolicyとheartbeatの各`updated_at`を判定するTTL。P04の責務 |
| `BrowserPolicyMaxDomains` | `128` | policyの`domains`および`planned_domains`に対する上限契約 |
| `BrowserFailurePolicy` | `fail_open` | pause、取得失敗grace超過、`release_controls`、config不正、書込失敗時に`enforce=false`の空policyを扱う契約 |

## 前提条件

- 正本policy JSONの必須キーは`version:int`、`generation:int`、`enforce:bool`、`dry_run:bool`、`domains:string[]`、`planned_domains:string[]`、`updated_at:RFC3339Nano UTC`の7つだけである。policyの`generation`だけが単調増加し、heartbeatには`generation`を持たせない。
- 正本owner heartbeat JSONは`version:int`と`updated_at:RFC3339Nano UTC`の2つだけである。`generation`、policy項目、task data、PID、端末固有値を追加しない。
- Hostだけがpolicyとheartbeatの両`updated_at`を`BrowserLivenessTTLSeconds`の15秒TTLで判定する。P03のSwift実装はpolicyを読み込まず、heartbeatとのgeneration比較およびdual TTL判定を行わない。Hostのeffective readはP04の責務である。
- Engineはheartbeatを読み込まない。Engineはpolicyを構造検証して書く・更新するだけであり、backendは5秒ごとにpolicyを再発行する。
- `dry_run=true`のpolicyは必ず`domains=[]`、`planned_domains`は予定集合とする。予定集合が非空かつ前回fingerprintから変化した場合だけ、backendが固定notify「ブラウザ遮断（dry-run）」を1回送る。空集合への変化、不変、設定なしでは通知しない。
- pause、取得失敗grace超過、`release_controls`、config不正、書込失敗は`enforce=false`の空policyとする。既存`Plan.Actions`に`browser.block`を混ぜず、既存NDJSON/protocol version 1を変更しない。
- `status`応答は既存`StatusPayload`としてデコードでき、既存の正常稼働状態を示す場合だけheartbeat更新のトリガーになる。送信失敗、RPC error、デコード失敗、status停止、backend停止では更新しない。
- 共有ディレクトリはApplication Support配下の`TCCLocalConnector/BrowserPolicy`である。実装はディレクトリ0700、heartbeatファイル0600を設定し、テストは一時Application Supportルートを注入する。
- 正常なstatus応答を受理した時点のUTC時刻を`updated_at`へ書く。ファイルのmtimeを正本時刻にしない。

## 実装手順

1. `BrowserPolicyHeartbeat.swift`に`version`と`updated_at`だけを持つ`Codable`値型を定義し、UTC RFC3339Nano形式を固定する。
2. Application Support配下の共有ディレクトリを作成または検証し、0700を設定する。作成・権限・エンコード失敗はheartbeat更新失敗として扱い、アプリをクラッシュさせない。
3. 同一ディレクトリ内の一意な一時ファイルへ完全なJSONを書いて0600を設定し、原子的置換で`BrowserOwnerHeartbeatFileName`へ反映する。部分JSONや空ファイルを読者に見せない。
4. `stop()`で更新タイマーを無効化し、heartbeatを削除する。停止後の保留更新が再生成しないよう、既存のMainActor直列化または世代無効化を使う。対象不存在の削除は成功扱いにする。
5. `MenuController`はheartbeat所有者を1つだけ保持する。`synchronize`がstatusを要求し、`consumeBackendMessages`が正常で新鮮なstatus応答を受理した直後だけ、`BrowserOwnerHeartbeatIntervalSeconds=5`秒周期の更新を開始または継続する。`config_paths`だけの成功では更新しない。
6. backend停止、status停止、送信失敗、RPC error、デコード失敗、pause、アプリ終了、`quit`ではheartbeat更新を停止または削除する。`quit`はheartbeatを停止してからbackend shutdownを開始する。
7. `consumeBackendMessages`の通知、config path、plan、`Plan.Actions`報告は既存順序と意味を維持する。heartbeat更新失敗は既存の主処理や警告表示を壊さず、次の正常statusまで有効証明を延長しない。
8. 2つのテストファイルで、正常status、異常status、停止、pause、quit、再起動、JSON厳密性、原子的更新、権限・書込失敗を検証する。テストは実ユーザー領域へ書かない。

## Behavior Specification

### Heartbeatの正本形

heartbeatは次の2キーだけを持つ。キーの追加・欠落・型違い・時刻変換不能は無効として扱う。

```json
{"version":1,"updated_at":"2026-08-10T12:34:56.789000000Z"}
```

`updated_at`はUTC RFC3339Nanoであり、heartbeatに`generation`は存在しない。

### 更新条件

- 正常で新鮮な`status`応答を受理した時だけ、heartbeatを生成または更新する。
- `BrowserOwnerHeartbeatIntervalSeconds=5`秒はSwift所有者heartbeatの更新間隔である。`BrowserBackendHeartbeatIntervalSeconds`はbackendのpolicy再発行間隔であり、Swift側で参照しない。
- status送信失敗、RPC error、応答停止、デコード失敗、backend停止では更新しない。
- pause、取得失敗grace超過、`release_controls`、config不正、書込失敗では、backend/Host側の正本に従い`enforce=false`の空policyとなる。Swift heartbeatは停止または削除し、鮮度を延命しない。
- `quit`およびアプリ終了ではタイマーを停止し、heartbeatを削除または無効化する。停止後に保留処理が再生成しない。

### 責務境界

- Hostだけがpolicyとheartbeat双方の`updated_at`を15秒TTLで判定する。P03はこの判定を実装しない。
- Engineはheartbeatを読み込まず、policyを構造検証して書く・更新するだけである。
- Hostのeffective readはP04で実装する。P03のSwift実装はpolicyを読み込まず、generationを比較しない。
- 既存NDJSON/protocol version 1、既存Native message、既存`Plan.Actions`を変更しない。

## Correctness Criteria

- P03-CC-01: heartbeat JSONが`version`と`updated_at`だけで、`version == BrowserPolicyVersion == 1`かつUTC RFC3339Nanoである。
- P03-CC-02: 正常で新鮮なstatus応答を受理した時だけheartbeatが生成・更新される。config path応答、通知、planだけでは更新されない。
- P03-CC-03: heartbeat更新間隔の正本は`BrowserOwnerHeartbeatIntervalSeconds=5`であり、Swift側が`BrowserBackendHeartbeatIntervalSeconds`を参照しない。
- P03-CC-04: status停止、backend停止、送信失敗、RPC error、デコード失敗、pause、アプリ終了、quitではheartbeatが停止または削除され、古いheartbeatを更新して延命しない。
- P03-CC-05: heartbeat更新は同一ディレクトリ内の一時ファイルから原子的に行われ、読者は完全な旧JSONまたは完全な新JSONだけを観測する。ディレクトリ0700、ファイル0600を守る。
- P03-CC-06: 更新・削除・権限・書込失敗はアプリをクラッシュさせず、次の正常statusまでheartbeatを有効証明として再生成しない。
- P03-CC-07: `quit`とタイマー、再起動と保留更新の競合でheartbeatが再生成されない。
- P03-CC-08: backendのpolicy schema、policy generation、dry-run notify、Engineのheartbeat非参照、Hostのdual TTL、P04のeffective read、既存NDJSON/protocol version 1、既存`Plan.Actions`は本processの実装に混入しない。

## Left to Implementation

- `BrowserPolicyHeartbeat`の可視性、MainActorまたはactor境界、時刻注入の形を既存Swift 6の警告を増やさない最小構成で決める。
- `StatusPayload`の正常状態判定は既存`BackendState`の値に合わせ、未知状態では更新しない。
- `replaceItemAt`の初回ファイル不存在時の挙動を確認し、初回作成と置換の双方で原子性を満たす。
- Timerのテスト可能な注入方法と、一時Application Supportルートの注入方法を既存テスト基盤に合わせて決める。

## Verification Gates

| command | pass_criteria | evidence_spec | failure_policy | criterion_refs |
|---|---|---|---|---|
| `cd macos && swift test --filter BrowserPolicyHeartbeatTests` | heartbeatテストが終了コード0 | JSON厳密性、5秒間隔、原子的更新、stop、削除、失敗時の各テスト名と結果 | 1件でも失敗なら未完了とし、P03対象内の原因だけを修正して再実行 | P03-CC-01, P03-CC-03, P03-CC-05, P03-CC-06, P03-CC-07 |
| `cd macos && swift test --filter MenuControllerTests` | MenuController既存テストとheartbeat連携テストが終了コード0 | 正常status、status失敗・停止、pause、quit、再起動の結果 | 失敗時は既存backend・plan処理を変更せず、P03連携箇所だけを修正 | P03-CC-02, P03-CC-04, P03-CC-07, P03-CC-08 |
| `cd macos && swift test` | 全Swiftテストが終了コード0 | 標準出力のテスト総数と失敗0行 | 失敗時は完了判定せず、最初の失敗を特定して再実行 | P03-CC-01〜P03-CC-08 |
| `rtk git diff --check` | 出力なし、終了コード0 | 終了コードと空出力 | 失敗時はP03計画ファイルの空白・改行だけを修正 | 変更範囲 |
| `rtk git diff --name-only -- plan-firefox-extension/process-03.md plan-firefox-extension/process-03.appendix.md` | 指定2ファイルだけが表示される | 表示されたパスを逐語保存 | 他ファイルを変更せず、差分を報告して停止 | 変更範囲 |
| `rtk rg -n "BrowserBackendHeartbeatIntervalSeconds|BrowserOwnerHeartbeatIntervalSeconds|BrowserLivenessTTLSeconds|generation|dual TTL|browser.block|ブラウザ遮断（dry-run）" plan-firefox-extension/process-03.md plan-firefox-extension/process-03.appendix.md` | Swift側でbackend間隔を参照せず、policy/Host/Engine/backend境界と禁止事項が明記される | 一致行を保存し、`BrowserBackendHeartbeatIntervalSeconds`に「Swift側では参照しない」があることを確認 | 欠落・矛盾があれば文書を未完了とする | P03-CC-03, P03-CC-08 |

## 生成情報

- 担当: `P03`
- 題名: `Swift所有者ハートビート`
- 生成日: `2026-08-10`
- 対象リポジトリ: `/Users/takets/repos/tcc-local-connector`
- 対象プラットフォーム: macOS 14以上
- 生成物: `plan-firefox-extension/process-03.md`
- 正本heartbeat版: `BrowserPolicyVersion=1`
- 変更制約: 計画ファイル2ファイルのみ更新。実装対象はSwift実装2ファイル＋Swiftテスト2ファイルとして明示する。
