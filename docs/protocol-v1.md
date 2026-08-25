# Protocol v1

`ready`, `status`, `reload_config`, `pause`, `resume`, `refresh_now`, `config_paths`, `report_actions`, `event.plan`, `event.state_changed`, `event.notify`

The macOS menu talks to the backend over stdin/stdout NDJSON. 既存の NDJSON protocol version 1 は変更しません. NDJSON protocol version 1 に event は追加しません.

Firefox Native Messaging is a separate framing path: 4 バイトの little-endian length prefix plus JSON, not NDJSON.
