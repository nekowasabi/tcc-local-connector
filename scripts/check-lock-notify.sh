#!/usr/bin/env bash
# Manual check: the polling cycle (not LaunchWatcher) quits an already-running Slack and shows the Locked notification.
set -euo pipefail
label=jp.takets.tcc-local-connector
domain="gui/$(id -u)"
plist="$HOME/Library/LaunchAgents/$label.plist"
launchctl bootout "$domain/$label" || true  # Why: stop LaunchWatcher so Slack stays open until the first polling cycle.
sleep 2
open -a Slack
sleep 8
launchctl bootstrap "$domain" "$plist"
echo "connector started; Slack should quit within ~10s with a Locked notification"
