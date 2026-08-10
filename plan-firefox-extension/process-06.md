# P06 Host同梱・Native Hostマニフェスト導入・削除

## Implementation Brief

目的は、macOS用アプリ束に `tcc-firefox-native-host` を実行可能な状態で同梱し、Firefox ESRが利用者単位でNative Messaging Hostを起動できるマニフェストを安全に導入・更新・削除できるようにすることです。対象は、束ね処理、導入スクリプト、削除スクリプト、およびこれらを検査する手順です。

## 変更ファイルと行番号

実装対象は次の3ファイルに限定します。

- `scripts/make-app-bundle.sh` 行1-10を維持し、Go製Native Hostのビルド・配置・実行権限設定を追加する。
- `scripts/install-firefox-native-host.sh` 行1-末尾を新規作成する。
- `scripts/uninstall-firefox-native-host.sh` 行1-末尾を新規作成する。

macOSクライアント、既存NDJSONのprotocol version 1、`Plan.Actions`、Firefox拡張の責務は変更しません。HTTP、launchd、systemd、常駐デーモン、ブラウザ外の通信経路は導入しません。

## Symbol Targets

- `scripts/make-app-bundle.sh`: `root`、`app`、ビルド成果物配置、署名処理。
- `scripts/install-firefox-native-host.sh`: `FirefoxNativeHostName`、`FirefoxExtensionID`、アプリパス解決、マニフェスト生成、原子的配置、権限・内容検査。
- `scripts/uninstall-firefox-native-host.sh`: 対象ホスト名、生成済み絶対パスの判定、限定削除、失敗時停止。
- Native Host成果物: `tcc-firefox-native-host`。
- マニフェストJSONキー: `name`、`description`、`path`、`type`、`allowed_extensions`。

## ローカル定数

以下はこの処理で固定し、環境変数や利用者設定で上書きしません。

| 定数 | 値 |
|---|---|
| `BrowserPolicyVersion` | `1` |
| `BrowserOwnerHeartbeatFileName` | `firefox-owner-heartbeat.json` |
| `BrowserStateDirRelative` | `TCCLocalConnector/BrowserPolicy` |
| `BrowserLivenessTTLSeconds` | `15`秒 |
| `NativeHostPollIntervalMilliseconds` | `1000`ミリ秒 |
| `NativeMessageHeaderBytes` | `4` |
| `NativeMessageMaxPayloadBytes` | `65536` |
| `BrowserPolicyMaxDomains` | `128` |
| `FirefoxNativeHostName` | `jp.takets.tcc_local_connector.firefox` |
| `FirefoxExtensionID` | `firefox-domain-blocker@tcc-local-connector.takets.jp` |
| `BrowserPrivateWindowPolicy` | `not_allowed` |
| `BrowserFailurePolicy` | `fail_open` |
| `BlockedPagePath` | `blocked.html` |

Native Hostマニフェストの固定値は `type=stdio`、`allowed_extensions=[FirefoxExtensionID]` とします。`path` はインストール時に解決したアプリ内実行ファイルの絶対パスだけを使用します。

### 正本schema

- `policy` は `version`、`generation`、`enforce`、`dry_run`、`domains`、`planned_domains`、`updated_at` の7キーだけを持つ。未知キー、欠落、型不正、上限超過は無効として扱う。
- `heartbeat` は `version`、`updated_at` の2キーだけを持つ。`heartbeat` に `generation` はない。
- `policy` の `generation` だけが単調増加し、Hostが受信済みの古いpolicyを破棄する。TTLを判定する主体はHostだけであり、policyとheartbeatそれぞれの `updated_at` をHostが確認する。
- 既存NDJSON/protocol version 1を維持し、schema以外のキーや通信方式を追加しない。

## 前提条件

