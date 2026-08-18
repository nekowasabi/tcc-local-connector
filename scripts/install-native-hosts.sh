#!/usr/bin/env bash
set -euo pipefail

# Registers every configured native-messaging host present in the app bundle.
# Keep browser entries in sync with NativeMessagingCatalog in
# macos/Sources/TCCLocalConnector/NativeMessagingInstaller.swift.
# Adding Chrome: set CHROME_ORIGINS to the chrome-extension:// origin. The shared
# Firefox host binary is reused until a Chrome-specific host is required.

ChromeNativeHostName='jp.takets.tcc_local_connector.chrome'
ChromeHostFile='tcc-firefox-native-host'
ChromeManifestDirRel='Google/Chrome/NativeMessagingHosts'
ChromeDescription='TCC Local Connector Chrome native messaging host'
CHROME_ORIGINS=()

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

[[ -d "$app_arg" ]] || fail "app bundle not found: $app_arg"
app=$(cd "$app_arg" && pwd -P) || fail "cannot resolve app bundle: $app_arg"

firefox_args=(--app "$app")
if "$check_only"; then
  firefox_args=(--check --app "$app")
fi
bash "$root/scripts/install-firefox-native-host.sh" "${firefox_args[@]}"

if ((${#CHROME_ORIGINS[@]} == 0)); then
  exit 0
fi

host="$app/Contents/Resources/$ChromeHostFile"
case "$host" in
  "$app"/Contents/Resources/"$ChromeHostFile") ;;
  *) fail "native host resolves outside the app bundle" ;;
esac
[[ -f "$host" && ! -L "$host" ]] || fail "native host is not a regular file: $host"
[[ -x "$host" ]] || fail "native host is not executable: $host"
[[ "$(stat -f '%Lp' "$host")" = 700 ]] || fail "native host mode must be 0700: $host"

home=$(cd "$HOME" && pwd -P) || fail "cannot resolve HOME"
manifest_dir="$home/Library/Application Support/$ChromeManifestDirRel"
manifest="$manifest_dir/$ChromeNativeHostName.json"

if "$check_only"; then
  [[ -d "$manifest_dir" && ! -L "$manifest_dir" ]] || fail "chrome manifest directory not found or unsafe: $manifest_dir"
  [[ "$(stat -f '%Lp' "$manifest_dir")" = 700 ]] || fail "chrome manifest directory mode must be 0700"
  [[ "$(stat -f '%Lp' "$manifest")" = 600 ]] || fail "chrome manifest mode must be 0600"
  python3 - "$manifest" "$ChromeNativeHostName" "$host" "${CHROME_ORIGINS[@]}" <<'PY' || fail "chrome manifest content mismatch: $manifest"
import json
import os
import sys

manifest_path, expected_name, expected_host, *origins = sys.argv[1:]
with open(manifest_path, encoding="utf-8") as manifest_file:
    manifest = json.load(manifest_file)
assert type(manifest) is dict
assert set(manifest) == {"name", "description", "path", "type", "allowed_origins"}
assert manifest["name"] == expected_name
assert manifest["path"] == expected_host and os.path.isabs(manifest["path"])
assert manifest["type"] == "stdio"
assert manifest["allowed_origins"] == origins
PY
  printf 'Chrome native host check passed: %s\n' "$manifest"
  exit 0
fi

mkdir -p "$manifest_dir"
[[ -d "$manifest_dir" && ! -L "$manifest_dir" ]] || fail "chrome manifest directory is unsafe: $manifest_dir"
chmod 0700 "$manifest_dir"
temp=$(mktemp "$manifest_dir/.${ChromeNativeHostName}.json.XXXXXX") || fail "cannot create chrome manifest temporary file"
cleanup() {
  [[ -z "${temp:-}" ]] || rm -f -- "$temp"
}
trap cleanup EXIT
chmod 0600 "$temp"
python3 - "$temp" "$ChromeNativeHostName" "$ChromeDescription" "$host" "${CHROME_ORIGINS[@]}" <<'PY'
import json
import sys

manifest_path, name, description, host_path, *origins = sys.argv[1:]
manifest = {
    "name": name,
    "description": description,
    "path": host_path,
    "type": "stdio",
    "allowed_origins": origins,
}
with open(manifest_path, "w", encoding="utf-8") as manifest_file:
    json.dump(manifest, manifest_file, indent=2)
    manifest_file.write("\n")
PY
chmod 0600 "$temp"
mv -f -- "$temp" "$manifest"
temp=''
[[ "$(stat -f '%Lp' "$manifest")" = 600 ]] || fail "installed chrome manifest mode must be 0600"
printf 'Installed Chrome native host manifest: %s\n' "$manifest"
