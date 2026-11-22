#!/usr/bin/env bash
set -euo pipefail

label=jp.takets.tcc-local-connector
executable=/Applications/TCCLocalConnector.app/Contents/MacOS/TCCLocalConnector
domain="gui/$(id -u)"
agents="$HOME/Library/LaunchAgents"
plist="$agents/$label.plist"

if [[ ! -x "$executable" ]]; then
  printf 'アプリが見つかりません: %s\n' "$executable" >&2
  exit 1
fi

mkdir -p "$agents"
tmp=$(mktemp "$agents/.$label.XXXXXX")
trap 'rm -f -- "$tmp"' EXIT
cat > "$tmp" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$executable</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
PLIST
plutil -lint "$tmp"
mv -f -- "$tmp" "$plist"
if launchctl print "$domain/$label" >/dev/null 2>&1; then
  launchctl bootout "$domain/$label"
  # Why: launchd can still be unloading the previous process after bootout returns.
  for ((attempt = 0; attempt < 20; attempt++)); do
    if ! launchctl print "$domain/$label" >/dev/null 2>&1 &&
       launchctl bootstrap "$domain" "$plist" 2>/dev/null; then
      exit 0
    fi
    sleep 0.25
  done
fi
launchctl bootstrap "$domain" "$plist"
