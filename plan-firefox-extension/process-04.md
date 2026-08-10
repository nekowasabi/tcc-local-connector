# P04: Firefox Native Messaging Host

## Implementation Brief（P04-CC）

新規 `cmd/tcc-firefox-native-host/main.go` と同ディレクトリの `*_test.go` を実装する。HostはFirefoxのNative Messaging接続中だけ標準入出力を使い、policyファイルとowner heartbeatを検証して拡張へ遮断対象ドメインを送る。既存 `protocol.Server`、既存NDJSON、既存macOSクライアント、`Plan.Actions` は変更しない。HTTP、launchd、systemd、常駐デーモン、tcc2の直接取得も実装しない。

Native MessagingはNDJSONとは別のプロトコルである。Host→extensionは4byte little-endianのpayload長prefixとUTF-8 JSON、extension→Hostの初回入力は同じframingのhelloだけを使う。Hostから送るJSONに時刻は含めず、extensionにTTL再検証を要求しない。Firefox拡張側は `browser.runtime.Port.postMessage` でJSONオブジェクトを送受信し、4byteのencode/decodeを自前実装しない。4byte framingはHostの標準入出力境界だけの責務である。

## 変更ファイルと行番号

行番号は新規実装の配置目安であり、実装時はシンボルを優先する。今回の変更対象はこのcoreと付録の2ファイルだけである。

| 対象実装 | 予定行 | 内容 |
|---|---:|---|
| `cmd/tcc-firefox-native-host/main.go` | L1-L360 | Native Messaging framing、厳密な状態JSON検証、TTL・generation判定、policy送信、終了処理 |
| `cmd/tcc-firefox-native-host/main_test.go` | L1-L520 | framing、JSON schema、時刻、generation、状態遷移、stdout非汚染、切断・書込み失敗 |

変更禁止: `internal/protocol/*`、既存コマンド、`macos/*`、親PLAN、他process、appendix以外の計画、要件書、未追跡 `.claude/`、`PLAN.md`、`plan/`、`docs/requirements/`。

## Symbol Targets

実装対象の主なシンボルは次のとおり。テスト専用の非公開Reader/Writer、時計、状態ディレクトリ差し替え型は必要最小限で追加できる。

- `main`
- `run`
- `readNativeMessage`
- `writeNativeMessage`
- `readHello`
- `validateHello`
- `readPolicyFile`
- `readOwnerHeartbeatFile`
- `validatePolicy`
- `validateOwnerHeartbeat`
- `resolveBrowserStateDir`
- `readEffectivePolicy`
- `policyMessage`
- `emptyPolicyMessage`
- `pollPolicy`
- `writeJSONError`

## ローカル定数

| 定数名 | 値 | 意味 |
|---|---:|---|
| `BrowserPolicyVersion` | `1` | policy、heartbeat、Native Messageの版 |
| `BrowserFailurePolicy` | `fail_open` | 異常時は空集合 |
| `NativeMessageHeaderBytes` | `4` | payload長prefixのバイト数 |
| `NativeMessageMaxPayloadBytes` | `65536` | payloadの送受信上限 |
| `NativeHostPollIntervalMilliseconds` | `1000` | policy監視周期 |
| `BrowserPolicyMaxDomains` | `128` | 各ドメイン配列の上限 |
| `BrowserLivenessTTLSeconds` | `15` | policyとheartbeatのTTL |
| `BrowserPrivateWindowPolicy` | `not_allowed` | private windowには適用しない |
| `NativeReconnectInitialSeconds` | `1` | Firefox再接続の初期待機秒数 |
| `NativeReconnectMaxSeconds` | `30` | Firefox再接続の最大待機秒数 |
| `BrowserPolicyFileName` | `firefox-browser-policy.json` | policyファイル名 |
| `BrowserOwnerHeartbeatFileName` | `firefox-owner-heartbeat.json` | heartbeatファイル名 |
| `BrowserStateDirRelative` | `TCCLocalConnector/BrowserPolicy` | Application Supportからの相対パス |

