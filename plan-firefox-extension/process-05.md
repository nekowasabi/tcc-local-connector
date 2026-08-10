# P05 Firefox拡張・遮断・説明ページ

## Implementation Brief

`firefox-extension/` のFirefox ESR拡張を実装する。拡張は
`browser.runtime.connectNative` が返す `browser.runtime.Port` を使い、
`port.postMessage({type:"hello",version:1})` と `port.onMessage` のJSON objectを
扱う。4byte little-endian framingはHost P04とwire fixtureの責務であり、拡張に
`encodeNativeMessage`/`decodeNativeMessage`を実装させない。

Hostが有効と判定したpolicyのドメインだけを通常windowの`main_frame`要求で
同期的に`blocked.html`へredirectする。無効化指示、切断、設定なし、異常入力では
内部集合を即時に空にし、`BrowserFailurePolicy=fail_open`とする。

## 変更ファイルと行番号

行番号は実装時の目標行であり、実装後に実行行を確定する。

| ファイル | 予定行 | 変更内容 |
|---|---:|---|
| `firefox-extension/manifest.json` | 1-32 | Manifest V2、固定ID、Native Messaging・webRequest権限、永続background、`incognito=not_allowed` |
| `firefox-extension/background.js` | 1-300 | Port接続、JSON object検証、世代watermark、遮断判定、切断時空化、再接続 |
| `firefox-extension/blocked.html` | 1-18 | 正規化hostと固定理由だけを表示する説明ページ |
| `firefox-extension/blocked.js` | 1-90 | queryからhostだけを読み取り、安全に画面へ設定 |
| `firefox-extension/tests/*` | 1-260 | Port object契約、policy、世代、無効化、host境界、redirect、再接続のテスト |

## Symbol Targets

- `manifest.json`: `manifest_version`, `browser_specific_settings.gecko.id`, `permissions`, `background`, `incognito`。
- `background.js`: `BrowserPolicyVersion`, `BrowserFailurePolicy`, `BrowserLivenessTTLSeconds`, `NativeHostPollIntervalMilliseconds`, `NativeMessageHeaderBytes`, `NativeMessageMaxPayloadBytes`, `NativeReconnectInitialSeconds`, `NativeReconnectMaxSeconds`, `BrowserPolicyMaxDomains`, `BrowserPrivateWindowPolicy`, `normalizeDomain`, `validatePolicyMessage`, `isNewerGeneration`, `isBlockedHost`, `blockedUrl`, `connectNative`, `handleNativeMessage`, `handleDisconnect`, `scheduleReconnect`, `onBeforeRequest`。
- `blocked.js`: `readBlockedHost`, `renderBlockedHost`。
- `tests/*`: Portに渡すhello object、policy object、watermark、無効化、切断、境界一致のケース。

## ローカル定数

以下を拡張内の単一契約定数として定義し、値を変更しない。

| 定数 | 値 |
|---|---|
| `BrowserPolicyVersion` | `1` |
| `BrowserFailurePolicy` | `fail_open` |
| `BrowserLivenessTTLSeconds` | `15` |
| `NativeHostPollIntervalMilliseconds` | `1000` |
| `NativeMessageHeaderBytes` | `4` |
| `NativeMessageMaxPayloadBytes` | `65536` |
| `NativeReconnectInitialSeconds` | `1` |
| `NativeReconnectMaxSeconds` | `30` |
| `BrowserPolicyMaxDomains` | `128` |
| `BrowserPrivateWindowPolicy` | `not_allowed` |
| `BrowserOwnerHeartbeatFileName` | `firefox-owner-heartbeat.json` |
| `BrowserStateDirRelative` | `TCCLocalConnector/BrowserPolicy` |
| `FirefoxNativeHostName` | `jp.takets.tcc_local_connector.firefox` |
| `FirefoxExtensionID` | `firefox-domain-blocker@tcc-local-connector.takets.jp` |
| `BlockedPagePath` | `blocked.html` |

Host P04とwire fixtureでは4byte little-endian長さprefixと65536バイトのpayload上限を
検証する。Firefox拡張はPortが渡すJSON objectを検証し、自前の4byte framing処理を持たない。
既存NDJSON/protocol version 1、`Plan.Actions`、macOSを変更しない。

## 前提条件

- 対象はmacOS上のFirefox ESR、Manifest V2、`browser`名前空間、同期型`webRequestBlocking`である。
- Hostが状態ファイルを読み、TTLを判定した結果をPortへ送る。拡張は状態ファイル、TTL、heartbeat、`updated_at`を読まず再検証しない。
- policy正本`firefox-browser-policy.json`は、`version:int`、`generation:int`、`enforce:bool`、`dry_run:bool`、`domains:string[]`、`planned_domains:string[]`、`updated_at:RFC3339Nano UTC`の7必須キーを持つ。heartbeat正本`firefox-owner-heartbeat.json`は、`version:int`、`updated_at:RFC3339Nano UTC`の2必須キーだけを持ち、`generation`を持たない。
- 両`updated_at`の15秒TTLを判定するのはHostだけである。Hostは有効なpolicyだけでなく、無効状態、`dry_run=true`、`enforce=false`、空`domains`、状態欠損・破損・未知キー・上限超過・期限切れを、空`domains`のpolicy objectとして送る。
- policyのgenerationだけが単調値である。heartbeatとのgeneration一致は要求しない。
- IDNは事前にASCII punycode化された設定値だけを受け付け、拡張は非ASCII値を変換しない。
- HTTP、launchd、systemd、常駐デーモンを追加しない。未追跡`.claude/`、`PLAN.md`、`plan/`、`docs/requirements/`を変更しない。

