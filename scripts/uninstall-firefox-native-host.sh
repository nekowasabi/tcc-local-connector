#!/usr/bin/env bash
set -euo pipefail

FirefoxNativeHostName='jp.takets.tcc_local_connector.firefox'
root=$(cd "$(dirname "$0")/.." && pwd -P)
app_arg="$root/dist/TCCLocalConnector.app"

usage() {
  printf 'Usage: %s [--app APP_PATH]\n' "${0##*/}" >&2
}

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --app)
      (($# >= 2)) || { usage; exit 2; }
      app_arg=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

[[ -d "$app_arg" ]] || fail "app bundle not found: $app_arg"
app=$(cd "$app_arg" && pwd -P) || fail "cannot resolve app bundle: $app_arg"
host_parent="$app/Contents/Resources"
[[ -d "$host_parent" ]] || fail "app Resources directory not found: $host_parent"
host_parent=$(cd "$host_parent" && pwd -P) || fail "cannot resolve app Resources directory"
host="$host_parent/tcc-firefox-native-host"
case "$host" in
  "$app"/Contents/Resources/tcc-firefox-native-host) ;;
  *) fail "native host resolves outside the app bundle" ;;
esac
[[ -f "$host" && ! -L "$host" ]] || fail "native host is not a regular file: $host"

home=$(cd "$HOME" && pwd -P) || fail "cannot resolve HOME"
manifest="$home/Library/Application Support/Mozilla/NativeMessagingHosts/$FirefoxNativeHostName.json"
if [[ ! -e "$manifest" && ! -L "$manifest" ]]; then
  printf 'Firefox native host manifest is not installed: %s\n' "$manifest"
  exit 0
fi
[[ -f "$manifest" && ! -L "$manifest" ]] || fail "manifest is not a regular file"
readarray=()
while IFS= read -r value; do
  readarray+=("$value")
done < <(
python3 - "$manifest" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as manifest_file:
    manifest = json.load(manifest_file)
assert type(manifest) is dict
assert type(manifest.get("name")) is str
assert type(manifest.get("path")) is str
print(manifest["name"])
print(manifest["path"])
PY
)
[[ "${#readarray[@]}" = 2 ]] || fail "manifest is invalid"
name=${readarray[0]}
path=${readarray[1]}
[[ "$name" = "$FirefoxNativeHostName" ]] || fail "manifest name mismatch; refusing removal"
[[ "$path" = "$host" ]] || fail "manifest path mismatch; refusing removal"

# Why: Name and generated path matching confines removal to this app's own manifest.
rm -- "$manifest"
printf 'Removed Firefox native host manifest: %s\n' "$manifest"
