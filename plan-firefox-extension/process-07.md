# Process 07: 結合検証・文書・受入・ロールバック

## Implementation Brief

P07は、Firefoxドメイン遮断機能を既存のTCC Local Connectorへ直列統合し、結合検証、利用者向け文書、Firefox ESR実機受入、削除・ロールバックを完了させる工程である。実装対象は次の新規検証スクリプトと既存文書の具体的更新であり、既存の実装方式・Protocol v1・Plan.Actionsの契約を壊さない。

この文書だけで実装・検証・完了判定できることを要件とする。P07が完了するまで、対象Processへ戻せるよう各ゲートの証跡を保存する。自動ゲートと手動受入証跡は別々に扱い、片方だけの成功で完了扱いにしない。

## 変更ファイルと行番号

実装時に変更してよいファイルは次の5つだけである。行番号は挿入後の目安であり、既存内容の不必要な移動・整形は行わない。

| ファイル | 変更内容 | 予定行 |
|---|---|---:|
| `scripts/verify-firefox-extension.sh` | W01〜W06を呼び出す結合検証スクリプト。失敗時は非ゼロ終了し、手動受入は実行しない | 新規 1〜220 |
| `README.md` | Firefox ESR導入、設定、検証、署名・配布境界、削除・ロールバックを追記 | 101〜145 |
| `MANUAL.md` | 利用者向け導入順、dry_run、停止時の許可復帰、受入確認、復旧手順を追記 | 135〜205 |
| `docs/protocol-v1.md` | 既存NDJSON/protocol version 1は変更なし、Native Messagingは別4byteフレーミングである旨だけを追記 | 131〜145 |
| `docs/requirements/firefox-domain-blocking.md` | 参照のみ。変更禁止 | 変更なし |

`.claude/`、`PLAN.md`、`PLAN-firefox-extension.md`、`plan/`、この工程以外の計画・要件書は変更しない。

## Symbol Targets

- Go側: 既存の設定解析、`Plan.Actions`生成、`event.plan`、`event.notify`、Protocol v1入出力、`report_actions`。
- Swift側: 既存のタスク取得、状態遷移、一時停止、制御所有者heartbeat、Native Messaging Host起動・標準入出力境界。
- Firefox側: Manifest V2、`browser.block` ポリシー受信、`webRequest.onBeforeRequest`、同期 `redirect`、`main_frame`、`incognito=not_allowed`、`blocked.html`。
- 検証側: `scripts/verify-firefox-extension.sh` のW01〜W06関数または同等の明示された実行単位。

シンボル名が実装と異なる場合は、上記の責務に対応する既存シンボルへ結び付け、別の抽象層を新設しない。

## ローカル定数

実装・テスト・文書で次の値を変更してはならない。

| 定数 | 値 |
|---|---|
| `BrowserBlockActionType` | `browser.block` |
| `BrowserPolicyVersion` | `1` |
| `BrowserStateDirRelative` | `TCCLocalConnector/BrowserPolicy` |
| `BrowserBackendHeartbeatIntervalSeconds` | `5`秒 |
| `BrowserOwnerHeartbeatIntervalSeconds` | `5`秒 |
| `BrowserLivenessTTLSeconds` | `15`秒 |
| `NativeHostPollIntervalMilliseconds` | `1000`ミリ秒 |
| `NativeMessageHeaderBytes` | `4` |
| `NativeMessageMaxPayloadBytes` | `65536` |
| `BrowserPolicyMaxDomains` | `128` |
| `FirefoxNativeHostName` | `jp.takets.tcc_local_connector.firefox` |
| `FirefoxExtensionID` | `firefox-domain-blocker@tcc-local-connector.takets.jp` |
| `BrowserPrivateWindowPolicy` | `not_allowed` |
| `BrowserFailurePolicy` | `fail_open` |

正本schemaは次のキーだけを許可する。policyは`version`、`generation`、`enforce`、`dry_run`、`domains`、`planned_domains`、`updated_at`の7キー、heartbeatは`version`、`updated_at`の2キーとする。両schemaの`version`は1で、policyの`generation`だけが単調増加し、heartbeatにgenerationを持たせない。policyとheartbeatの両`updated_at`のTTLを判定できるのはHostだけである。既存NDJSON/protocol version 1は変更せず、Native Messagingの4byte長ヘッダーは別フレーミングとして扱う。

## 前提条件

