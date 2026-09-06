# 利用マニュアル

本書は macOS 用常駐ツール **TCC Local Connector** の運用手順です。
目標は、TaskChute Cloud 2 の「実行中」タスクをもとに、指定ルールに従ってアプリとプロセスを自動整合することです。

## 1. 動作環境

- macOS 26.5.1 以降（Apple Silicon）
- Go 1.26
- Swift 6.3 / Xcode 26.4
- `tcc2` がインストール済みかつログイン済み
- 本体を利用するユーザーが `config.yml` を管理できる環境
- Windows 版（タスクトレイ常駐、実機未検証）は「11. Windows での利用」を参照

## 2. インストールと起動

### 2-1. 配布アプリの作成

```bash
go build ./...
swift build --package-path macos -c release
bash scripts/make-app-bundle.sh
```

完了すると `dist/TCCLocalConnector.app` が生成されます。  
必要に応じてアプリを配備します。

```bash
cp -R dist/TCCLocalConnector.app /Applications/
```

### 2-2. アプリ起動

- 初回起動後、Dock ではなくメニューバーのアイコンで起動状態を確認します。
- `ad-hoc` 署名のため、初回起動時に Gatekeeper の警告が出る場合は、画面表示に従って許可します。

### 2-3. 設定ファイルの初期化

```bash
mkdir -p ~/.config/tcc-local-connector
cp config.example.yml ~/.config/tcc-local-connector/config.yml
chmod 600 ~/.config/tcc-local-connector/config.yml
```

`task_source.executable` など実行ファイルパスは必ず絶対パスで記入してください。

## 3. メニューの使い方

メニューバーアイコンは `TCC Local Connector` という名称で、以下を提供します。

- `今すぐ再取得`
  - 直ちに 1 サイクルを実行します。
- `設定を再読込`
  - `~/.config/tcc-local-connector/config.yml` を再読み込みします。
- `設定ファイルを開く`
  - 設定ファイルを Finder で開きます。
- `ログを開く`
  - バックエンド動作ログを開きます。
- `15分間一時停止` / `1時間一時停止` / `明日の開始時刻まで一時停止` / `一時停止を解除`
  - 強制停止を一時的に止める運用保守機能です。
- `完全終了…`
  - アプリを終了します。

表示項目:
- 現在のタスク
- 状態（starting / running / backendDown / backendIncompatible など）
- 最終取得時刻

## 4. 設定ファイルの概要

`config.yml` は YAML 形式です。最低限次を満たします。

- `version: 2` を必須指定
- `task_source` は `type: tcc2_mcp` で、`executable` が必須
- `rules` でルール配列を記述（未指定でも可）
- `polling`, `safety`, `logging` は省略可能（省略時は既定値）

主要な `rules[].ensure` のアクション:

- `app.start` / `app.stop`
- `process.start` / `process.stop`
- `command.run`
- `notify`

### 主要な注意点

- `config.yml` は本アプリ権限で任意コード実行になり得る重要ファイルです。
  取り扱い権限とパーミッションを厳密に保ってください。
- `allow_shell: true`、`allow_external_process_control`、`allow_force_terminate` は拒否されます
  （設定検証でエラー）。
- 取得不能な場合は安全側動作に寄せるため、一定時間で制御を解除します。

## 5. 運用推奨手順（段階投入）

1. 最初は `safety.dry_run: true` で検証
2. ルールを 1 つずつ追加（まず `notify`）
3. `app.start` / `app.stop` を追加して通常の挙動を確認
4. `process.*` や `command.run` の追加を最後に実施
5. 安定してから本番運用する

## 6. 制約（運用上の既知制限）

- 強制終了（SIGKILL 相当）は実行しません。終了は基本的に通常終了で行います。
- ブラウザ URL への誘導、tmux / WSL 連携は対象外です。Windows 版は「11. Windows での利用」を参照してください。
- 既定は 60 秒周期です。
- ad-hoc 署名下では通知が制約を受ける場合があります。

## 7. トラブルシューティング

- タスクが取得できない
  - `tcc2 login` と `tcc2 status` を確認
  - `~/.config/tcc-local-connector/config.yml` の `task_source` とパーミッションを確認
