# TCC Local Connector

## プロジェクト概要

TaskChute Cloud 2 の実行中タスクを取得し、設定したルールに従ってアプリ起動・停止やコマンド実行を行う macOS メニューバー常駐ツールです。

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

## ログイン時起動の有効化

1. メニューから設定画面を開く
2. ログイン時起動を有効化
3. 設定が ON になっていることを確認
4. ad-hoc 署名下では登録失敗する場合があるため、失敗時はメニュー上のステータス表示を確認

## 段階投入の推奨手順

1. `dry_run: true`、`rules: []` で 2〜3 日運用し、影響範囲を観測
2. `dry_run: true` で通常ルールを追加し 1〜2 日運用
3. `dry_run: false` かつ `notify` / `app.start` のみ有効化
4. `app.stop` を追加してアプリ停止まで許可
5. `process.start` / `command.run` を追加
6. 最後にログイン時起動を有効化

## 既知の制約

- 強制終了は行いません。完全終了は `quit` でのみ実行
- ブラウザ URL 誘導は対象外です
- tmux / nvim 連携は対象外です
- Windows 版は対象外です
- 外部プロセス名だけでの停止は実施しません
- `config.yml` を書き換えられる利用者は本アプリ権限で任意コードを実行できる
- `DefaultFailureGraceSeconds` 相当の遅延後に制御を解放する場合があります

## 障害時 runbook

1. 強制したい場合: メニュー > 一時停止 > 明日の開始時刻まで
2. 続けて止まらない場合: Quit して手動で対象を確認
3. タスクが取れない: `tcc2 status` / `tcc2 login` / 取得ログを確認
4. バックエンド再起動: stderr を確認し、版不一致なら同一タグで再起動
5. 通知が出ない: メニューの最終取得時刻と `notify` 動作を確認
6. 完全に戻す: ログイン時起動 OFF、Quit、設定と状態ファイルの確認

## ポーリング間隔

- 既定: `DefaultPollIntervalSeconds` = 60 秒
- 最小: 10 秒（`MinPollIntervalSeconds`）
- 180 秒より長い失敗連続は設計上の解除遅延に影響するため、既定を推奨

## 参照ログと出力場所

- 設定: `~/.config/tcc-local-connector/config.yml`
- 一時停止: `~/.local/state/tcc-local-connector/pause.json`
- 台帳: `~/.local/state/tcc-local-connector/managed-processes.json`