- 対象OSはmacOS、対象ブラウザは対象macOS上のFirefox ESRである。
- Firefox拡張はManifest V2で、`webRequestBlocking` による同期redirectを使用する。監視対象は`main_frame`だけである。
- `config.yml`を正本とし、`config.yaml`を自動探索しない。
- 既存NDJSONとProtocol v1、既存`Plan.Actions`、既存アプリ制御・通知・`process.*`・`command.run`を維持する。`browser.block`をNDJSON eventへ追加しない。
- HTTP、launchd、systemd、常駐デーモンは追加しない。Native Messaging HostはFirefox接続中だけ動作し、切断時に終了する。
- policyとSwift所有者heartbeatの双方が15秒以内のときだけ遮断を有効とする。TTL判定はHostだけが行う。
- backend停止かつSwift heartbeat生存、Swift停止かつbackend heartbeat生存のどちらも許可へ戻す。
- `pause`、取得失敗grace超過、`release_controls`、Host切断、設定欠損・破損はすべて空集合とする。
- ドメインは小文字・末尾ドット除去へ正規化し、`x.com`は`x.com`と`sub.x.com`だけに一致し、`x.com.evil.test`には一致しない。
- `blocked.html`は正規化済みhostと「現在のタスクにより、このドメインは遮断中です。」だけを表示する。
- `dry_run`は遮断せず、予定集合が変化した時だけ固定notifyを1回出す。configの正本は`config.yml`だけである。

## 実装手順

1. `scripts/verify-firefox-extension.sh`を新規作成し、`set -euo pipefail`、リポジトリルート解決、W01〜W06の終了コード伝播、証跡ディレクトリ指定を実装する。`--schema-drift`指定時はW06の固定schema drift検査だけを実行できるようにする。実機操作はスクリプトへ混ぜない。
2. Go統合で`browser.block`の解析・和集合・正規化・上限128件・空集合遷移を確認し、既存アクションとNDJSONの順序・互換性を確認する。NDJSONへ新eventを追加しない。
3. Swift統合でbackend heartbeatと所有者heartbeatを各正本の5秒間隔で更新する。両方の15秒TTLを判定してpolicyを有効にするのはHost（P04）だけであり、片方停止時やpause等の解除条件時はHostが空集合をNative Messagingへ送る。
4. Native Messaging HostとFirefox拡張の契約を確認する。4バイト長ヘッダー、最大65536バイト、Protocol v1とは別のNative Messagingフレーミング、Host名、拡張ID、Manifest V2、同期redirect、`main_frame`、private window非作用を固定する。
5. `blocked.html`の表示、dry_run notify、再接続、ブラウザ再起動、設定なしの許可復帰を自動検証可能な範囲でテストする。
6. README、MANUAL、`docs/protocol-v1.md`を更新し、導入順、設定、署名・配布境界、削除・ロールバック、既存機能回帰確認を具体化する。
7. W01〜W06を実行後、Firefox ESR実機受入表を通常ウィンドウで実施する。自動ゲート成功と全手動証跡が揃ったときだけP07を完了とする。

## Behavior Specification

### 有効条件と許可復帰

`browser.block` policyの時刻とSwift所有者heartbeatの時刻がともに現在から15秒以内で、pause・grace超過・release_controls・設定異常・Host切断に該当しない場合だけ遮断集合を有効にする。backendだけ、Swiftだけが生存する状態は許可である。許可は空集合を明示して遷移させ、古い集合を保持しない。

### ドメインと表示

正規化済みhostが登録値と完全一致、または登録値の直後が`.`である場合だけ対象とする。URL、パス、ポート、ワイルドカード、`x.com.evil.test`は対象外。対象の通常ウィンドウmain frameは同期redirectで`blocked.html`へ移し、hostと固定文だけ表示する。private windowでは作用させない。

### dry_runと既存契約

`dry_run=true`ではwebRequestの遮断を行わず、予定集合の変化時だけ固定notifyを1回出す。同一集合の周期更新で重複notifyを出さない。既存`event.plan`、`event.notify`、`process.*`、`command.run`、アプリ制御の動作とProtocol v1のNDJSON境界を維持する。`browser.block`は既存のPlan.ActionsやNDJSON eventへ混ぜず、policy状態からNative Messagingの別4byteフレーミングで拡張へ連携する。

## Correctness Criteria

親PLANのSC-01〜SC-08は参照専用とし、P07の完了判定は次のProcess固有条件で行う。

