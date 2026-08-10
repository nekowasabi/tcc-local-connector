# Process 07 Appendix: 結合検証・文書・受入・ロールバック

## 背景

Firefoxドメイン遮断は既存のタスク計画とmacOSアプリ制御の上に載るため、単体テストだけでは、heartbeatの所有権、Native Messagingの切断、空集合への復帰、Firefox ESRの通常ウィンドウ限定、既存Protocol v1の回帰を保証できない。本付録は、P07の実装者と受入者が同じ操作・同じ判定を再現するための補助仕様である。正本schemaはpolicyの7キー（`version`、`generation`、`enforce`、`dry_run`、`domains`、`planned_domains`、`updated_at`）とheartbeatの2キー（`version`、`updated_at`）だけである。policy generationだけを単調増加とし、両updated_atのTTLはHostだけが判定する。

## Why

- 常駐サービスを増やさず、Firefox接続時だけNative Messaging Hostを起動するため、Host切断を正常な許可状態として検証する必要がある。
- backendとSwift所有者heartbeatを別々に生存扱いすると、片方停止時に古い遮断状態が残るため、双方の15秒以内を有効条件とする。
- policyとheartbeatのschemaを別々の暗黙仕様にすると余分なキーやgenerationの逆転を見逃すため、schema drift検査を独立ゲートにする。
- `x.com`の部分文字列判定は`x.com.evil.test`を誤遮断するため、host境界を`.`単位で確認する。
- 自動検証は契約・回帰を効率よく検出するが、Firefox ESRの説明ページ、private window、再起動は実機の目視証跡が必要である。
- 設定や拡張の署名・配布境界を文書化しないと、検証済み成果物と利用者が導入した成果物が一致しないため、導入順と削除順を固定する。

## 候補比較

| 論点 | 採用 | 採用理由 | 不採用候補 |
|---|---|---|---|
| ブラウザ連携 | Firefox Native Messaging | HTTP公開面を増やさず、Firefox接続のライフサイクルに追従できる | HTTPサーバー、launchd/systemd常駐 |
| 遮断方式 | Manifest V2 `webRequestBlocking`同期redirect | 対象をmain frameに限定し、固定説明ページへ確実に遷移できる | 非同期処理、URL誘導 |
| private window | `incognito=not_allowed` | 追加権限と専用処理を避け、対象外を明示できる | private windowへの作用保証 |
| 失敗時 | `fail_open`で空集合 | Host・設定・heartbeat障害で通常閲覧を妨げない | 古いpolicyの保持 |
| 設定名 | `config.yml`のみ | 正本を一つに保ち、環境差による誤設定を防ぐ | `config.yaml`自動探索 |
| 検証順 | W01〜W06の直列実行後に手動受入 | 失敗箇所を特定し、実機確認を未検証のまま通さない | 自動・手動の同時完了扱い |
| schema | policy 7キー、heartbeat 2キー、policy generationのみ単調 | 正本との差分を独立検出できる | heartbeatへのgeneration追加、NDJSON event追加 |

## 手動確認

### 導入順

1. 既存アプリ・Goバックエンド・Swiftパッケージの回帰テストを実行する。
2. `config.yml`をバックアップし、`dry_run: true`と少数ドメインで設定する。`config.yaml`は探索せず、設定ファイルの権限は所有者のみ読み書き可能にする。
3. GoとSwiftをビルドし、Native Messaging HostとFirefox拡張を同じ成果物から配置する。Host名は`jp.takets.tcc_local_connector.firefox`、拡張IDは`firefox-domain-blocker@tcc-local-connector.takets.jp`に一致させる。
4. Firefox ESRへ拡張を導入し、通常ウィンドウでdry_run通知と無遮断を確認する。
5. `dry_run: false`へ切り替え、対象タスクを有効化して受入表を実施する。
6. 受入後に拡張、Native Messaging Host、設定の順で導入物を記録する。署名済み配布物を使う場合、署名検証結果も保存する。

### Firefox ESR実機受入表

各行に「前提設定、操作、期待結果、実測時刻、スクリーンショットまたはログ、判定者」を記録する。通常ウィンドウの行はすべて合格必須である。

