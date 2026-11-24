#!/usr/bin/env bash
# Install dist/TCCLocalConnector.app (built by `make bundle`) into /Applications and restart the LaunchAgent.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
open -a Slack || true  # Why: a running Slack lets the first polling cycle after restart show the Locked notification.
ditto "$root/dist/TCCLocalConnector.app" /Applications/TCCLocalConnector.app
sleep 5
launchctl kickstart -k "gui/$(id -u)/jp.takets.tcc-local-connector"
echo "installed and restarted"
