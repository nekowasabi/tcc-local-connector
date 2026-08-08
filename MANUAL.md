# 利用マニュアル

本書は macOS 用常駐ツール **TCC Local Connector** の運用手順です。
目標は、TaskChute Cloud 2 の「実行中」タスクをもとに、指定ルールに従ってアプリとプロセスを自動整合することです。

## 1. 動作環境

- macOS 26.5.1 以降（Apple Silicon）
- Go 1.26
- Swift 6.3 / Xcode 26.4
- `tcc2` がインストール済みかつログイン済み
- 本体を利用するユーザーが `config.yml` を管理できる環境

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
5. 安定後にログイン時起動を有効化

## 6. 制約（運用上の既知制限）

- 強制終了（SIGKILL 相当）は実行しません。終了は基本的に通常終了で行います。
- ブラウザ URL への誘導、tmux / WSL / Windows 連携は対象外です。
- 既定は 60 秒周期です。
- ad-hoc 署名下では通知やログイン時起動登録が制約を受ける場合があります。

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
- [docs/macos-verification-2026-08-07.md](docs/macos-verification-2026-08-07.md): 実機検証結果
