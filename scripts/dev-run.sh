#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
app="$root/dist/TCCLocalConnector.app"
executable="$app/Contents/MacOS/TCCLocalConnector"

running_pids=$(pgrep -f "$executable" || true)
if [[ -n "$running_pids" ]]; then
  # Why: Match the bundle executable path instead of the process name to avoid terminating an unrelated app.
  for pid in $running_pids; do
    kill -TERM "$pid"
  done

  for pid in $running_pids; do
    for ((attempt = 0; attempt < 50; attempt++)); do
      if ! kill -0 "$pid" 2>/dev/null; then
        break
      fi
      sleep 0.1
    done
    if kill -0 "$pid" 2>/dev/null; then
      echo "TCCLocalConnector did not terminate: pid=$pid" >&2
      exit 1
    fi
  done
fi

bash "$root/scripts/make-app-bundle.sh"
open "$app"
