#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/verify-go.sh
bash scripts/verify-swift.sh
bash scripts/make-app-bundle.sh
bash scripts/forbidden-audit.sh
bash scripts/loc-report.sh
test -f docs/evidence/e2e-markers.txt
for marker in ready plan report_actions:accepted paused resumed; do
  rg -q "^${marker//:/ }$|^$marker$" docs/evidence/e2e-markers.txt
done
echo "ALL AUTOMATED VERIFICATION PASSED"
