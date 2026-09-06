# TCC Local Connector

## プロジェクト概要

TaskChute Cloud 2 の実行中タスクを取得し、設定したルールに従ってアプリ起動・停止やコマンド実行を行う macOS メニューバー常駐ツールです。

## 何ができるツールか

実行中のタスク名を監視し、ルールに合致したらアプリの起動/停止・コマンド実行・通知・ブラウザでのドメインブロックを自動で行います。

具体的な利用シーン:

- タスク名に「執筆」や「コーディング」などを含む場合、FirefoxでSNSやニュースサイトへのアクセスをそのタスク実行中だけブロックする
- 集中したいタスクの開始時に音楽アプリを起動する
- 集中したいタスクの開始時にSlackなど通知アプリを終了する
- 会議系のタスク名にマッチしたら、録画アプリや議事録アプリを自動起動する
- `default.on_task_start` で、新規タスク開始のたびに一度だけ通知を表示するなど、タスク名によらない既定アクションを設定する
- `stop.on_task_end` で、タスク終了のたびに一度だけ通知を表示するなど、タスク名によらない終了アクションを設定する

## 前提環境

- macOS 26.5.1 以降（Apple Silicon）
- Swift 6.3
- Xcode 26.4
- Go 1.26
- `tcc2` インストール済み、ログイン済み

## ビルド手順

1. 依存取得と Go ビルド
   - `go build ./...`
2. Swift 側ビルド
   - `swift build --package-path macos -c release`
3. アプリ同梱
   - `bash scripts/make-app-bundle.sh`
4. 配置
   - `cp -R dist/TCCLocalConnector.app /Applications/`

`dist/TCCLocalConnector.app` はこのリポジトリの同梱スクリプトで作成されます。

## CLI での設定確認と一巡実行

設定を変更した後は、副作用なしで構文・権限・ルールを確認できます。

- `go run ./cmd/tcc-local-connector-backend config-test --config /path/to/config.yml`
- `go run ./cmd/tcc-local-connector-backend run-once --config /path/to/config.yml`

`run-once` は TaskChute の取得と、設定された Go 側のプロセス・コマンド操作を1回だけ実行します。アプリ起動・通常終了・通知は macOS メニューバーアプリが `plan` を受けて実行します。

## 設定の作り方

1. `config.example.yml` をコピー
   - `mkdir -p ~/.config/tcc-local-connector`
   - `cp config.example.yml ~/.config/tcc-local-connector/config.yml`
2. 実行権限を持つパスを指定
   - `task_source.executable` は絶対パスで記入（PATH 依存を避ける）
3. 設定ファイル保護
   - `chmod 600 ~/.config/tcc-local-connector/config.yml`
4. `config.yml` は本アプリの権限で任意コード実行できる性質があるため、所有者・パーミッション管理を必ず行う。

## ad-hoc 署名と Gatekeeper 回避

`bash scripts/make-app-bundle.sh` は ad-hoc 署名を行います。初回起動時は Gatekeeper の警告が出る場合があります。
回避手順:
- 右クリックで開く
- またはシステム設定から `開発元を確認できません` を許可

## 段階投入の推奨手順

1. `dry_run: true`、`rules: []` で 2〜3 日運用し、影響範囲を観測
2. `dry_run: true` で通常ルールを追加し 1〜2 日運用
3. `dry_run: false` かつ `notify` / `app.start` のみ有効化
4. `app.stop` を追加してアプリ停止まで許可
5. `process.start` / `command.run` を追加

## 既知の制約

- 強制終了は行いません。完全終了は `quit` でのみ実行
- ブラウザ URL 誘導は対象外です
- tmux / nvim 連携は対象外です
- Windows 版は実 Windows 上での動作確認が未実施です（「Windows 版」節を参照）
- 外部プロセス名だけでの停止は実施しません
- `config.yml` を書き換えられる利用者は本アプリ権限で任意コードを実行できる
- `DefaultFailureGraceSeconds` 相当の遅延後に制御を解放する場合があります

## 障害時 runbook

1. 強制したい場合: メニュー > 一時停止 > 明日の開始時刻まで
2. 続けて止まらない場合: Quit して手動で対象を確認
3. タスクが取れない: `tcc2 status` / `tcc2 login` / 取得ログを確認
4. バックエンド再起動: stderr を確認し、版不一致なら同一タグで再起動
5. 通知が出ない: メニューの最終取得時刻と `notify` 動作を確認
6. 完全に戻す: Quit、設定と状態ファイルの確認

## ポーリング間隔

- 既定: `DefaultPollIntervalSeconds` = 60 秒
- 最小: 10 秒（`MinPollIntervalSeconds`）
- 180 秒より長い失敗連続は設計上の解除遅延に影響するため、既定を推奨

## 参照ログと出力場所

- 設定: `~/.config/tcc-local-connector/config.yml`
- 一時停止: `~/.local/state/tcc-local-connector/pause.json`
- 台帳: `~/.local/state/tcc-local-connector/managed-processes.json`

## Windows 版

タスクトレイ常駐の .NET 8 アプリ（`windows/`）と Go バックエンドを WSL/Linux 上でクロスビルドします。実 Windows での動作確認は未実施です（Linux 上のビルドと単体テストのみ）。

### ビルド

