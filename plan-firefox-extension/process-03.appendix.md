# P03 付録 — Swift所有者ハートビート

## 背景

Firefox拡張へ渡すpolicyは、backendだけが動いている状態やSwiftアプリだけが動いている状態を有効とみなしてはならない。backendはpolicyを5秒ごとに再発行し、Swiftアプリは正常なstatus応答を受けたときだけowner heartbeatを5秒間隔で更新する。Hostだけが両ファイルの`updated_at`を15秒TTLで判定する。

policyの正本schemaは`version:int`、`generation:int`、`enforce:bool`、`dry_run:bool`、`domains:string[]`、`planned_domains:string[]`、`updated_at:RFC3339Nano UTC`の7必須キーである。heartbeatの正本schemaは`version:int`、`updated_at:RFC3339Nano UTC`の2必須キーだけであり、heartbeatにgenerationはない。policy generationだけが単調に増加する。

## Why

- 正常なstatus応答の後だけheartbeatを更新するのは、要求送信だけではbackendが処理可能であることを証明できず、古い所有者証明を延命するためである。
- heartbeatを2項目に限定するのは、生存時刻と契約版以外を有効判定へ持ち込まず、generationやpolicy項目の不整合を増やさないためである。
- backendのpolicy再発行間隔とSwiftのheartbeat更新間隔を別定数にするのは、両者の責務を混同しないためである。Swift側の正本は`BrowserOwnerHeartbeatIntervalSeconds=5`であり、`BrowserBackendHeartbeatIntervalSeconds`は参照しない。
- Hostだけがdual TTLを判定するのは、policyとownerの両方を知る有効判定を一箇所へ集約し、EngineやSwiftへ責務を逆流させないためである。
- generation比較をしないのは、generationがpolicyだけの単調値であり、heartbeatへ複製すると再起動・遅延・書込順序の不整合を招くためである。
- 原子的置換を使うのは、Hostの読取りとSwiftの書込みが競合しても、部分JSONを有効証明として扱わせないためである。
- 失敗時に停止または削除するのは、異常状態のSwiftが新しい時刻を書いて有効期間を延長しないためである。

## 候補比較

| 候補 | 採否 | 理由 |
|---|---|---|
| 2項目heartbeat JSONを同一ディレクトリ内で原子的に置換 | 採用 | 既存のファイル共有範囲で完結し、読者に部分内容を見せない。 |
| heartbeatをSQLiteで管理 | 不採用 | 単一の鮮度証明に依存・ロック・復旧を追加し、範囲が過大になる。 |
| HTTP localhost endpoint | 不採用 | 待受け寿命と権限境界を追加し、既存のstdio境界を変える。 |
| `launchd`による所有者監視 | 不採用 | アプリのquit・終了と所有者寿命が分離する。 |
| PIDだけをheartbeatへ保存 | 不採用 | PID再利用とプロセス実体確認が必要で、生存時刻の代替にならない。 |
| heartbeatへpolicy generationを複製 | 不採用 | policyだけの単調値という正本を壊し、P03に不要な比較責務を追加する。 |

## 手動確認

1. アプリを起動し、正常なstatus応答後にApplication Support配下の`TCCLocalConnector/BrowserPolicy/firefox-owner-heartbeat.json`が生成されることを確認する。
2. JSONが`version`と`updated_at`だけで、versionが1、時刻がUTC RFC3339Nanoであることを確認する。generationが存在しないことも確認する。
3. 親ディレクトリ0700、heartbeatファイル0600を確認する。
4. backendを停止またはstatus応答を止め、heartbeatが更新されず、Swift側が古い時刻を延命しないことを確認する。15秒TTLによる有効判定はHostの確認として行う。
5. pause、取得失敗grace超過、`release_controls`、config不正、書込失敗を順に発生させ、backend/Host側の正本が`enforce=false`の空policyになることと、heartbeatが停止または削除されることを確認する。
6. `quit`とアプリ終了でタイマーが止まり、heartbeatが削除または無効化され、終了後に再生成されないことを確認する。
7. アプリを再起動し、正常なstatus応答を受けるまで前回のheartbeatを新鮮な所有者証明として延長しないことを確認する。
8. 共有ディレクトリを書込み不可にした状態でstatusを返し、アプリがクラッシュせず、heartbeatが更新されないことを確認する。確認後はテスト用の権限変更だけを元に戻す。
9. `dry_run=true`のpolicyで`domains=[]`、`planned_domains`が予定集合となること、予定集合の非空変化だけで固定notify「ブラウザ遮断（dry-run）」が1回送られ、空集合への変化・不変・設定なしでは通知されないことを確認する。これはP03のSwift実装ではなくbackendの検証対象である。
10. policyとheartbeatのgeneration比較が行われず、Hostのdual TTLだけが有効判定を行うことを確認する。これはP04の検証対象である。

## 失敗時の回復

- heartbeatの破損、未知キー、version不一致、時刻変換不能: 有効証明として使わず、次の正常statusで原子的に再生成する。P03はpolicyを読み込んで補正しない。
- 一時ファイル書込み、権限、原子的置換の失敗: 既存heartbeatを更新せず、可能なら無効化または削除する。アプリの主処理は継続し、Hostは鮮度を満たさない状態として扱う。
- Timerの二重起動: 既存更新を停止してから1本だけ再登録する。`BrowserBackendHeartbeatIntervalSeconds`をSwift側のタイマーへ流用しない。
- quitとの競合: heartbeat停止・削除を直列化してからbackend shutdownへ進み、停止後の保留処理が再生成できないようにする。
- backend停止、status停止、pause、アプリ終了: heartbeatの更新を止めるか削除し、古いファイルを更新して鮮度を延命しない。
- policy発行、dry-run notify、config不正、`release_controls`、Engineのheartbeat非参照、Hostのdual TTL、effective readの失敗: P03のSwift実装へ回復処理を追加せず、それぞれの担当processで扱う。
- テスト失敗: P03の4つの実装対象ファイルだけを検証し、親PLAN、他process、実装コード、要件書、未追跡ファイルを変更しない。

## 再生成条件

次のいずれかが変わったときだけ、この付録を再生成する。

- policyまたはheartbeatの共有パス、ファイル名、JSON形、version、TTL、permissionが変わった。
- `BrowserOwnerHeartbeatIntervalSeconds`、backendのpolicy再発行間隔、status応答、pause、取得失敗grace、`release_controls`、backend停止・Host切断の意味が変わった。
- Hostのdual TTL、Engineのheartbeat非参照、P04のeffective read、policy generationの単調性が変わった。
- Native messageの4byte frame、payload上限、既存NDJSON/protocol version 1、既存`Plan.Actions`の契約が変わった。
- `MenuController`の`synchronize`、`consumeBackendMessages`、`quit`、`BrowserPolicyHeartbeat`、または4つのSwift対象ファイルの配置が変わった。

単なる文言修正では再生成せず、生成日と差分理由だけを更新する。再生成時も親PLAN、他process、実装コード、要件書、`.claude/`、`PLAN.md`、`plan/`、`docs/requirements/`を入力として変更しない。