- 対象はmacOS利用者単位のFirefox ESRです。FirefoxのNative Messaging Host検索規則に従い、配置先は `~/Library/Application Support/Mozilla/NativeMessagingHosts/` とします。
- アプリ束は `dist/TCCLocalConnector.app` を既定の成果物とし、導入スクリプトは引数または既定値からアプリパスを解決します。解決結果は正規化済み絶対パスにします。
- Native Hostの実体を生成するGoパッケージまたはコマンドの所在は実装時に既存リポジトリから特定し、推測で新しい通信方式やサービスを作りません。
- 設定側はドメインをASCII小文字化し、末尾ドットを除去してから、ASCII DNS LDH規則を検査します。URL、path、port、wildcard、IP、非ASCII IDN、空ラベルは拒否し、IDNはpunycodeで設定します。
- policyとSwift所有者heartbeatの双方が15秒以内のときだけpolicyを有効とします。TTLはHostだけが判定し、Native Host切断、未知型、破損、期限切れ、policyの古いgenerationは空集合または安全な無視とします。
- Firefox側はManifest V2 background script、`webRequest` と `webRequestBlocking`、対象メインフレームだけの同期redirect、`incognito=not_allowed` を使用します。説明ページは正規化済みhostと固定文「現在のタスクにより、このドメインは遮断中です。」だけを表示します。

## 実装手順

1. `scripts/make-app-bundle.sh` の既存Goバックエンド・Swift・Info.plist・署名の順序を維持し、Native Hostをアプリの `Contents/Resources/tcc-firefox-native-host` にビルドする工程を追加する。
2. Native Hostのビルド完了後、成果物が通常ファイルであることを検査し、`chmod 0700` 相当で所有者だけが実行できる状態にする。実行権限設定は署名より前に行う。
3. アプリ束内のNative Hostを対象に署名を行い、束ね処理の終了時に実行可能性を検査する。既存のアプリ署名を削除・置換しない。
4. `scripts/install-firefox-native-host.sh` でアプリパスを正規化し、`Contents/Resources/tcc-firefox-native-host` が指定アプリ配下にあること、通常ファイルで実行可能であることを検査する。
5. 導入先ディレクトリを利用者単位で作成し、ディレクトリを `0700`、生成対象の一時ファイルを `0600` にする。既存の他ホストマニフェストは読み替えず、対象名だけを扱う。
6. `name`、`description`、絶対 `path`、`type=stdio`、固定 `allowed_extensions` をJSONとして一時ファイルに書き、JSON構文・ID・path・実行可能性を検査する。
7. 同一ファイルシステム上で一時ファイルを対象名へrenameし、マニフェストを原子的に置き換える。移設・更新時も新しい正規化済みpathで再生成する。
8. `scripts/uninstall-firefox-native-host.sh` で対象マニフェストを読み、固定Host名とこの処理が生成可能なpathの一致を検査した場合だけ削除する。別Host、別path、利用者データは削除しない。
9. 不正な引数、アプリ不在、Host不在、権限変更失敗、検査失敗、rename失敗は非ゼロ終了とし、後続の削除や部分的な置換を行わない。途中ファイルは可能な範囲で対象一時ファイルだけを安全に除去する。
10. HTTP daemon、launchd/systemd登録、Firefox拡張の署名・配布は実装しない。拡張の一時導入は手動検証の境界とし、署名・配布はリリース工程の別責務として記録する。

## Behavior Specification

### 束ね処理

- `make-app-bundle.sh` は既存の `tcc-local-connector-backend` とSwiftアプリの生成を維持し、Native Hostを同じアプリの `Resources` に同梱する。
- Native HostはFirefoxから標準入出力で起動され、4バイト長ヘッダー、最大65536バイトのメッセージ本文、1秒のポーリング間隔、既存NDJSON/protocol version 1との境界を守る。Host切断時に常駐を継続しない。
- ブラウザ遮断の有効性は、HostがpolicyとSwift所有者heartbeatの両方のTTLを満たすことを確認し、policyのgenerationが受信済みの最新値である場合だけとする。満たさない場合は空集合として扱う。

### 導入・更新

- 生成先は `${HOME}/Library/Application Support/Mozilla/NativeMessagingHosts/jp.takets.tcc_local_connector.firefox.json` とする。
- JSONの `path` はアプリ束内Hostの絶対path、`allowed_extensions` は固定IDのみ、`type` は `stdio` とする。
- 既存manifestがあっても、対象Host名の正しいmanifestとして検査できた場合は原子的に再生成する。移設後は旧pathを残すmanifestを使わない。
- 導入後にファイルモード、JSON、固定ID、絶対path、実行可能性を再検査し、どれか一つでも不一致なら失敗とする。

### 削除

