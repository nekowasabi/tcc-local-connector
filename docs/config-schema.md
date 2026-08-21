# 設定スキーマ

## `default.on_task_start`

タスク取得成功時に新規開始を検知したタスクへ適用する、既定アクションの配列です。通常の `rules` より前に計画されます。未定義の場合は空配列として扱います。

```yaml
default:
  on_task_start:
    - type: notify
      title: Task started
      message: Review the dry-run plan
```

| 項目 | 制約 |
|---|---|
| アクション数 | 最大20件。YAMLの記述順を保持する |
| 許可する種類 | `app.start`、`app.stop`、`process.start`、`process.stop`、`command.run`、`notify` |
| 禁止する種類 | `browser.block` |
| 時間制限 | 省略または `0` は30秒へ正規化。指定値は1〜300秒 |
| アプリ操作 | 同じ既定配列内で同一bundleの `app.start` と `app.stop` を併用しない |
| プロセス操作 | 同じ既定配列内で同一 `process_id` の `process.start` と `process.stop` を併用しない |
| shell | `safety.allow_shell` の既存制約を継承する |
| 実行パス | 実行ファイルは絶対パス、実行可能であること。作業ディレクトリも絶対パス |
| 環境変数 | 既存のキー形式と値検証を継承する |

開始判定は、取得に成功したタスクの有効かつ一意な `task_id` 集合と、直前の有効な成功取得との差分で行います。初回取得は空集合との差分です。複数タスクが同時に追加されても、既定アクション列はそのサイクルで一度だけ計画されます。

pause、取得失敗、grace/released、不正な設定reload、欠損・重複した `task_id` では比較集合を更新せず、既定アクションを発火しません。欠損・重複IDの場合も通常ルールの評価は継続します。

既定アクションは `default` フェーズ、通常ルールは `rules` フェーズとして内部計画に格納され、`ActionID` は両フェーズを通した連番です。各アクションは計画順に所有実行器へdispatchされます。既定アクションの失敗や時間切れは残りの既定アクションと通常ルールを停止しません。

公開 `plan` にはフロントエンドアクションだけを含めます。同一サイクルで複数のフロントエンドアクションをdispatchする場合、各イベントはdefaultとrulesを結合した同じ停止対象集合を保持します。`process.start`、`process.stop`、`command.run` の実行パス、引数、環境変数、標準出力、標準エラー、詳細情報は公開イベントやログへ出力しません。

## 安全設定

```json
{"safety":{"dry_run":true}}
```

`dry_run` の既定値は `true` です。バックエンドアクションを有効化する前に設定を棚卸しし、dry-run の計画とログを確認してください。