- 要件: Go 1.26、.NET 8 SDK 以上（`EnableWindowsTargeting` により Linux から win-x64 を publish 可）
- `make win-release` で `release/` に次の 3 つが出力されます
  - `TCCLocalConnector.exe`（トレイアプリ）
  - `tcc-local-connector-backend.exe`（バックエンド）
  - `tcc-firefox-native-host.exe`（Firefox Native Messaging Host）
- 補助: `make win-build`（ビルド）、`make win-test`（単体テスト）、`make win-clean`

### 配置と起動

- 3 つの exe を同じフォルダに置きます。
- `config.yml` を exe と同じフォルダに置くとそれが使われます。無ければ `%USERPROFILE%\.config\tcc-local-connector\config.yml` を読みます。
- 状態ファイルとログは `%USERPROFILE%\.local\state\tcc-local-connector\`（`backend.log`、`pause.json`、`managed-processes.json`）です。
- `TCCLocalConnector.exe` をダブルクリックするとトレイに常駐します（ウィンドウなし）。トレイアイコンの右クリックメニューは macOS 版と同じ項目です。`notify` はトレイのバルーン通知で表示します。

### 設定の差分

- `bundle_id` は実行ファイル名を書きます（例: `slack.exe`、`firefox.exe`）。
- `app.start` は ShellExecute 経由で起動するため、App Paths レジストリに登録されたアプリは PATH に無くても起動できます。
- `app.stop` はウィンドウ閉じ要求（WM_CLOSE）を送り、grace 秒以内に終了しなければ強制終了します。
- `command.run` の `shell: true` は `cmd /S /C` で実行します。
- `task_source.executable` は tcc2 の exe の絶対パス（Windows パス）を書きます。
- `config.yml` のパーミッション検査（0600）は Windows では行われません。

### Firefox

起動時に Host マニフェストを `%APPDATA%\TCCLocalConnector\NativeMessagingHosts\jp.takets.tcc_local_connector.firefox.json` へ書き、`HKCU\Software\Mozilla\NativeMessagingHosts\jp.takets.tcc_local_connector.firefox` に登録します。拡張の読み込みは macOS と同じく `about:debugging` から `firefox-extension/manifest.json` を選択します。owner heartbeat は `%APPDATA%\TCCLocalConnector\BrowserPolicy\` に書きます。削除はレジストリキーとマニフェストを手動で消します（uninstall スクリプトは macOS 用）。

### 制約

- 実 Windows 上での動作確認は未実施です。
- 未署名 exe のため SmartScreen / Defender の警告が出ることがあります。
- 単一インスタンス保証はありません（多重起動を防ぎません）。

## Firefox ESR 拡張の導入

Firefox ESR を導入してから、アプリをビルドし、一時拡張を読み込みます。Native Messaging Host は `make dev` / `scripts/make-app-bundle.sh` とアプリ起動時に自動登録します。対象ブラウザは Firefox のみです。

```bash
make dev
```

Firefox で `about:debugging#/runtime/this-firefox` を開き、「一時的なアドオンを読み込む」から `firefox-extension/manifest.json` を選択します。設定の正本は `~/.config/tcc-local-connector/config.yml` だけです。`browser.block` は `ensure` に記述します。

```yaml
rules:
  - id: focus
    match:
      task_name_contains: [Focus]
    ensure:
      - type: browser.block
        domains: [example.com]
```

拡張の権限は `nativeMessaging`、`webRequest`、`webRequestBlocking`、`<all_urls>` です。Host マニフェストは利用者専用のディレクトリ権限 `0700`、ファイル権限 `0600` で登録されます。`safety.dry_run: true` では遮断せず、予定集合の変更を通知だけで確認できます。設定欠損・破損、pause、取得失敗の猶予超過、制御解放、Host の未接続・切断、ポリシーまたは所有者心拍の期限切れでは fail-open とし、最悪 16.000 秒以下で許可へ復帰します。通常ウィンドウだけが対象で、プライベートウィンドウには `incognito=not_allowed` により作用しません。

削除は一時拡張を Firefox から削除して `bash scripts/uninstall-firefox-native-host.sh` を実行します。ロールバックは拡張を無効化し、Host マニフェストを削除し、`browser.block` を設定から除去します。旧版へ戻す場合は旧アプリを配置して Host マニフェストを再登録します。一時導入には Mozilla の署名は不要ですが、署名済み拡張の作成・配布は本リポジトリの対象外です。最後に `bash scripts/verify-firefox-extension.sh` を実行し、既存の NDJSON、アプリ制御、通知、`process.*`、`command.run` に回帰がないことを確認します。
# default.on_task_start

`default.on_task_start` は、タスク取得で新規開始を検知したときに、通常ルールより先に一度だけ計画されます。初回取得は空集合との差分として扱います。設定しない場合は空です。

`stop.on_task_end` は、タスク取得で終了を検知したときに、通常ルールの後に一度だけ計画されます。初回取得では終了差分は空です。設定しない場合は空です。

実行安全性のため `safety.dry_run: true` が既定です。バックエンドアクションを有効化する前に、設定ファイルの棚卸しと dry-run の結果確認を行ってください。既定フェーズと終了フェーズでは `browser.block` は使用できません。

欠損・重複した `task_id` では開始・終了比較状態を更新せず、既定アクションと終了アクションだけを抑制します。通常ルールの評価と公開計画は継続します。フロントエンドアクションはdefault→rules→stopの計画順にdispatchされ、各公開計画はdefaultとrulesとstopを結合した同じ停止対象集合を保持します。
