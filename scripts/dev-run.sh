#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
bash "$root/scripts/make-app-bundle.sh"
open "$root/dist/TCCLocalConnector.app"