## 前提条件

- 状態ディレクトリはmacOS標準Application Support配下の `TCCLocalConnector/BrowserPolicy` とし、Hostが設定ファイルを直接読んだりtcc2を呼び出したりしない。
- policyファイルのJSONオブジェクトは、次の7キーだけを持つ。`version:int`、`generation:int`、`enforce:bool`、`dry_run:bool`、`domains:string[]`、`planned_domains:string[]`、`updated_at:RFC3339Nano UTC`。7キーはすべて必須で、未知キー・欠損・型不一致・破損JSONを無効とする。
- heartbeatファイルのJSONオブジェクトは、次の2キーだけを持つ。`version:int`、`updated_at:RFC3339Nano UTC`。2キーはすべて必須で、未知キー・欠損・型不一致・破損JSONを無効とする。heartbeatに `generation` を入れない。
- Hostだけがpolicyとheartbeatの `updated_at` を検証する。両方のversionが1、両方の時刻が未来でなく、両方が現在から15秒以内であるときだけpolicyの `domains` を有効化する。
- generationはpolicy側だけの単調値としてHostが採用する。heartbeatとのgeneration一致は要求しない。接続ごとにwatermarkを初期化し、新規接続時は現在のeffective policyを有効・無効にかかわらず必ず1回送信する。接続中はgenerationが大きい有効policyだけを置換し、同一generationで内容不変はno-op、同一generationで内容が変わったpolicyと古いgenerationは破棄する。無効化policyはgenerationに関係なく空domainsへ遷移させる。
- `dry_run=true`、`enforce=false`、`domains` 空、状態欠損・破損・未知必須キー・上限超過・期限切れ・未来時刻は、Hostがextensionへ空domainsを送る。古いgenerationは現在の有効policyを維持し、新たに送信しない。
- `domains` と `planned_domains` はASCII小文字、末尾ドット除去、重複なし、辞書順、128件以下のDNS名とする。URL、path、port、wildcard、IP、非ASCII、空ラベルは拒否する。
- Host切断、入力EOF、stdout Write失敗は終了する。extensionは `onDisconnect` で内部集合を空にする。
- `BrowserPrivateWindowPolicy=not_allowed`、macOS、Firefox ESR、Manifest V2を対象とする。

## 実装手順

1. `resolveBrowserStateDir` でユーザーのApplication Supportを解決し、固定絶対パスを使わず状態ファイル名を連結する。
2. `readNativeMessage` でheaderを部分Read可能な形で読み、4byte little-endian長を復元する。0または65536超を、payload割当て前に拒否する。payloadは部分Readと途中EOFを扱う。
3. JSONを1メッセージ単位でUTF-8・未知キー拒否・型検証する。`writeNativeMessage` はJSONをmarshalし、4byte little-endian長を付け、部分Writeを完了まで処理する。ログはstderrだけに出す。
4. 初回hello `type=hello, version=1` だけを受理し、未知キー・未知type・version不一致・JSON不正では終了する。
5. policyとheartbeatを厳密な7キー・2キーとして読み、version、時刻、配列、ドメイン、上限を検証する。
6. 両状態のTTLが有効な場合だけpolicyのgenerationを単調性検証する。heartbeatのgenerationは参照しない。無効状態、`dry_run`、`enforce`、空domainsではgenerationに関係なく空policyを作る。
7. hello直後に現在のeffective policyを必ず1件送り、接続単位のwatermarkを設定する。以後1000ミリ秒ごとに再読込し、generationが大きいpolicyだけ置換する。同一generation・同一内容はno-op、同一generation・内容変更と古いgenerationは破棄し、無効化だけはgenerationに関係なく空policyへ遷移させる。再接続時は新しいwatermarkから開始するため、同じgenerationの現在policyも初回送信する。
8. stdin EOF、Host切断、Write失敗で再接続・常駐化せず終了する。stdoutにはNative Message frame以外を出力しない。
9. 部分Read/Write、EOF、上限境界、不正JSON、未知キー、未来時刻、期限切れ、古いgeneration、両heartbeat組合せ、切断、stdout汚染をテストする。

