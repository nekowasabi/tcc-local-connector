# 設定スキーマ

本書は `docs/config-schema.md` の仕様であり、実装は `internal/config/config.go` / `internal/config/validate.go` に準拠する。

- 設定の形式は YAML。
- `version: 2` が必須。
- `task_source.executable` が必須。
- `task_source.type` は `tcc2_mcp` のみ。
- `task_source.view_id` は任意。
- `polling` / `safety` / `logging` / `rules` は任意。
- `polling.failure_policy` は `release_controls` のみ許可。

`config.yml` は本人限定（機密保護）で書き換えられる状態が前提であり、利用者が書き換え可能である場合は任意コード実行に相当するため、その前提を明記する。`allow_shell: true` の設定は権限チェックで追加制約を受ける（後述）。

## トップレベル

| キー | 型 | 必須 | 既定値 | 概要 |
|---|---|---|---|---|
| `version` | number | true | `2` | スキーマのバージョン |
| `task_source` | object | true | （なし） | MCP 呼び出し元設定 |
| `polling` | object | false | `{"interval_seconds":60,"timeout_seconds":20,"failure_grace_seconds":180,"failure_policy":"release_controls"}` | 取得ポーリング関連 |
| `safety` | object | false | `{"dry_run":false,"allow_shell":false,"allow_external_process_control":false,"allow_force_terminate":false}` | セーフガード関連 |
| `logging` | object | false | `{"level":"info","retain_days":14}` | ログ出力関連 |
| `rules` | array | false | `[]` | ルール定義配列（最大100件） |

## task_source

- `type`（string, required）: `tcc2_mcp` のみ
- `executable`（string, required）: 絶対パス必須。実行可能ファイルであること（`executable()` 検査）
- `args`（array[string], optional）: 既定 `["mcp"]`
- `view_id`（string, optional）: `null` または文字列

## polling

- `interval_seconds`（number, optional）:
  - 必須範囲 `10..3600`
  - 既定 `60`
  - 必要なら `timeout_seconds` より大きい値
- `timeout_seconds`（number, optional）:
  - 必須範囲 `1..120`
  - 既定 `20`
- `failure_grace_seconds`（number, optional）:
  - 必須範囲 `0..3600`
  - 既定 `180`
- `failure_policy`（string, optional）:
  - 固定値 `release_controls`

## safety

- `dry_run`（bool, optional）: テスト実行時の副作用抑止
- `allow_shell`（bool, optional）: `true` 時はファイル許可が厳格化（0600 相当の上位制約）
- `allow_external_process_control`（bool, optional）: 既定 `false`。`true` は受理しない
- `allow_force_terminate`（bool, optional）: 既定 `false`。`true` は受理しない

## logging

- `level`（string, optional）: `debug|info|warn|error`
- `retain_days`（number, optional）:
  - 必須範囲 `1..365`
  - 既定 `14`

## rules

- `rules` は配列要素の上限 100。
- 各要素 `id` は識別子形式（`[a-z0-9][a-z0-9-]{0,63}`）。
- `priority` は整数。
- `match.task_name_contains` / `match.task_name_not_contains` は配列で記載。
  - 判定は **`contains` がOR、`not_contains` がAND NOT**。
  - 実行中タスク0件時は `contains` は偽、`not_contains` は真。
  - 空文字は禁止。
- `ensure` / `on_enter` / `on_exit` は各配列で最大20件。
- `browser.block` は `ensure` だけで使用でき、`on_enter` / `on_exit` では拒否される。
- `app.stop` / `process.stop` / `command.run` は `grace_seconds` / `timeout_seconds` 等の範囲チェックあり。
- 空配列も妥当。

## Action 7 種

### `app.start`

- `type: "app.start"`
- `bundle_id`（必須）
- `title`/`message`/`level` は不可

### `app.stop`

- `type: "app.stop"`
- `bundle_id`（単一対象）または `bundle_ids`（複数対象、`browser.block` の `domains` と同じリスト形式）。少なくとも一方が必須
- `grace_seconds`（optional / 範囲 `1..120`）

### `process.start`

- `type: "process.start"`
- `process_id`（必須、重複不可）
- `executable`（必須、絶対パスで実行可能）
- `args`（optional）
- `working_dir`（optional、絶対パスで存在するディレクトリ）
- `env`（optional、キーは識別子形式）

### `process.stop`

- `type: "process.stop"`
- `process_id`（必須）
- `grace_seconds`（optional / 範囲 `1..120`）

### `command.run`

- `type: "command.run"`
- `executable`（必須、絶対パスで実行可能）
- `args`（optional）
- `timeout_seconds`（optional / 範囲 `1..300`）
- `shell`（optional）
  - `safety.allow_shell` が `false` の場合はエラー

### `notify`

- `type: "notify"`
- `title`（必須、最大200 runes）
- `message`（必須、最大500 runes）
- `level`（optional、`info|warn|error` のみ、空文字も許容）

### `browser.block`

- `type: "browser.block"`
- `domains`（array[string], required）: 1..128件のDNS名
- 各要素はASCII小文字化され、末尾のドット1個が除去される。
- 全体は253バイト以下、各ラベルは63バイト以下のASCII英数字または `-` とし、ラベルの先頭・末尾の `-` と空ラベルは禁止する。
- URL、path、port、wildcard、IPv4/IPv6、非ASCIIは拒否する。IDNはpunycodeで指定する。
- このアクションはFirefox向けポリシーへ分離され、既存の `Plan.Actions` には出力されない。

## 検証エラーコード（19種）

ここに挙げる 19 種は `internal/config/validate.go` の検証結果として返る。

1. `unsupported_config_version`
2. `missing_required_field`
3. `unknown_field`
4. `unknown_action_type`
5. `unsupported_action`
6. `duplicate_rule_id`
7. `duplicate_process_id`
8. `value_out_of_range`
9. `timeout_exceeds_interval`
10. `unsupported_value`
11. `executable_not_found`
12. `invalid_identifier`
13. `shell_not_allowed`
14. `forbidden_safety_flag`
15. `rule_conflict_same_priority`
16. `browser_block_requires_ensure`
17. `browser_domains_required`
18. `browser_domains_limit`
19. `invalid_browser_domain`

## 取り扱い上の要点

- `browser.redirect` は実行時に `unsupported_action`（明示的拒否）。
- 設定ファイルの正本は `~/.config/tcc-local-connector/config.yml`。
- 権限違反（`ErrInsecurePermissions`）は設定値の解析成功後にスキーマとは別経路で拒否され、`shell` 実行可否の判定には影響しない。
- スキーマ違反は `ValidationError[]` にして行単位で通知する。
