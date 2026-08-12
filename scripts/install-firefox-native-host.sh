#!/usr/bin/env bash
set -euo pipefail

FirefoxNativeHostName='jp.takets.tcc_local_connector.firefox'
FirefoxExtensionID='firefox-domain-blocker@tcc-local-connector.takets.jp'
# The native messaging manifest uses type=stdio and one allowed_extensions entry.
root=$(cd "$(dirname "$0")/.." && pwd -P)
app_arg="$root/dist/TCCLocalConnector.app"
check_only=false

usage() {
  printf 'Usage: %s [--check] [--app APP_PATH]\n' "${0##*/}" >&2
}

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --check)
      check_only=true
      shift
      ;;
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

resolve_app_and_host() {
  [[ -d "$app_arg" ]] || fail "app bundle not found: $app_arg"
  app=$(cd "$app_arg" && pwd -P) || fail "cannot resolve app bundle: $app_arg"
  local host_parent="$app/Contents/Resources"
  [[ -d "$host_parent" ]] || fail "app Resources directory not found: $host_parent"
  host_parent=$(cd "$host_parent" && pwd -P) || fail "cannot resolve app Resources directory"
  host="$host_parent/tcc-firefox-native-host"

  case "$host" in
    "$app"/Contents/Resources/tcc-firefox-native-host) ;;
    *) fail "native host resolves outside the app bundle" ;;
  esac
  [[ -f "$host" && ! -L "$host" ]] || fail "native host is not a regular file: $host"
  [[ -x "$host" ]] || fail "native host is not executable: $host"
  [[ "$(stat -f '%Lp' "$host")" = 700 ]] || fail "native host mode must be 0700: $host"
}

validate_manifest() {
  local candidate=$1
  local expected_path=$2
  [[ -f "$candidate" && ! -L "$candidate" ]] || fail "manifest is not a regular file: $candidate"
  python3 - "$candidate" "$FirefoxNativeHostName" "$FirefoxExtensionID" "$expected_path" <<'PY' || fail "manifest content mismatch: $candidate"
import json
import os
import sys

manifest_path, expected_name, expected_extension, expected_host = sys.argv[1:]
with open(manifest_path, encoding="utf-8") as manifest_file:
    manifest = json.load(manifest_file)
assert type(manifest) is dict
assert set(manifest) == {"name", "description", "path", "type", "allowed_extensions"}
assert manifest["name"] == expected_name
assert type(manifest["description"]) is str and manifest["description"]
assert manifest["path"] == expected_host and os.path.isabs(manifest["path"])
assert manifest["type"] == "stdio"
assert manifest["allowed_extensions"] == [expected_extension]
PY
  [[ -f "$expected_path" && ! -L "$expected_path" && -x "$expected_path" ]] || fail "manifest path is not an executable regular file"
  [[ "$(stat -f '%Lp' "$expected_path")" = 700 ]] || fail "manifest host mode must be 0700"
}

resolve_app_and_host
home=$(cd "$HOME" && pwd -P) || fail "cannot resolve HOME"
manifest_dir="$home/Library/Application Support/Mozilla/NativeMessagingHosts"
manifest="$manifest_dir/$FirefoxNativeHostName.json"

if "$check_only"; then
  [[ -d "$manifest_dir" && ! -L "$manifest_dir" ]] || fail "manifest directory not found or unsafe: $manifest_dir"
  [[ "$(stat -f '%Lp' "$manifest_dir")" = 700 ]] || fail "manifest directory mode must be 0700"
  [[ "$(stat -f '%Lp' "$manifest")" = 600 ]] || fail "manifest mode must be 0600"
  validate_manifest "$manifest" "$host"
  printf 'Firefox native host check passed: %s\n' "$manifest"
  exit 0
fi

mkdir -p "$manifest_dir"
[[ -d "$manifest_dir" && ! -L "$manifest_dir" ]] || fail "manifest directory is unsafe: $manifest_dir"
chmod 0700 "$manifest_dir"

temp=$(mktemp "$manifest_dir/.${FirefoxNativeHostName}.json.XXXXXX") || fail "cannot create manifest temporary file"
backup=''
installed=false
cleanup() {
  local status=$?
  if "$installed"; then
    if [[ -n "$backup" && -f "$backup" ]]; then
      mv -f -- "$backup" "$manifest" || printf 'error: could not restore prior manifest\n' >&2
      backup=''
    else
      rm -f -- "$manifest" || printf 'error: could not remove failed manifest\n' >&2
    fi
  fi
  [[ -z "${temp:-}" ]] || rm -f -- "$temp"
  [[ -z "$backup" ]] || rm -f -- "$backup"
  return "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
chmod 0600 "$temp"
python3 - "$temp" "$FirefoxNativeHostName" "$FirefoxExtensionID" "$host" <<'PY'
import json
import sys

manifest_path, name, extension_id, host_path = sys.argv[1:]
manifest = {
    "name": name,
    "description": "TCC Local Connector Firefox native messaging host",
    "path": host_path,
    "type": "stdio",
    "allowed_extensions": [extension_id],
}
with open(manifest_path, "w", encoding="utf-8") as manifest_file:
    json.dump(manifest, manifest_file, indent=2)
    manifest_file.write("\n")
PY
chmod 0600 "$temp"
validate_manifest "$temp" "$host"

# Why: A same-directory rename prevents Firefox from observing a partially written manifest.
if [[ -e "$manifest" || -L "$manifest" ]]; then
  [[ -f "$manifest" && ! -L "$manifest" ]] || fail "existing manifest is not a regular file"
  backup=$(mktemp "$manifest_dir/.${FirefoxNativeHostName}.backup.XXXXXX") || fail "cannot preserve existing manifest"
  cp -- "$manifest" "$backup"
  chmod 0600 "$backup"
fi
# Why: Ignore termination only across the rename commit point so cleanup cannot
# mistake an installed manifest for an uncommitted temporary file.
trap '' HUP INT TERM
mv -f -- "$temp" "$manifest"
temp=''
installed=true
trap 'exit 130' HUP INT TERM
validate_manifest "$manifest" "$host"
[[ "$(stat -f '%Lp' "$manifest")" = 600 ]] || fail "installed manifest mode must be 0600"
installed=false
[[ -z "$backup" ]] || rm -f -- "$backup"
backup=''
printf 'Installed Firefox native host manifest: %s\n' "$manifest"