- P07-CC-01: policyの7キー、heartbeatの2キー、policy generation単調増加、Hostだけの両updated_at TTL判定が固定されている。
- P07-CC-02: `browser.block`の対象ドメインが正規化され、和集合・128件上限・空集合が決定的である。
- P07-CC-03: policyとSwift所有者heartbeatの双方が15秒以内の場合だけ遮断し、それ以外はfail openである。
- P07-CC-04: `x.com`と`sub.x.com`を遮断し、`x.com.evil.test`と無関係ドメインを許可する。
- P07-CC-05: pause、取得失敗grace超過、release_controls、Host切断、設定欠損・破損で空集合になる。
- P07-CC-06: Firefox ESRの通常ウィンドウでblocked.htmlが固定文と正規化hostだけを表示し、private windowには作用しない。
- P07-CC-07: dry_runが遮断せず、予定集合の変化時だけ固定notifyを1回出す。
- P07-CC-08: 既存NDJSON/protocol version 1、Plan.Actions、アプリ制御、notify、`process.*`、`command.run`に回帰がない。`browser.block` eventは追加しない。
- P07-CC-09: README/MANUALが導入順、`config.yml`正本、権限、fail-open、削除、rollbackを記載し、最悪許可復帰時間が`BrowserLivenessTTLSeconds + NativeHostPollIntervalMilliseconds`以下、数値では16.000秒以下である。
- P07-CC-10: Swift所有者heartbeatが正常status時だけ5秒間隔で更新され、status停止、pause、quit、アプリ終了時に停止または削除される。Swiftはdual TTLを判定しない。
- P07-CC-11: Firefox拡張のManifest V2、必要権限、Native Host名、拡張ID、同期main-frame redirect、`incognito=not_allowed`が固定される。

## Left to Implementation

なし。P07で決める範囲は本書に固定済みである。実装中に契約違反や既存挙動との衝突が見つかった場合は推測で拡張せず、該当する前工程へ戻して未完了として報告する。

## Verification Gates

| gate | command | pass_criteria | evidence_spec | failure_policy | criterion_refs |
|---|---|---|---|---|---|
| P07-GATE-01 静的範囲 | `git diff --check && git diff --name-only && shellcheck scripts/verify-firefox-extension.sh`（未導入時は`bash -n scripts/verify-firefox-extension.sh`） | 許可ファイル以外に差分がなく、構文・shellcheck/静的検査が成功 | コマンド全文、終了コード、`git diff --name-only`、静的検査出力 | 失敗なら実装担当へ戻し、P07完了不可 | P07-CC-09 |
| P07-GATE-02 Go回帰 | `go test ./... -race -count=1` | 全Goテスト成功。既存NDJSON、Plan.Actions、`process.*`、`command.run`、notifyを含む | 標準出力保存、終了コード、テスト件数 | 失敗ならGo統合担当へ戻す | P07-CC-02, P07-CC-05, P07-CC-07, P07-CC-08 |
| P07-GATE-03 Swift回帰 | `swift test --package-path macos` | 全Swiftテスト成功。heartbeatの更新・停止・削除、pause、release、quit、再起動を含む。backend停止・Swift停止のdual TTLと許可復帰はP04および手動受入で検証する | 標準出力保存、終了コード、対象テスト名 | 失敗ならSwift統合担当へ戻す | P07-CC-08, P07-CC-10 |
| P07-GATE-04 Firefox拡張 | `web-ext lint --source-dir <extension-dir>` | Manifest V2、権限、Host名、拡張ID、同期redirect、private window設定に違反なし | lint出力、拡張ディレクトリ、終了コード | 失敗ならFirefox実装担当へ戻す | P07-CC-11 |
| P07-GATE-05 結合スクリプト | `bash scripts/verify-firefox-extension.sh` | W01〜W06の必須検証とポリシー境界テストが全成功。手動操作を要求しない | 実行時刻、コミット、各Wの結果、証跡パス | 1件でも失敗ならP07完了不可。対象Processへ戻す | P07-CC-01, P07-CC-02, P07-CC-03, P07-CC-04, P07-CC-05, P07-CC-06, P07-CC-07, P07-CC-08, P07-CC-09, P07-CC-10, P07-CC-11 |
| P07-GATE-06 schema drift | `bash scripts/verify-firefox-extension.sh --schema-drift` | policyが7キー、heartbeatが2キーのみで、generation・TTL所有者・protocol framingの契約に逸脱がない | 検査出力、対象schema、終了コード、比較元 | 失敗ならschema担当Processへ戻し、完了扱いにしない | P07-CC-01, P07-CC-08 |

`shellcheck`は配布前のmacOS検証環境で必須とし、未導入環境での`bash -n`は暫定の最低条件にすぎない。schema drift検査は他の静的検査と分離して実行する。最悪許可復帰時間は、heartbeat停止時刻からFirefoxで最初に許可された時刻までを同一時計で記録し、`BrowserLivenessTTLSeconds + NativeHostPollIntervalMilliseconds`以下、すなわち16.000秒以下であることを確認する。

## 生成情報

- 担当: P07 直列統合（結合検証・文書・受入・ロールバック）
- 作成日: 2026-08-10
- 作成対象: `scripts/verify-firefox-extension.sh`、`README.md`、`MANUAL.md`、`docs/protocol-v1.md`
- 参照保持: `docs/requirements/firefox-domain-blocking.md`（変更しない）
- 成果物: 自動ゲート証跡、Firefox ESR手動受入表、文書差分、ロールバック確認
- 完了条件: P07は直列統合担当であり、W01〜W06または手動受入のいずれかが失敗した場合は完了扱いにせず、失敗した対象Processへ戻す