| ID | 操作 | 期待結果 |
|---|---|---|
| A01 | 有効タスクで`x.com`を開く | 遮断され、`blocked.html`に正規化済み`x.com`と固定文だけ表示 |
| A02 | `sub.x.com`を開く | 遮断される |
| A03 | `x.com.evil.test`を開く | 許可される |
| A04 | 無関係ドメインを開く | 許可される |
| A05 | 遮断後の説明ページを確認 | 固定文「現在のタスクにより、このドメインは遮断中です。」以外の説明・タスク名を表示しない |
| A06 | `dry_run: true`で対象タスクを有効化 | サイトは遮断されず、予定集合の変化時だけ固定notifyが1回出る |
| A07 | `config.yml`なしでHostを再接続 | 空集合となり、任意ドメインが許可される |
| A08 | backendだけ停止しSwift heartbeatを生存させる | 空集合となり許可される |
| A09 | Swiftだけ停止しbackend heartbeatを生存させる | 空集合となり許可される |
| A10 | FirefoxからHostを切断する | 保持中の遮断が消え、許可される |
| A11 | Hostを再接続する | 条件が揃った場合だけ最新policyが再適用される |
| A12 | private windowで対象ドメインを開く | 本機能による遮断は作用しない（`not_allowed`） |
| A13 | Firefoxを再起動して対象ドメインを開く | 初期許可から始まり、Host接続後に条件が揃えば遮断される |
| A14 | pause、取得失敗grace超過、release_controlsを各々発生させる | いずれも空集合となり許可される |
| A15 | heartbeat停止から許可復帰まで計測 | 最悪許可復帰時間が`BrowserLivenessTTLSeconds + NativeHostPollIntervalMilliseconds`以下、数値では16.000秒以下である |

backend停止とSwift停止は別々に実施する。A15は停止時刻、次のHostポーリング時刻、空集合到着時刻、Firefoxで許可された時刻を記録し、`15秒 + 1000ミリ秒`以下、すなわち16.000秒以下であることを計算する。

### 回帰確認

既存のアプリ起動・停止、notify、`process.*`、`command.run`、`event.plan`、`event.state_changed`、`event.notify`、Protocol v1のNDJSON改行区切りを、browser.blockを含まない設定でも確認する。browser.blockを含む設定でも、他アクションの順序・結果・`report_actions`が変化しないことをログで比較する。`browser.block`をNDJSON eventへ追加しない。`docs/protocol-v1.md`には既存NDJSON/protocol version 1は変更なし、Native Messagingは別4byteフレーミングとの注記だけを残す。

## 失敗時の回復

自動ゲートW01〜W06の失敗、手動受入の不合格、または証跡欠落は完了扱いにしない。失敗した責務を特定し、該当する対象Processへ戻して修正後にW01から再実行する。P07は直列統合担当であり、P07内で仕様を勝手に緩和しない。

利用者向けの即時ロールバックは次の順序で行う。

1. `config.yml`を直前バックアップへ戻すか、`browser.block`の`domains`を空にし、`dry_run: true`へ変更する。
2. メニューのpauseではなく、必要ならアプリを完全終了してNative Messaging Hostとの接続を切る。
3. Firefox拡張を無効化し、Native Messaging Hostの登録ファイルと同梱実行ファイルを導入前バックアップへ戻す。
4. Firefoxを再起動し、対象ドメインが許可されることを確認する。
5. 変更前のアプリ制御・notify・`process.*`・`command.run`・NDJSONが復旧していることを自動テストとログで確認する。

削除時は、設定、Firefox拡張、Native Messaging Host登録、同梱ファイルの順に対象を確認してから除去する。ユーザーの既存設定や既存アプリ本体を無断削除しない。署名済み配布物は署名者・チームの配布境界で取り消し、作業者が署名鍵を新設・移管しない。

## 再生成条件

次のいずれかが発生した場合、P07の証跡を破棄せず、対象Processの成果物と固定契約を再確認して本付録の手動受入を最初から再実施する。

- Firefox ESR、macOS、Swift、Go、Native Messaging仕様、Manifest V2の実行環境が変わった。
- `BrowserLivenessTTLSeconds`、`NativeHostPollIntervalMilliseconds`、Host名、拡張ID、Protocol version、失敗ポリシーが変わった。
- `blocked.html`固定文、ドメイン正規化、private window方針、dry_run通知の仕様が変わった。
- `README.md`、`MANUAL.md`、`docs/protocol-v1.md`の導入・削除・署名境界が変わった。
- 自動ゲートのコマンド、テスト対象、証跡形式を変更した。
- Firefox拡張、Host、Swift、Goのいずれかを別成果物から配布した。

再生成後も、A01〜A15の全行、自動ゲートW01〜W06、既存機能回帰、最悪許可復帰時間の計測を省略しない。
