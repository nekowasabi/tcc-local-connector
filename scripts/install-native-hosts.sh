#!/usr/bin/env bash
set -euo pipefail

# Registers native-messaging hosts present in the app bundle.
# Keep browser entries in sync with NativeMessagingCatalog in
# macos/Sources/TCCLocalConnector/NativeMessagingInstaller.swift.

root=$(cd "$(dirname "$0")/.." && pwd -P)
app_arg="$root/dist/TCCLocalConnector.app"
check_only=false

usage() {
  printf 'Usage: %s [--check] [--app APP_PATH]\n' "${0##*/}" >&2
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

firefox_args=(--app "$app_arg")
if "$check_only"; then
  firefox_args=(--check --app "$app_arg")
fi
bash "$root/scripts/install-firefox-native-host.sh" "${firefox_args[@]}"
