# P02 appendix: ブラウザポリシー状態・Engine連携・policy発行

## 背景

Engineの既存サイクルはタスク取得、ルール評価、`Plan.Actions`、既存notifyを扱う。Firefox遮断を既存アクションへ混ぜると、macOS executorとNDJSON/protocol version 1に新しい実行対象が漏れるため、Firefox固有状態は専用policy JSONとして発行する。

P02の責務は、Engineが検証済みの予定集合から正本policyを発行し続けることに限る。owner heartbeatの読込み、policyとheartbeatの15秒dual TTL、Host向けeffective readはP04の責務であり、P02で重複させない。heartbeatは2キーだけで、policy generationとの一致を持たない。

## Why

- 専用JSONを採用する理由は、既存 `Plan.Actions` とNDJSON/protocol version 1を変更せずFirefox状態を連携するため。
- policyにだけgenerationを持たせる理由は、policyの順序制御を保ちながらheartbeatへ不要な世代契約を追加しないため。
- 5秒再発行を採用する理由は、Engineが稼働中である間policyの `updated_at` を継続更新するため。TTL判定自体はHostだけが行う。
- dry-runで非空かつ前回と異なる集合だけ通知する理由は、同一集合の周期再発行による重複通知と空集合変化の通知を避けるため。
- fail-open空policyを採用する理由は、Pause、設定不正、取得失敗、書込み失敗で古い遮断を継続しないため。
- 一時ファイル、sync、rename、ディレクトリsyncを採用する理由は、読み手に途中JSONを見せず現行正本を保全するため。

## 候補比較

| 候補 | 採否 | 理由 |
|---|---|---|
| `Plan.Actions`へ `browser.block` を追加 | 不採用 | 既存executorとNDJSON/protocol version 1へ未知のアクションを渡すため |
| Engineがheartbeatを読みdual TTLを判定 | 不採用 | Host専用のeffective read責務と重複し、所有者境界を崩すため |
| heartbeatにgenerationを追加 | 不採用 | 正本heartbeatは2キーだけで、policyとの不要な結合を作るため |
| policyだけを毎回発行 | 不採用 | backend停止時の更新停止を検出する責務はP04のTTL判定に委譲されるが、P02の5秒発行契約を満たさないため |
| policyを5秒ごとに再発行 | 採用 | 稼働中のpolicy `updated_at` とgenerationを更新し、予定集合の変化を一貫して公開するため |
| dry-runを毎回通知 | 不採用 | 5秒再発行で同一通知が繰り返されるため |
| 非空集合の変化だけ通知 | 採用 | 利用者に意味のある変化だけを固定タイトルで通知するため |

## 手動確認

1. policyを一時ディレクトリへ発行し、7キーだけが指定型で存在し、`updated_at`がRFC3339Nano UTCであることを確認する。
2. heartbeatを2キーだけで用意し、P02のEngineがそれを読み込まず、generation一致やTTL判定を行わないことを確認する。
3. 同じ予定集合を5秒周期で発行し、policyの `updated_at` とgenerationだけが更新されることを確認する。
4. dry-runで非空集合を発行し、固定タイトル `ブラウザ遮断（dry-run）` が1回だけ出ることを確認する。同一集合の再発行、空集合への変化、設定なしでは増えないことを確認する。
5. Pause、取得失敗grace超過、`release_controls`、設定不正、書込み失敗を試し、`enforce=false`かつ両配列空のpolicyになることを確認する。
6. 一時ファイル書込み、ファイルsync、rename、ディレクトリsyncを失敗させ、現行正本が壊れないことを確認する。
7. `TestPlan_NoBrowserBlockInActions`、既存notify、NDJSON/protocol version 1、`go test ./... -race -count=1`を実行し、既存境界へ漏れないことを記録する。

## 失敗時の回復

- JSON検証、書込み、sync、renameの失敗では現行正本を削除・切断せず、空policy発行または読み手のfail-openに委ねる。
- 破損したpolicyを内容推測で修復しない。次の正常なEngine発行で完全な7キーpolicyを再発行する。
- generationを減少させない。値の復旧が必要な場合は、既存正本を壊さず次回発行で単調値を継続する。
- dry-run notifyの失敗はpolicy発行を失敗扱いにせず、`domains=[]`を維持する。
- Hostのheartbeat読込み、dual TTL、effective readに問題がある場合はP04だけを切り分け、P02へ判定ロジックを追加しない。
- 既存rules、protocol、notifyの回帰は完了扱いにせず、P02対象のpolicy発行接続だけを修正する。親PLAN、他process、実装コード、要件書へ変更を広げない。

## 再生成条件

- `BrowserPolicyVersion`、`BrowserPolicyFileName`、`BrowserOwnerHeartbeatFileName`、`BrowserBackendHeartbeatIntervalSeconds`、`BrowserPolicyMaxDomains`、`BrowserFailurePolicy`または固定notifyタイトルを変更した場合。
- policyの7必須キー、heartbeatの2必須キー、型、RFC3339Nano UTC、policyだけのgeneration、未知キー拒否を変更した場合。
- Engineの5秒再発行、`RunCycleNow`、`Reload`、`Pause`、`Resume`、取得失敗grace超過、`release_controls`、設定不正、書込み失敗の発行契約を変更した場合。
- dry-runの「非空かつ前回fingerprintから変化した場合だけ通知」契約、空policyのfail-open条件、原子的書込み順を変更した場合。
- P04のHost専用effective read境界、既存 `Plan.Actions` 非混入、NDJSON/protocol version 1互換、またはP02固有の検証ゲートを変更した場合。