## Behavior Specification

### 状態JSON

policy `firefox-browser-policy.json` は次の7キーだけを持つ。

```json
{
  "version": 1,
  "generation": 42,
  "enforce": true,
  "dry_run": false,
  "domains": ["example.com"],
  "planned_domains": ["example.com"],
  "updated_at": "2026-08-10T12:00:00.000000000Z"
}
```

heartbeat `firefox-owner-heartbeat.json` は次の2キーだけを持つ。

```json
{
  "version": 1,
  "updated_at": "2026-08-10T12:00:00.000000000Z"
}
```

`updated_at` はRFC3339Nano形式のUTCで、未来でなく、現在から15秒以内でなければならない。policyの `generation` は正の整数で、Hostの採用値以上だけを候補にする。heartbeatにはgenerationを要求しない。

### Native Messaging

- wire形式は `[4byte little-endian payload length][UTF-8 JSON payload]`。NDJSONではない。
- extension→Hostのhelloは `{"type":"hello","version":1}` のみ。拡張コードでは `browser.runtime.Port.postMessage({type:"hello", version:1})` を使い、4byte framingを扱わない。
- Host→extensionは `type=policy`、`version`、`generation`、`enforce`、`dry_run`、`domains`、任意の `planned_domains` を持つ。timestamp系キーは持たない。
- Hostは上記JSONを4byte little-endian framingで標準出力へ書くが、拡張側の `Port.postMessage` が受け取るのはJSONオブジェクトであり、両者を同じ層の処理として実装しない。
- payload長0、65536超、JSON不正、UTF-8不正、未知キー、型不一致は無効とする。
- Host切断、入力EOF、Write失敗ではHostが終了し、extensionの `onDisconnect` が内部集合を空にする。Host切断時も拡張の内部集合は空化する。

### 有効化判定

| 条件 | Hostの送信 |
|---|---|
| policy/heartbeatのversionが1、両 `updated_at` が未来でなく15秒以内、`enforce=true`、`dry_run=false`、domains非空 | generationがwatermarkより大きければdomainsを送る。同じgenerationかつ同一内容ならno-op |
| 上記以外、または状態欠損・破損・未知キー・上限超過 | `domains:[]`、`enforce:false` のpolicyを送る |
| policy generationが採用済みより古い | 古いdomainsを適用せず、送信しない |
| 接続直後 | 現在のeffective policyをgenerationにかかわらず1回送る |
| 接続中のgenerationが大きい | 新しいpolicyに置換して送る |
| 接続中の同一generation・同一内容 | no-op |
| 接続中の同一generation・内容変更 | 破棄して置換しない |
| 再接続後の同一generation | 新しいwatermarkのため初回だけ送る |

extensionは受信した各policyを常に適用する。`enforce=false` または `dry_run=true` または `domains` 空なら、直ちに内部集合を空にする。TTLの再検証はしない。

## Correctness Criteria

- P04-CC-01: policyは7キー、heartbeatは2キーだけを厳密に受理し、別名キー・未知キー・欠損・型不一致・破損を拒否する。
- P04-CC-02: 両ファイルのversion=1、未来でない、15秒以内というTTLの論理積だけで有効化し、heartbeat generation一致を要求しない。
- P04-CC-03: policy generationの単調性だけをHostが採用し、古いgenerationを適用しない。watermarkは接続単位で、新規接続では現在policyを同じgenerationでも初回送信できる。
- P04-CC-04: `dry_run=true`、`enforce=false`、domains空、状態異常時にHostが空domainsを送る。
- P04-CC-05: 4byte little-endian framing、部分Read/Write、EOF、65536境界、65537超過、UTF-8・JSON不正、poll、state変化をテストする。
- P04-CC-06: Host→extension messageにtimestampがなく、extensionが受信policyを常に適用し、無効条件で内部集合を直ちに空にする。
- P04-CC-07: Host切断、入力EOF、Write失敗で終了し、stdoutにframe以外を出力しない。
- P04-CC-08: Native Messagingと既存NDJSON/protocol version 1を混同せず、Firefox `Port.postMessage` objectとHost wire framingを混同せず、既存 `protocol.Server`、macOS、`Plan.Actions` を変更しない。
- P04-CC-09: `go test ./cmd/tcc-firefox-native-host -race -count=1` と `go vet ./cmd/tcc-firefox-native-host` が成功する。

