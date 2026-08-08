#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
swift build --package-path macos -c release
swift test --package-path macos
