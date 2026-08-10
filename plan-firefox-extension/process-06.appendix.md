# P06 補足

## 背景

Firefox拡張がNative Messaging Hostを起動するには、利用者単位の所定ディレクトリにHostマニフェストが必要です。アプリ束内の実行ファイルとmanifestの絶対pathがずれると、アプリ更新や移設後に拡張が接続できません。導入と削除を明示的なスクリプトに分け、更新時の再生成と対象限定の削除を確認可能にします。

対象ブラウザはmacOSのFirefox ESRです。Native Hostはブラウザ接続中だけ標準入出力で動作し、HTTP、launchd、systemd、常駐デーモンを利用しません。拡張の署名・配布は、ローカルでの一時導入による動作確認とは別のリリース境界です。

正本schemaは、policyの `version`、`generation`、`enforce`、`dry_run`、`domains`、`planned_domains`、`updated_at` の7キーと、heartbeatの `version`、`updated_at` の2キーだけです。heartbeatにgenerationはなく、policy generationだけが単調増加します。TTLを判定するのはHostだけです。

## Why

- アプリ束のResourcesへHostを置くことで、バックエンドやSwiftアプリと同じ配布単位で絶対pathを管理できます。
- manifestを一時ファイルからrenameすることで、Firefoxが書きかけのJSONを読む時間を作らず、更新失敗時に既存の有効なmanifestを残せます。
- 利用者単位の配置に限定することで、システム全体のFirefox設定や他利用者のNative Hostに影響を与えません。
- uninstallでHost名と生成済みpathを照合することで、同名ファイルの差し替えや利用者が管理する別Hostの誤削除を避けられます。
- fail-openとHostだけによる期限確認を導入境界でも確認することで、Host切断や古いpolicyが閲覧遮断を残す事故を防げます。Hostはpolicyの古いgenerationを破棄します。

## 候補比較

| 候補 | 採否 | 理由 |
|---|---|---|
| アプリResources内にHostを同梱し、利用者manifestから絶対pathで参照 | 採用 | アプリ更新・移設時のpathを再生成でき、配布単位が明確。 |
| `/usr/local/bin` などへHostを共有配置 | 不採用 | 利用者境界が曖昧で、権限・アンインストール範囲が広がる。 |
| launchdでHostを常駐させる | 不採用 | Native Messagingの接続寿命と異なり、禁止された常駐経路になる。 |
| HTTP localhost daemonで拡張と通信 | 不採用 | HTTP daemon禁止に反し、ポート・認証・停止処理が増える。 |
| 既存manifestを直接上書き | 不採用 | 書き込み途中の破損をFirefoxが読む可能性がある。 |
| 固定一時ファイルを直接rename | 条件付き | 同時実行時の衝突があるため、所有者専用の一意な一時ファイルと同一ディレクトリで扱う必要がある。 |

## 手動確認

1. 既存のFirefox Native Messaging Hostsディレクトリと他Hostの一覧を保存する。
2. `make-app-bundle.sh` を実行し、Resources内のHostが実行可能であること、実行権限が所有者専用であることを確認する。
3. installを初回実行し、manifestのJSON、固定Host名、固定拡張ID、`type=stdio`、絶対path、ファイルモードを確認する。
4. 同じアプリpathでinstallを再実行し、manifestが破損せず、pathが変わらないことを確認する。
5. アプリ束を別の一時ディレクトリへ複製してinstallを再実行し、manifestのpathだけが新しい実在Hostへ変わることを確認する。旧pathを参照していないことも確認する。
6. Firefox ESRへ拡張を一時導入し、通常ウィンドウで対象hostのメインフレームを開く。`blocked.html`、正規化済みhost、固定文「現在のタスクにより、このドメインは遮断中です。」だけが表示されることを確認する。
7. サブリソース、対象外host、プライベートウィンドウを確認し、対象メインフレーム以外を遮断しないこと、`incognito=not_allowed`を守ることを確認する。
8. Native Host切断、未知型、破損メッセージ、期限切れpolicy、期限切れheartbeat、policyの古いgenerationを一つずつ作り、遮断集合が空になりfail-openになることを確認する。heartbeatにはgenerationを追加しない。
9. uninstallを実行し、対象manifestだけが消え、他Host、Firefoxプロファイル、利用者設定、アプリ束が残ることを確認する。
10. 不一致のmanifestを対象位置へ置いてuninstallを実行し、削除せず非ゼロ終了することを確認する。
11. 拡張の署名済み配布物ではなく、一時導入物で確認したことを記録する。署名・配布の確認はリリース工程で別途行う。

## 失敗時の回復

- 束ね失敗時は、生成途中のアプリを利用せず、既存の配布済みアプリを残す。実装が既存束を削除する場合は、失敗前のバックアップまたは再生成可能な成果物だけを回復対象にする。
- manifest生成失敗時は、対象ディレクトリ内の一時ファイルだけを除去し、既存manifestを変更しない。
- rename後の検査失敗時は、直前の対象manifestを保存していた場合だけそれを戻し、新しいpathを参照する不完全な状態を残さない。
- 移設確認に失敗した場合は、新しいmanifestを検証できるまでFirefoxを再接続しない。旧アプリのmanifestを勝手に削除しない。
- uninstallの対象照合に失敗した場合は停止する。対象以外のHostやFirefox利用者データを復元・削除する操作へ拡張しない。
- Native Host切断、policy破損、heartbeat期限切れ、policyの古いgenerationでは遮断を解除し、再接続・再同期後にだけ新しいpolicyを受け入れる。HostだけがTTLを判定する。

## 再生成条件

- アプリ束の絶対pathが変わったとき。
- `tcc-firefox-native-host` が更新、再ビルド、署名更新されたとき。
- Firefox Native Messaging Hostのmanifestが存在しない、JSONとして壊れている、固定Host名・固定拡張ID・`type=stdio`・絶対pathのいずれかが不一致のとき。
- manifestのファイルモードが `0600` でない、または親ディレクトリが `0700` でないとき。
- Firefox ESRのNative Messaging Host解決規則、拡張ID、Browser契約の固定値を変更するとき。
- アプリ更新後に旧pathを参照していることが検査で分かったとき。
- 生成処理の途中で終了し、対象manifestと一時ファイルの状態を確定できないとき。再生成前に対象manifestの照合と一時ファイルの整理を行う。
