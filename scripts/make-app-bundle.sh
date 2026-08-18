#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
app="$root/dist/TCCLocalConnector.app/Contents"
rm -rf "$root/dist/TCCLocalConnector.app"
mkdir -p "$app/MacOS" "$app/Resources"
go build -o "$app/Resources/tcc-local-connector-backend" "$root/cmd/tcc-local-connector-backend"
native_host="$app/Resources/tcc-firefox-native-host"
go build -o "$native_host" "$root/cmd/tcc-firefox-native-host"
test -f "$native_host" && test ! -L "$native_host"
chmod 0700 "$native_host"
swift build --package-path "$root/macos" -c release
cp "$root/macos/.build/release/TCCLocalConnector" "$app/MacOS/TCCLocalConnector"
cp "$root/macos/Resources/Info.plist" "$app/Info.plist"
codesign --force --sign - "$native_host"
codesign --force --sign - "$root/dist/TCCLocalConnector.app"
test -x "$native_host"
test "$(stat -f '%Lp' "$native_host")" = 700
# Why: Bundle build registers hosts for the current user so `make dev` does not need a second CLI.
bash "$root/scripts/install-native-hosts.sh" --app "$root/dist/TCCLocalConnector.app"
