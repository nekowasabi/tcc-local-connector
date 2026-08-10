# P04: Firefox Native Messaging Host — 付録

## 背景

Firefox拡張は、backendが出力したブラウザpolicyとSwift所有者の生存状態を単独では判断できない。Native Messaging Hostがpolicyとheartbeatを同時に検証し、両方が有効なときだけ遮断対象を渡す。どちらかが失効した場合や接続が切れた場合は空集合に戻す。

状態JSONの正本は次の固定契約である。policy `firefox-browser-policy.json` は `version`、`generation`、`enforce`、`dry_run`、`domains`、`planned_domains`、`updated_at` の7キーだけ、heartbeat `firefox-owner-heartbeat.json` は `version`、`updated_at` の2キーだけを持つ。Hostだけが両方の `updated_at` を検証し、heartbeat generationは存在しない。

## Why（P04-CCの設計理由）

- 既存NDJSONとNative Messagingを分離すると、既存 `protocol.Server` とmacOSクライアントを変更せずにFirefox境界を追加できる。
- Native Messaging stdioなら待受ポート、認証、HTTP常駐プロセスを増やさず、Firefox接続の寿命にHostを合わせられる。
- policyとheartbeatのversion、未来でない時刻、15秒TTLを論理積にすることで、片側停止後の古い遮断を有効化しない。
- generationはpolicyだけで単調性を管理する。heartbeatにもgenerationを持たせると、所有者の生存確認とpolicy世代の一致という別の責務を結合してしまう。
- HostでTTLを検証し、extensionは受信policyを直ちに適用する。二重検証を要求せず、Native Messageに時刻を混ぜないことで責務をHostに限定する。
- `fail_open` と切断時の空集合により、Hostや状態提供者が停止したときに古い遮断が残らない。
- 接続単位のwatermarkを採用すると、接続中はgenerationの単調性を守りつつ、再接続時には同じgenerationの現在policyを初回送信できる。Host再起動・Firefox再接続を跨いでwatermarkを永続化しないためである。
- 接続中の同一generation・内容変更は破棄する。generationをpolicy内容の世代識別子として扱い、同じ世代の後勝ち更新で遮断状態を揺らさないためである。無効化は別の安全側遷移としてgenerationに関係なく空集合を送る。
- Hostの4byte framingと拡張の `Port.postMessage` objectを分離する。Firefox APIがwire framingを隠蔽しているため、拡張コードに自前framingを持たせると二重エンコードになる。

## 候補比較

| 候補 | 採否 | 理由 |
|---|---|---|
| Native Messaging stdio Host | 採用 | Firefox標準の接続境界で、既存NDJSONと分離できる |
| HTTP localhost | 不採用 | 待受、認証、ポート管理、常駐化が必要で禁止事項に反する |
| Unix socket | 不採用 | ソケット寿命と権限管理を新設し、今回の境界を広げる |
| backendからFirefoxへ直接送信 | 不採用 | Firefox接続外の常駐・通信経路を追加する |
| Hostがtcc2を直接取得 | 不採用 | backendのpolicy正本とルール評価を重複させる |

## 手動確認

1. 対象Firefox ESRの `browser.runtime.Port.postMessage({type:"hello", version:1})` でhelloを送り、拡張側ではJSON objectとしてpolicyを受信する。Host標準出力では4byte little-endian長付きのpolicyになり、NDJSONの改行や拡張側の自前4byte処理がないことを確認する。
2. policyとheartbeatを有効期限内に保ち、通常ウィンドウで `domains` が適用されることを確認する。private windowでは `not_allowed` により適用されないことを確認する。
3. `dry_run=true`、`enforce=false`、domains空で、Hostが空domainsを送ることを確認する。
4. policyまたはheartbeatを欠損、破損、未知キー、version不一致、未来時刻、15秒超過、129件に差し替え、空domainsになることを確認する。古いgenerationは破棄されて新規送信されず、無効化（`enforce=false`または`dry_run=true`）はgenerationに関係なく空domainsになることを確認する。
5. heartbeatにgenerationを追加した入力が拒否され、正しい2キーだけのheartbeatが受理されることを確認する。
6. Host入力EOF、Firefox Port切断、Host stdoutのWrite失敗でHostが終了し、extensionの `onDisconnect` 後に内部集合が空になることを確認する。
7. Host起動から終了までstdoutを保存し、Native Message frame以外のログ・警告・デバッグ文字列がなく、診断はstderrだけであることを確認する。
8. 接続中にgenerationを進めたpolicyだけが置換され、同一generation・同一内容はno-op、同一generation・内容変更と古いgenerationは破棄されることを確認する。Host再起動またはFirefox再接続後は同じgenerationの現在policyが初回だけ送信されることを確認する。

## 失敗時の回復

- framing、JSON、schema、version、時刻、ドメイン、上限の失敗は有効化せず、空domainsを送る。初回hello不正とstdout Write失敗はHostを終了する。
- policyまたはheartbeatが読めない場合は、常駐化や別通信へ切り替えず、次の1000ミリ秒pollで再読込する。
- 接続中の古いpolicy generationは破棄し、heartbeatとのgeneration一致を待たない。同一generation・同一内容はno-op、同一generation・内容変更も置換しない。無効状態を観測した場合はgenerationに関係なく空集合へ遷移する。再接続時はwatermarkを初期化するため現在policyを初回送信する。
- extension側の再検証不備をHostのNative Messageへ時刻追加で補わない。TTL判定はHostだけの責務とする。
- テスト失敗時は、最初の失敗gate、シンボル、証跡を確定し、P04対象実装だけを修正する。既存protocol、他process、親PLAN、要件書へ変更を広げない。
- 実機でHostが終了した場合はFirefoxの再接続で再起動する。HTTP、launchd、systemd、手動常駐化は回復策にしない。

## 再生成条件

次のいずれかが変わった場合は、P04のcoreとこの付録を同時に再生成する。

- policyの7キー、heartbeatの2キー、キーの型、未知キー規則、ファイル名
- 両 `updated_at` のRFC3339Nano UTC・未来時刻・15秒TTL規則
- policy generationの単調性、heartbeatにgenerationを置かない規則
- Native Messagingの4byte little-endian framing、payload上限、hello/policy形、timestampを含めない規則、Firefox `Port.postMessage` objectとの層分離
- 接続単位watermark、初回送信、generation置換/no-op/破棄、無効化のgeneration非依存、再接続時の同一generation初回送信
- `dry_run`、`enforce`、空domains、状態異常、切断時のfail-open動作
- `BrowserPolicyVersion`、`BrowserFailurePolicy`、poll周期、ドメイン上限、private window方針
- macOS、Firefox ESR、Manifest V2、既存NDJSON/protocol version 1、`Plan.Actions` の互換性
- HTTP、launchd、systemd、常駐デーモン、tcc2直接取得、既存実装変更の禁止事項
- 対象実装、Symbol Targets、P04-CC、P04-GATE、受入基準