## 実装手順

1. `manifest.json`に固定ID、Manifest V2、`nativeMessaging`、`webRequest`、`webRequestBlocking`、`<all_urls>`、永続background、`incognito: "not_allowed"`を定義する。
2. `background.js`に契約定数、空の内部遮断集合、`lastGeneration`、Port、再接続backoffを定義する。起動時は`lastGeneration=-1`にする。
3. `connectNative`で`browser.runtime.connectNative(FirefoxNativeHostName)`を呼び、返されたPortへ`port.postMessage({type:"hello",version:1})`を一度送る。`port.onMessage`ではJSON objectを受ける。拡張側に4byte encode/decodeを追加しない。
4. policy objectは`type:"policy"`、`version:1`、`generation:int`、`enforce:bool`、`dry_run:bool`、`domains:string[]`、任意の`planned_domains:string[]`だけを契約対象とし、型、version/type、payload相当の最大65536バイト、ドメイン数を検証する。
5. 起動時と再接続時は`lastGeneration=-1`へ戻す。正常policyは全ドメイン検証後、`generation > lastGeneration`のときだけ集合とwatermarkを置換する。同じgenerationはno-op、古いgenerationは無視する。
6. `enforce=false`、`dry_run=true`、`domains=[]`、またはHostから届いた空policyはgenerationに関係なく直ちに内部集合を空にする。異常policy objectもfail_openで空にする。
7. ドメインをASCII小文字化後に末尾ドット除去し、ASCII DNS LDH、ラベル長、全体長を検証する。URL、path、port、wildcard、IP、非ASCII、空ラベルは拒否する。
8. `webRequest.onBeforeRequest`を同期listenerとして登録し、`type === "main_frame"`、`details.incognito !== true`、host境界一致の全条件を満たすときだけ`blocked.html?host=<percent-encoded-normalized-host>`へredirectする。`x.com.evil.test`は`x.com`に一致させない。
9. 拡張自身、`blocked.html`、説明ページへの再入場はredirectしない。完全URL、query、fragment、タスク名はredirect先へ渡さない。
10. `onDisconnect`で同期的に内部集合を空化し、`lastGeneration=-1`へ戻してから、`NativeReconnectInitialSeconds`から`NativeReconnectMaxSeconds`までbackoffで再接続する。重複timerは1つだけにする。
11. `blocked.html`と`blocked.js`はqueryからhostだけを安全に読み、正規化hostと固定理由「現在のタスクにより、このドメインは遮断中です。」をテキストノードとして表示する。
12. Node.jsテスト、構文検査、`web-ext lint`、Firefox ESR実機で通常window、dry_run、切断、設定なし、private windowを確認する。

## Behavior Specification

### P05-CC — PortとNative Message object契約

- Host P04→Firefox拡張のwireはHostとwire fixtureだけが4byte little-endian framingを扱う。拡張は`browser.runtime.Port`の`postMessage`/`onMessage`でJSON objectを扱い、`encodeNativeMessage`/`decodeNativeMessage`を持たない。
- helloは`{type:"hello",version:1}`である。policyは`{type:"policy",version:1,generation,enforce,dry_run,domains}`に、任意で`planned_domains`を加える。
- schema、`version`、`type`、整数generation、boolean、配列、最大ドメイン数、各ドメインの境界を検証する。無効objectは受理せず内部集合を空にする。

### P05-GATE — Policy適用とwatermark

- 起動時・再接続時は空集合と`lastGeneration=-1`から開始する。
- 正常policyは`generation > lastGeneration`だけ置換し、同じgenerationはno-op、古いgenerationは無視する。
- `enforce=false`、`dry_run=true`、`domains=[]`、Hostからの空policyはgenerationに関係なく直ちに空集合にする。切断時も空集合化しwatermarkをresetする。
- 拡張はTTL、heartbeat、`updated_at`を検証しない。両`updated_at`の15秒TTLはHostだけが判定し、generationの単調性はpolicyだけで判定する。
- `main_frame`の同期redirect、通常window限定、完全一致または`.`境界付きサブドメイン一致、`blocked.html`固定理由を満たす。

## Correctness Criteria

