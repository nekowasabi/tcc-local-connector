# P05 付録：Firefox拡張・遮断・説明ページ

## 背景

macOSクライアントが管理する遮断policyを、Firefox ESRの通常windowへ限定して適用する。
Hostがpolicyとheartbeatの正本を検証し、Portを通じて拡張へJSON objectを渡す。
Hostとwire fixtureの4byte little-endian framingを拡張へ漏らさず、拡張は受信した結果を
常に処理する。遮断できない状態では内部集合を空にして`fail_open`とする。

正本policyは`version:int`、`generation:int`、`enforce:bool`、`dry_run:bool`、
`domains:string[]`、`planned_domains:string[]`、`updated_at:RFC3339Nano UTC`の7必須キー、
heartbeatは`version:int`、`updated_at:RFC3339Nano UTC`の2必須キーだけである。
heartbeatにgenerationはない。両updated_atの15秒TTLはHostだけが判定する。

## Why

- PortのJSON objectを使うのは、Firefox拡張のNative Messaging APIの責務に合わせ、拡張がwire framingを二重実装しないため。
- 4byte framingをHost P04とwire fixtureに限定するのは、wire検証を保ちつつ、拡張の`browser.runtime.Port`契約を単純にするため。
- 起動・再接続で`lastGeneration=-1`へ戻すのは、新しいPortのセッションが最新policyを必ず受け入れられるようにするため。
- 正常policyだけを`generation > lastGeneration`で置換するのは、同じ世代の再送をno-opにし、古い世代への後退を防ぐため。
- 無効化指示をgeneration非依存で空化するのは、古いwatermarkのために停止指示を取りこぼさないため。
- TTLとheartbeatの判定をHostだけに置くのは、正本と時刻検証を一箇所に集約し、拡張が時計やファイルを再解釈しないため。
- Manifest V2と同期`webRequestBlocking`を使うのは、Firefox ESRで要求前にmain_frame redirectを決定するため。
- `main_frame`だけを対象にするのは、画像、XHR、iframeなどページ内部の通信を変更しないため。
- hostと固定理由だけを説明ページへ渡すのは、完全URL、タスク名、heartbeat時刻の漏えいを防ぐため。
- `NativeReconnectInitialSeconds`/`NativeReconnectMaxSeconds`を使うのは、再接続間隔の契約名をP05内で統一するため。

## 候補比較

| 論点 | 採用 | 見送った候補 | 判断理由 |
|---|---|---|---|
| 拡張通信API | `browser.runtime.Port`のJSON object | 拡張内の4byte encode/decode | APIの既存契約を使い、wire処理の重複を避けるため |
| Host/wire検証 | 4byte little-endian framing | NDJSON Native Messaging | 指定wire形式を守り、既存NDJSON/protocol version 1のmacOS経路と混同しないため |
| TTL判定 | Hostだけ | 拡張とHostの二重判定 | 状態ファイルと時計の責務を広げないため |
| 世代 | policyのgenerationだけ | heartbeatとのgeneration一致 | heartbeatにgenerationを追加せず、単調値の正本を一つにするため |
| 要求遮断 | 同期`main_frame` redirect | content script後処理 | 後処理では対象ページの読み込みを防げないため |
| 通信経路 | Native Messaging | HTTP localhost | HTTPサーバーと常駐サービスが禁止されているため |
| 互換基準 | Firefox ESR / Manifest V2 | Manifest V3のみ | 指定された同期blocking APIの契約に合わないため |
| 異常時 | fail_open | fail_closed | 切断・破損・期限切れで古い遮断を残さないため |
| ドメイン | ASCII punycodeの完全host | URLやwildcard | URL部品・wildcard・IPによる曖昧な一致を避けるため |

## 手動確認

Firefox ESRのmacOS実機で、次を確認する。各ケースで集合とwatermarkの状態を記録する。

