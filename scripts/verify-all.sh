#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/verify-go.sh
bash scripts/verify-swift.sh
bash scripts/make-app-bundle.sh
bash scripts/forbidden-audit.sh
bash scripts/loc-report.sh
echo "ALL AUTOMATED VERIFICATION PASSED"