- P05-CC-01: Portでhello/policyのJSON object schema、version/type、型、planned_domains、最大payload・最大ドメイン数を検証し、拡張独自の4byte encode/decodeを要求しない。
- P05-CC-02: 正本policyの7必須キー、heartbeatの2必須キー、heartbeatにgenerationなし、Hostだけの15秒TTL判定を仕様境界に反映する。
- P05-CC-03: 起動・再接続の`lastGeneration=-1`、正常policyの`>`置換、同一generation no-op、古いgeneration無視、無効化のgeneration非依存空化を満たす。
- P05-CC-04: 切断時に空集合とwatermark resetを同期実行し、1〜30秒で再接続する。
- P05-CC-05: 完全一致と`.`境界付きサブドメイン一致だけがredirectされ、`x.com.evil.test`のsuffix偽陽性がない。
- P05-CC-06: `main_frame`以外、private window、空集合、`dry_run`、`enforce=false`、拡張自身のURLはredirectされない。
- P05-CC-07: `blocked.html`には正規化hostと固定理由だけを表示し、完全URL、タスク名、heartbeat時刻を渡さない。
- P05-CC-08: 固定定数、Firefox ESR/Manifest V2、`Plan.Actions`、macOS、既存NDJSON経路を変更しない。

## Left to Implementation

- `firefox-extension/`配下の実装、テスト、拡張パッケージ配置。ただしPort object契約とP05-CC/P05-GATEの規則は変更しない。
- 既存Hostとwire fixtureが4byte framingを検証する具体的な接続方法。
- `web-ext`の利用可能な実行方法とFirefox ESR実機の確認環境。

## Verification Gates

| gate | command | pass_criteria | evidence_spec | failure_policy | criterion_refs |
|---|---|---|---|---|---|
| P05-GATE-01 静的ファイル | `test -f firefox-extension/manifest.json && test -f firefox-extension/background.js && test -f firefox-extension/blocked.html && test -f firefox-extension/blocked.js` | 4ファイルが存在し終了コード0 | 終了コードと対象ファイル一覧 | 失敗時は完了にしない | P05-CC-01, P05-CC-08 |
| P05-GATE-02 構文 | `node --check firefox-extension/background.js && node --check firefox-extension/blocked.js` | 両方終了コード0 | 終了コード | 失敗箇所を修正して再実行 | P05-CC-01, P05-CC-03 |
| P05-GATE-03 契約テスト | `node --test firefox-extension/tests` | Port object、schema、世代、無効化、切断、host境界の全テスト成功 | テストランナー成功行と失敗0 | 失敗仕様を修正して再実行 | P05-CC-01, P05-CC-02, P05-CC-03, P05-CC-04, P05-CC-05, P05-CC-06 |
| P05-GATE-04 wire fixture | `node --test firefox-extension/tests --test-name-pattern='wire|hello|policy'` | hello/policy objectとHost側4byte fixtureの全ケース成功 | 成功行とケース名 | 拡張へframing処理を追加せず、fixtureまたは契約を修正 | P05-CC-01, P05-CC-02 |
| P05-GATE-05 無効化世代 | `node --test firefox-extension/tests --test-name-pattern='generation|disable|disconnect|reconnect'` | watermark、無効化、切断、再接続の全ケース成功 | 成功行とケース名 | 集合とwatermarkの規則を満たすまで完了にしない | P05-CC-03, P05-CC-04 |
| P05-GATE-06 lint | `web-ext lint --source-dir firefox-extension` | lintエラー0 | lint終了行とエラー数 | 未導入ならUnverifiedとして残し導入後に再実行 | P05-CC-01, P05-CC-06, P05-CC-08 |
| P05-GATE-07 manifest | `node -e "const m=require('./firefox-extension/manifest.json'); if(m.manifest_version!==2||m.incognito!=='not_allowed'||m.browser_specific_settings.gecko.id!=='firefox-domain-blocker@tcc-local-connector.takets.jp') process.exit(1)"` | 固定値の検査終了コード0 | 終了コード | 不一致が1つでもあれば失敗 | P05-CC-02, P05-CC-08 |
| P05-GATE-08 ESR実機 | `web-ext run --source-dir firefox-extension --firefox=\"<Firefox ESR>\"` | 通常windowの遮断、dry_run・切断・設定なし・private windowの空集合、非対象の非遮断 | ESR版、各ケース、結果の手動記録 | 未実施はUnverifiedで完了扱いにしない | P05-CC-05, P05-CC-06, P05-CC-07 |
| P05-GATE-09 変更範囲 | `git diff --name-only -- plan-firefox-extension/process-05.md plan-firefox-extension/process-05.appendix.md && git status --short --untracked-files=all` | 変更対象がP05の2ファイルだけで、指定外ファイルに変更なし | 名前一覧と`git diff --stat` | 指定外変更があれば停止して報告 | P05-CC-08 |

## 生成情報

- 担当: P05
- 題名: Firefox拡張・遮断・説明ページ
- 生成日: 2026-08-10
- 対応基準: macOS上のFirefox ESR、Manifest V2、Port JSON object、Host/wire fixtureの4byte framing
- 契約版: `BrowserPolicyVersion=1`
- Process固有ID: `P05-CC`, `P05-GATE`
- 生成範囲: 本文書単独でP05の実装・検証・完了判定ができるよう、通信、正本schema、TTL責務、watermark、挙動、基準、検証門を収録
