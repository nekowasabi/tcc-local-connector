#!/usr/bin/env bash
set -euo pipefail

while IFS= read -r line; do
  case "$line" in
    *'"method":"notifications/initialized"'*) continue ;;
  esac
  id=$(printf '%s\n' "$line" | sed -nE 's/.*"id":([0-9]+).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18"}}\n' "$id"
      ;;
    *'"name":"get_user"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"- **Timezone:** UTC\\n- **Start of Day:** -05:00:00"}]}}\n' "$id"
      ;;
    *'"name":"get_taskchute"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"## 2026-08-07\\n- [In Progress] E2E task [ID: task_0123456789abcdef0123456789abcdef]"}]}}\n' "$id"
      ;;
  esac
done