- アプリ起動・停止が想定外
  - メニューバーで `今すぐ再取得` を実行して最新状態を反映
  - `app.stop` が `refused` になった場合、終了拒否扱い（安全側）
- バックエンドが繰り返し落ちる
  - ログ上で再起動待機列と失敗を確認
  - 設定エラーはメニューの通知・ログに `config_error` として報告されます
- 一時停止後も動作しているように見える
  - 絶対時刻で判定するため、時刻変更やスリープ復帰時は画面更新まで少し待ちます
- 完全停止したい
  - `一時停止` ではなく「完全終了…」を選択してください

## 8. 主要パス

- 設定: `~/.config/tcc-local-connector/config.yml`
- 一時停止状態: `~/.local/state/tcc-local-connector/pause.json`
- 台帳: `~/.local/state/tcc-local-connector/managed-processes.json`

## 9. 関連情報

- [README.md](README.md): 概要と開発向けビルド情報
- [docs/config-schema.md](docs/config-schema.md): 設定項目の完全仕様
- [docs/protocol-v1.md](docs/protocol-v1.md): バックエンド通信仕様
## 10. Firefox ESR での利用

### 10-1. 導入

1. Firefox ESR を導入します。
2. `make dev` または `bash scripts/make-app-bundle.sh` でアプリを生成します。Host マニフェストはビルド時とアプリ起動時に自動登録されます。
3. Firefox で `about:debugging#/runtime/this-firefox` を開き、「一時的なアドオンを読み込む」から `firefox-extension/manifest.json` を選択します。

拡張は `nativeMessaging`、`webRequest`、`webRequestBlocking`、`<all_urls>` の権限を使用します。Host マニフェストのディレクトリ権限は `0700`、ファイル権限は `0600` です。一時拡張は Firefox 終了時に解除されます。

### 10-2. 設定と確認

設定の正本は `~/.config/tcc-local-connector/config.yml` だけです。遮断対象は `rules[].ensure[]` に記述します。

```yaml
rules:
  - id: focus
    match:
      task_name_contains: [Focus]
    ensure:
      - type: browser.block
        domains: [example.com]
```

最初は `safety.dry_run: true` で起動し、遮断されず予定集合の変更だけが通知されることを確認します。その後 `false` にして、通常ウィンドウの対象ドメインが説明ページへ移ることを確認します。プライベートウィンドウは `incognito=not_allowed` のため対象外です。

設定欠損・破損、pause、取得失敗の猶予超過、制御解放、Host の未接続・切断、ポリシーまたは所有者心拍の期限切れでは fail-open となります。障害後は最悪 16.000 秒以下で通常閲覧の許可へ復帰します。`bash scripts/verify-firefox-extension.sh` を実行し、既存の NDJSON、アプリ制御、通知、`process.*`、`command.run` に回帰がないことも確認します。

### 10-3. 削除とロールバック

削除時は Firefox の一時拡張を削除し、登録時と同じアプリを移動・削除する前に `bash scripts/uninstall-firefox-native-host.sh --app /path/to/TCCLocalConnector.app` を実行して、`browser.block` を `config.yml` から除去します。安全上、登録済みアプリが存在せず Host の絶対パスを照合できない状態では削除スクリプトは失敗します。ロールバック時は拡張を無効化して Host マニフェストを削除し、旧アプリを配置する場合は `bash scripts/install-firefox-native-host.sh --app /path/to/TCCLocalConnector.app` で旧 Host を再登録します。

この一時導入に Mozilla の署名は不要です。署名済み拡張の作成・配布、更新チャネルの運用は本リポジトリの対象外です。
# 既定のタスク開始アクション

`default.on_task_start` に設定したアクションは、取得成功時に新規開始タスクの差分だけを対象として、通常ルールより先に計画されます。欠損・重複した `task_id` や取得失敗では比較状態を更新しません。

`stop.on_task_end` に設定したアクションは、取得成功時に終了タスクの差分だけを対象として、通常ルールの後に計画されます。欠損・重複した `task_id` や取得失敗では比較状態を更新しません。

