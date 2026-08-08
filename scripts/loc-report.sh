#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go_nontest_loc=$(rg -g '*.go' -g '!*_test.go' -n '^' internal cmd | wc -l | tr -d ' ')
swift_nontest_loc=$(rg -g '*.swift' -g '!**/Tests/**' -n '^' macos/Sources | wc -l | tr -d ' ')
echo "go_nontest_loc=$go_nontest_loc"
echo "swift_nontest_loc=$swift_nontest_loc"
threshold=1200
if (( go_nontest_loc > threshold || swift_nontest_loc > threshold )); then
  echo "loc_decision=exceeds_threshold; W10 requires documented review"
else
  echo "loc_decision=within_threshold; W10 LOC review is not required"
fi
