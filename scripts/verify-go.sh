#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go build ./...
go test ./... -race -count=1
threshold_check() {
  local package="$1" threshold="$2" output coverage
  output=$(go test "./$package" -cover 2>&1)
  printf '%s\n' "$output"
  coverage=$(printf '%s\n' "$output" | awk '/coverage:/{gsub(/%/,"",$5); print $5; exit}')
  if [[ -z "$coverage" ]] || awk "BEGIN { exit !($coverage >= $threshold) }"; then
    return 0
  fi
  echo "coverage threshold failed: $package=${coverage}% < ${threshold}%" >&2
  return 1
}
threshold_check internal/config 85
threshold_check internal/tcc2 85
threshold_check internal/rules 90