`safety.dry_run: true` を維持したまま、公開計画とログに実行情報が露出しないことを確認してください。既存のバックエンドアクションを有効化する場合は、設定を棚卸ししてから段階的に切り替えます。

欠損・重複IDを検出したサイクルでは既定アクションと終了アクションを発火しませんが、通常ルールの評価は継続します。フロントエンドアクションは計画順にdispatchされ、同一サイクルの各公開計画が持つ停止対象はdefaultとrulesとstopの和集合です。

## 11. Windows での利用

Windows 版はタスクトレイ常駐の .NET 8 アプリ（`windows/`）と Go バックエンドで構成されます。実 Windows 上での動作確認は未実施です（Linux 上のビルドと単体テストのみ）。

### 11-1. ビルド

WSL/Linux 上で実行します。Go 1.26 と .NET 8 SDK 以上が必要です（`EnableWindowsTargeting` により Linux から win-x64 を publish できます）。

```bash
make win-release
```

完了すると `release/` に次の 3 つが生成されます。

- `TCCLocalConnector.exe`（トレイアプリ）
- `tcc-local-connector-backend.exe`（バックエンド）
- `tcc-firefox-native-host.exe`（Firefox Native Messaging Host）

補助ターゲットとして `make win-build`（ビルド）、`make win-test`（単体テスト）、`make win-clean` があります。

### 11-2. 配置と起動

1. 3 つの exe を同じフォルダに置きます。
2. `config.yml` を exe と同じフォルダに置くとそれが使われます。無ければ `%USERPROFILE%\.config\tcc-local-connector\config.yml` を読みます。
3. `TCCLocalConnector.exe` をダブルクリックするとタスクトレイに常駐します（ウィンドウは開きません）。

トレイアイコンの右クリックでメニューを開きます。項目は macOS 版と同じです（「3. メニューの使い方」を参照）。`notify` アクションはトレイのバルーン通知で表示されます。

状態ファイルとログは `%USERPROFILE%\.local\state\tcc-local-connector\` に置かれます。

- `backend.log`
- `pause.json`
- `managed-processes.json`

### 11-3. 設定の差分

- `bundle_id` は実行ファイル名を書きます（例: `slack.exe`、`firefox.exe`）。
- `app.start` は ShellExecute 経由で起動するため、App Paths レジストリに登録されたアプリは PATH に無くても起動できます。
- `app.stop` はウィンドウ閉じ要求（WM_CLOSE）を送り、grace 秒以内に終了しなければ強制終了します。
- `command.run` の `shell: true` は `cmd /S /C` で実行します。
- `task_source.executable` は tcc2 の exe の絶対パス（Windows パス）を書きます。
- `config.yml` のパーミッション検査（0600）は Windows では行われません。

### 11-4. Firefox

- 起動時に Host マニフェストを `%APPDATA%\TCCLocalConnector\NativeMessagingHosts\jp.takets.tcc_local_connector.firefox.json` へ書き、`HKCU\Software\Mozilla\NativeMessagingHosts\jp.takets.tcc_local_connector.firefox` に登録します。
- 拡張の読み込みは macOS と同じです。`about:debugging#/runtime/this-firefox` から `firefox-extension/manifest.json` を選択します。
- owner heartbeat は `%APPDATA%\TCCLocalConnector\BrowserPolicy\` に書きます。
- 削除は上記のレジストリキーとマニフェストを手動で消します（`scripts/uninstall-firefox-native-host.sh` は macOS 用です）。

### 11-5. 制約とトラブルシューティング

- 実 Windows 上での動作確認は未実施です。
- 未署名 exe のため SmartScreen / Defender の警告が出ることがあります。
- 単一インスタンス保証はありません。多重起動しないよう注意してください。
- トレイに「同梱バックエンドが見つかりません」と出る場合、`tcc-local-connector-backend.exe` が `TCCLocalConnector.exe` と同じフォルダにあるか確認します。
- 「ブラウザ拡張の Host を登録できません」と出る場合、`%APPDATA%` 配下への書き込みと `HKCU` への登録が可能か確認します。