1. 起動直後と再接続直後に`lastGeneration=-1`であることを確認し、有効policyを受け入れる。
2. `generation`が増えるpolicyは置換、同じ世代はno-op、古い世代は無視されることを確認する。
3. `dry_run=true`、`enforce=false`、`domains=[]`、Hostからの空policyを順に送り、世代に関係なく直ちに空集合になることを確認する。
4. Native Hostを切断し、即時に空集合とwatermark resetになること、`NativeReconnectInitialSeconds`から`NativeReconnectMaxSeconds`で再接続することを確認する。
5. Port受信がJSON objectであり、helloが`{type:"hello",version:1}`、policyが指定schemaであることを確認する。拡張が4byte encode/decodeを呼ばないことをテストで確認する。
6. 有効policyで`example.com`へ移動し、完全一致で`blocked.html`へ同期redirectする。表示が正規化hostと固定理由だけであることを確認する。
7. `sub.example.com`を遮断し、`x.com.evil.test`は`x.com` ruleで遮断されないことを確認する。
8. 画像、iframe、XHR、private windowではredirectされないことを確認する。private window方針は`BrowserPrivateWindowPolicy=not_allowed`である。
9. scheme、path、port、wildcard、IP、非ASCII、空ラベルを含むruleが受理されないことを確認する。
10. 説明ページのqueryに完全URL、タスク名、heartbeat時刻、HTML相当文字列を与え、hostのテキストと固定理由だけが表示されることを確認する。
11. Hostが破損・期限切れ・設定なしを空policyとして送り、拡張がTTLを再判定せず空集合にすることを確認する。
12. Firefox ESRの版、OS、Host接続状態、期待値、実測結果を記録する。

## 失敗時の回復

- Port objectのテスト失敗時は、拡張へ4byte処理を追加せず、hello/policyのschema、version/type、型、最大値を確認する。
- 世代テスト失敗時は、起動・再接続の`lastGeneration=-1`、正常policyの`>`、同一世代no-op、古い世代無視を確認する。
- 無効化後に遮断が残る場合は、無効化分岐がgeneration比較より先に空化することを確認する。無効化policyは世代に関係なく空化する。
- 切断後に遮断が残る場合は、`onDisconnect`で空集合とwatermark resetを同期実行しているか確認する。Host本体は変更しない。
- redirectループ時は、`blocked.html`自身と拡張URLの除外、hostだけのquery生成を確認する。元URLやタスク名を追加して回避しない。
- Host/wire fixtureのframing失敗時は、4byte little-endian長さ、payload上限65536バイト、JSON境界を確認する。拡張をframing実装へ戻さない。
- `web-ext lint`未導入時はUnverifiedとして記録し、Node.jsテストと構文検査の結果で代替したと断定しない。
- 指定外ファイルの変更を検出した場合は作業を止め、変更一覧を報告する。P05の2ファイル以外を独断で復元しない。

## 再生成条件

次のいずれかが変わった場合に限り、このP05のcoreとappendixを再生成する。

- policyの7必須キー、heartbeatの2必須キー、heartbeatにgenerationなし、version、generationの意味、`updated_at`形式、TTL、HostだけがTTLを判定する責務。
- Portのhello/policy object schema、Host/wire fixtureの4byte framing、payload上限、拡張側のversion・generation検証規則。
- `BrowserPolicyVersion`、`FirefoxNativeHostName`、固定拡張ID、heartbeatファイル名、状態ディレクトリ、P05の固定定数。
- `NativeReconnectInitialSeconds`、`NativeReconnectMaxSeconds`、Firefox ESR、Manifest V2、`webRequestBlocking`、private window方針、`main_frame`限定、fail_open。
- ドメイン検証、punycode入力契約、host境界一致、`BlockedPagePath`、説明ページへ渡せる情報。
- 実機確認で、現在の仕様では説明できない再現性のある不具合が見つかった場合。

再生成時も更新対象は`process-05.md`と本ファイルの2ファイルに限定し、親PLAN、他process、実装コード、要件書、未追跡`.claude/`、`PLAN.md`、`plan/`、`docs/requirements/`は変更しない。