## Left to Implementation

- JSON内部構造体名、時計、Reader/Writer、状態ディレクトリ差し替えの非公開型。
- 初回無効状態の内部送信タイミング。ただし外部結果は空domainsで固定する。
- 同一generationでの内容比較の内部実装。ただし古いgenerationは適用しない。
- stderrの診断文言と終了コード。ただしstdout非汚染と切断時終了は固定する。

## Verification Gates（P04-GATE）

| gate_id | command | pass_criteria | evidence_spec | failure_policy | criterion_refs |
|---|---|---|---|---|---|
| P04-GATE-01 | `go test ./cmd/tcc-firefox-native-host -run 'Test(NativeMessage|Hello|Policy|Heartbeat|Generation|Poll|Disconnect|Stdout)' -count=1` | framing、部分Read/Write、EOF、schema、時刻、TTL、generation、poll、切断、stdoutの対象テストが終了コード0 | コマンド全文、終了コード、各PASS行 | 最初の失敗シンボルだけを修正して再実行し、他範囲へ広げない | P04-CC-01, P04-CC-02, P04-CC-03, P04-CC-04, P04-CC-05, P04-CC-06, P04-CC-07 |
| P04-GATE-02 | `go test ./cmd/tcc-firefox-native-host -race -count=1` | 全テストが終了コード0、競合なし | PASS行と終了コード | Host対象だけを修正して再実行する | P04-CC-01, P04-CC-02, P04-CC-03, P04-CC-04, P04-CC-05, P04-CC-06, P04-CC-07, P04-CC-08, P04-CC-09 |
| P04-GATE-03 | `go vet ./cmd/tcc-firefox-native-host` | 警告なし、終了コード0 | コマンド出力と終了コード | Host対象だけを修正する | P04-CC-09 |
| P04-GATE-04 | `gofmt -d cmd/tcc-firefox-native-host/main.go cmd/tcc-firefox-native-host/main_test.go` | 出力が空、終了コード0 | 空出力と終了コード | 対象2ファイルだけ整形する | P04-CC-09 |
| P04-GATE-05 | `rg -n 'http|launchd|systemd|tcc2|Listen\(|Serve\(|fmt\.Print|log\.' cmd/tcc-firefox-native-host` | 禁止通信・常駐・直接取得・stdout汚染がない。stderr許可箇所は個別確認 | 一致行と許可理由 | 禁止一致を除去して再実行する | P04-CC-07, P04-CC-08 |
| P04-GATE-06 | `git diff --name-only -- plan-firefox-extension/process-04.md plan-firefox-extension/process-04.appendix.md && git status --short` | 本作業で変更したファイルがP04の2ファイルだけ。未追跡既存資材は変更前のものとして区別 | コマンド出力 | 指定外の変更を止め、P04の2ファイル以外を変更しない | P04-CC-08 |

## 生成情報

- 担当: P04
- 題名: Firefox Native Messaging Host
- 対象実装: 新規 `cmd/tcc-firefox-native-host/main.go` と同ディレクトリの `*_test.go`
- 固定契約: policy 7キー、heartbeat 2キー、policy generation単調性、両updated_atの15秒TTL、4byte little-endian Native Messaging
- 互換性: 既存NDJSON/protocol version 1、macOS、`Plan.Actions` を維持する
- 安全方針: `BrowserFailurePolicy=fail_open`。異常、古いgeneration、切断、出力失敗では遮断を継続しない
- 再生成条件: 上記の固定契約、定数、対象シンボル、受入基準、禁止事項のいずれかが変わった場合
