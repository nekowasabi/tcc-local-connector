# Protocol v1

バックエンドは標準入出力の改行区切り JSON を使います。メソッドは次の順序で公開します。

- `health`
- `echo`
- `sleep`
- `cancel`
- `tcc2_probe`
- `status`
- `reload_config`
- `pause`
- `resume`
- `refresh_now`
- `config_paths`
- `report_actions`

イベントは次の順序で公開します。

- `event.plan`
- `event.state_changed`
- `event.notify`

## capabilities-list

`ready.capabilities` は次の順序で提供されます。

`health`, `echo`, `sleep`, `cancel`, `tcc2_probe`, `status`, `reload_config`, `pause`, `resume`, `refresh_now`, `config_paths`, `report_actions`, `event.plan`, `event.state_changed`, `event.notify`

## メソッド仕様

### `ready`
- 入力: なし
- 出力: `protocol_version` / `capabilities`

### `health`
- 入力: なし
- 出力: `{ "status": "ok" }`

### `echo`
- 入力: `value`
- 出力: `value`

### `sleep`
- 入力: `milliseconds`（0..30000）
- 出力: `{ "completed": true }`

### `cancel`
- 入力: `id`
- 出力: `{ "cancelled_id": "<id>" }`

### `tcc2_probe`
- 入力: なし
- 出力: `{ ...tcc2 probe result... }`

### `status`
- 入力: なし
- 出力: `state`, `cycle_id`, `parse_ok`, `running_tasks`, `errors`, `tcc2`, `config`, `user`, `managed_processes`

### `reload_config`
- 入力: なし
- 出力: `ok`, `applied`, `errors`

### `pause`
- 入力: `duration_seconds` または `until`
- 出力: `until`

### `resume`
- 入力: なし
- 出力: `resumed`

### `refresh_now`
- 入力: なし
- 出力: `accepted`, `cycle_id`

### `config_paths`
- 入力: なし
- 出力: `config`, `state_dir`, `log`, `pause`, `ledger`

### `report_actions`
- 入力: `cycle_id`, `results`
- 出力: `accepted`, `ignored`

## イベント

### `event.plan`
- `cycle_id`
- `issued_at`
- `state`
- `dry_run`
- `actions[]`
- `enforce_stop_bundle_ids[]`

### `event.state_changed`
- `from`
- `to`
- `at`
- `cycle_id`
- `code`
- `message`

### `event.notify`
- `level`
- `code`
- `title`
- `message`
- `at`

## エラーコード

- `message_too_large`
- `invalid_json`
- `unsupported_version`
- `invalid_request`
- `duplicate_id`
- `not_found`
- `invalid_params`
- `cancelled`
- `timeout`
- `unsupported`
- `tcc2_error`
- `method_not_found`
- `config_not_found`
- `insecure_permissions`
- `io_error`
- `busy`
- `paused`
- `invalid_state`
- `internal_error`
- `config_error`