- 対象ファイルが存在しない場合は成功扱いにできるが、対象以外のファイルを探索・削除しない。
- 対象ファイルのHost名または生成済みpathが一致しない場合は安全に停止し、内容を上書き・削除しない。
- 削除処理はFirefoxプロファイル、設定、拡張、アプリ束、他のNative Hostを変更しない。

### 拡張との境界

- 拡張はManifest V2 background scriptとして、`webRequest` と `webRequestBlocking` で対象メインフレームを同期的に `blocked.html` へredirectする。
- `incognito=not_allowed` とし、プライベートウィンドウを許可しない。
- redirect先で表示するのは正規化済みhostと固定文だけとする。Native Host切断、未知型、破損、期限切れ、policyの古いgenerationでは遮断集合を空にし、`BrowserFailurePolicy=fail_open` を守る。

## Correctness Criteria

- アプリ束に `Contents/Resources/tcc-firefox-native-host` があり、所有者実行可能である。
- マニフェストが利用者単位の正確なディレクトリ・ファイル名にあり、`type=stdio`、固定Host名、固定拡張ID、絶対pathを持つ。
- 一時ファイルからのrename以外で対象manifestを更新せず、導入失敗時に旧manifestを壊さない。
- 再導入でpathが変わった場合にmanifestが新pathへ更新され、旧pathを参照しない。
- 削除は対象Host名と生成済みpathが一致する場合だけ行い、他Host・利用者データを変更しない。
- すべての失敗経路が非ゼロ終了し、HTTP、launchd、systemd、常駐デーモンを作らない。
- policyとheartbeatの二重有効期限をHostだけが判定し、policy generationだけを単調増加として扱うこと、最大ドメイン数128、ASCII DNS LDH検証、非ASCII IDNはpunycode設定、空集合への安全側フォールバックが後続実装の契約として保たれる。
- Firefox通常ウィンドウの対象メインフレームだけが同期redirectされ、プライベートウィンドウでは有効化されない。

### P06固有条件

- `P06-CC-01`: Hostは同梱された実行ファイルを利用者単位manifestの絶対 `path` で参照し、`allowed_extensions` は固定IDだけとする。
- `P06-CC-02`: `policy` は7キー、`heartbeat` は2キーだけのschemaとし、heartbeatへgenerationを追加しない。
- `P06-CC-03`: TTLはHostだけが判定し、policyの古いgenerationは破棄してfail-openする。
- `P06-CC-04`: 導入・再導入・限定的uninstallは対象manifestだけを原子的かつ権限検査付きで扱う。
- `P06-CC-05`: HTTP、launchd、systemd、常駐デーモン、macOS、既存NDJSON/protocol version 1、`Plan.Actions`を変更しない。

## Left to Implementation

- Native Hostの既存Goパッケージ名・ビルド対象。実装時に `go list` または既存のビルド定義から確定する。
- アプリパスを引数で受けるか既定の `dist/TCCLocalConnector.app` を使うかの細部。ただし両方を許す場合も、解決後の検査規則と安全側失敗は変えない。
- JSON生成に使用する既存の処理系。macOS標準コマンドまたは既存リポジトリの処理系を優先し、追加依存は作らない。
- 署名・配布済みFirefox拡張のリリース運用。P06では拡張を一時導入して手動確認する境界までを扱う。

## Verification Gates

### Gate 1: 静的検査（P06-GATE-01）

- `command`: `bash -n scripts/make-app-bundle.sh scripts/install-firefox-native-host.sh scripts/uninstall-firefox-native-host.sh && rg -n 'tcc-firefox-native-host|jp.takets.tcc_local_connector.firefox|firefox-domain-blocker@tcc-local-connector.takets.jp|type=stdio|allowed_extensions|NativeMessagingHosts|launchd|systemd|http' scripts/make-app-bundle.sh scripts/install-firefox-native-host.sh scripts/uninstall-firefox-native-host.sh`
- `pass_criteria`: 構文検査が成功し、固定Host名・固定拡張ID・Native Host配置・マニフェスト型・導入先が確認でき、HTTP/launchd/systemd登録コードがない。
- `evidence_spec`: 終了コード0と、各固定値を含む該当行の出力を保存する。
- `failure_policy`: 非ゼロなら実装完了と判定せず、該当スクリプトだけを修正して再実行する。
- `criterion_refs`: P06-CC-01, P06-CC-02, P06-CC-05。

### Gate 2: 束ねと実行権限（P06-GATE-02）

- `command`: `bash scripts/make-app-bundle.sh && test -x dist/TCCLocalConnector.app/Contents/Resources/tcc-firefox-native-host && test "$(stat -f '%Lp' dist/TCCLocalConnector.app/Contents/Resources/tcc-firefox-native-host)" = 700`
- `pass_criteria`: 束ね処理が成功し、Resources内Hostが実行可能かつ所有者専用モードである。
- `evidence_spec`: コマンド終了コード0、成果物絶対path、モード、署名結果を記録する。
- `failure_policy`: ビルド失敗または権限不一致なら導入検証へ進まず、原因を特定して再実行する。
- `criterion_refs`: P06-CC-01, P06-CC-05。

### Gate 3: 初回導入・再導入・移設（P06-GATE-03）

- `command`: `bash scripts/install-firefox-native-host.sh --app dist/TCCLocalConnector.app; bash scripts/install-firefox-native-host.sh --app dist/TCCLocalConnector.app; bash scripts/install-firefox-native-host.sh --app /temporary/TCCLocalConnector.app`
- `pass_criteria`: 初回導入、同一path再導入、path変更を伴う再導入がそれぞれ原子的に成功し、最終JSONのpathが実在する実行可能Hostを指す。
- `evidence_spec`: 導入前後のJSON、モード、path、rename対象、終了コードを記録する。`/temporary` が用意できない環境では移設ケースをUnverifiedと明記する。
- `failure_policy`: 旧manifest破壊、path不一致、権限不一致があれば失敗扱いにして、バックアップした対象manifestだけを用いてロールバックする。
- `criterion_refs`: P06-CC-01, P06-CC-04。

### Gate 4: 削除と保護（P06-GATE-04）

- `command`: `bash scripts/uninstall-firefox-native-host.sh --app dist/TCCLocalConnector.app; test ! -e "$HOME/Library/Application Support/Mozilla/NativeMessagingHosts/jp.takets.tcc_local_connector.firefox.json"`
- `pass_criteria`: 対象manifestだけが削除され、他のNative Host、Firefoxプロファイル、利用者設定、アプリ束が残る。
- `evidence_spec`: 削除前後の対象ディレクトリ一覧、保護対象の存在、終了コードを記録する。
- `failure_policy`: Host名または生成済みpathが不一致なら削除しない。誤対象の兆候があれば処理を停止し、対象外を復元対象にしない。
- `criterion_refs`: P06-CC-04, P06-CC-05。

### Gate 5: Firefox ESR手動確認（P06-GATE-05）

- `command`: 対象macOSのFirefox ESRへ拡張を一時導入し、通常ウィンドウで設定済みhostを開く。次にHost切断、期限切れheartbeat、policyの古いgeneration、プライベートウィンドウを確認する。
- `pass_criteria`: 通常ウィンドウの対象メインフレームだけが `blocked.html` と固定文へredirectされ、切断・破損・期限切れ・policyの古いgenerationでは空集合かつfail-open、プライベートウィンドウでは有効化されない。
- `evidence_spec`: Firefox ESR版、拡張ID、対象host、各状態の表示結果、Native Host標準入出力の終了、日時を記録する。
- `failure_policy`: 失敗状態で遮断が残る、対象外リクエストを遮断する、固定文以外を表示する場合は失敗とし、リリース境界へ進めない。
- `criterion_refs`: P06-CC-02, P06-CC-03, P06-CC-05。

## 生成情報

- 担当: P06
- 題名: Host同梱・Native Hostマニフェスト導入・削除
- 対象: `scripts/make-app-bundle.sh`、`scripts/install-firefox-native-host.sh`、`scripts/uninstall-firefox-native-host.sh`、導入検査資材
- 作成日: 2026-08-10
- 生成条件: 指定されたBrowser契約、macOS Firefox ESR、既存NDJSON/protocol version 1、macOSクライアント、`Plan.Actions`を維持する。
- 完了判定: Verification Gates 1-4が成功し、Gate 5は実施結果を記録する。未実施または環境不足はUnverifiedとして残す。
