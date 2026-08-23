#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0

check_count() {
  local id="$1"
  local cmd="$2"
  local expected="$3"
  local observed
  observed="$( (bash -lc "$cmd" 2>/dev/null || true) | wc -l | tr -d ' ')"
  if [[ "$observed" != "$expected" ]]; then
    echo "$id: ${cmd} -> observed=$observed expected=$expected"
    fail=1
  else
    echo "$id: ok observed=$observed expected=$expected"
  fi
}

check_min() {
  local id="$1"
  local cmd="$2"
  local min="$3"
  local observed
  observed="$( (bash -lc "$cmd" 2>/dev/null || true) | wc -l | tr -d ' ')"
  if (( observed < min )); then
    echo "$id: ${cmd} -> observed=$observed expected>=${min}"
    fail=1
  else
    echo "$id: ok observed=$observed expected>=${min}"
  fi
}

check_eq() {
  local lhs="$1"
  local rhs="$2"
  local msg="$3"
  if [[ "$lhs" != "$rhs" ]]; then
    echo "$msg: lhs=$lhs rhs=$rhs"
    fail=1
  else
    echo "$msg: ok"
  fi
}

# D-01 .. D-15
check_count "D-01" "rg -n 'forceTerminate\\(|SIGKILL|signal\\.SIGKILL|syscall\\.SIGKILL|kill -9' internal cmd macos/Sources scripts --glob '!forbidden-audit.sh'" "1"
check_count "D-02" "rg -n '\\bpkill\\b|\\bkillall\\b|runningApplications\\(\\)' internal cmd macos/Sources" "0"
check_count "D-03" "rg -n '\"/bin/sh\"|\"-c\"|bash -c|zsh -c|sh -c' internal cmd" "1"
# CLI exposes config, status, and serve output through explicit stdout sinks.
check_count "D-04" "rg -n 'fmt\\.Print|os\\.Stdout|println\\(' internal cmd" "4"
check_count "D-05" "rg -n 'wsl\\.exe|GOOS=windows|go:build windows|NotifyIcon|PowerShell|wslpath' internal cmd macos/Sources scripts/make-app-bundle.sh scripts/dev-run.sh" "0"
check_count "D-06" "rg -n 'Logged in as|\\bEmail\\b|Bearer|password|secret|credential' internal macos/Sources" "5"
check_count "D-07" "rg -n 'com\\.tinyspeck|com\\.amazon\\.Lassen|/opt/homebrew|/Users/[A-Za-z0-9_.-]+' internal cmd macos/Sources --glob '!**/testdata/**'" "0"
check_count "D-08" "rg -ni 'in-progress count|in_progress_count' internal cmd macos/Sources" "0"
check_count "D-09" "rg -n '\\b(60|180|30000|65536|1048576|86400|21600)\\b' internal/engine internal/rules internal/state internal/ledger internal/config --glob '!*_test.go'" "0"
check_count "D-10" "rg -n 'time\\.Since|monotonic|remainingSeconds|elapsedSeconds|time\\.Tick' internal/state" "0"
check_min "D-11" "rg -n 'len\\(validationErrors\\)\\s*>\\s*0|len\\(validationErrors\\)\\s*!=\\s*0' internal/config/config.go" "1"
check_count "D-12" "rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' internal cmd macos/Sources scripts --glob '!forbidden-audit.sh'" "0"
check_count "D-13" "rg -n 'retry.*[0-9]+\\s*回|retries?\\s*[:=]\\s*[0-9]' PLAN-for-mac.md plan-for-mac/" "0"
check_count "D-14" "rg -n 'xcodebuild|\\.xcodeproj|XCUIApplication|XCUIElement' . --glob '!docs/**' --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**' --glob '!scripts/forbidden-audit.sh'" "0"
check_count "D-15" "rg -n 'golangci-lint|swiftlint|swift-format' . --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**' --glob '!scripts/forbidden-audit.sh'" "0"

# Internal constants check used as substitute for local unused-check script.
if ! staticcheck ./internal/constants >/tmp/forbidden-audit-staticcheck.log 2>&1; then
  echo "D-100: staticcheck ./internal/constants -> failed"
  cat /tmp/forbidden-audit-staticcheck.log
  fail=1
else
  echo "D-100: staticcheck ./internal/constants -> ok"
fi

go_default_failure_grace="$(awk -F'[= ]+' '/DefaultFailureGraceSeconds/ {for(i=1;i<=NF;i++){if($i ~ /^[0-9]+$/){print $i; break}}}' internal/constants/constants.go)"
swift_backend_down_grace="$(awk -F'[^0-9]+' '/backendDownReleaseGraceSeconds/ {print $2; exit}' macos/Sources/ConnectorCore/Constants.swift)"
check_eq "$go_default_failure_grace" "180" "Go DefaultFailureGraceSeconds"
check_eq "$swift_backend_down_grace" "180" "Swift backendDownReleaseGraceSeconds"

go_pause_next_day="$(awk -F'[= ]+' '/PauseNextDayStartHour/ {for(i=1;i<=NF;i++){if($i ~ /^[0-9]+$/){print $i; exit}}}' internal/constants/constants.go)"
swift_pause_next_day="$(awk -F'[^0-9]+' '/pauseNextDayStartHour/ {print $2; exit}' macos/Sources/ConnectorCore/Constants.swift)"
check_eq "$go_pause_next_day" "5" "Go PauseNextDayStartHour"
check_eq "$swift_pause_next_day" "5" "Swift pauseNextDayStartHour"

go_protocol_version="$(awk -F'[= ]+' '/Version[[:space:]]*=/{print $NF; exit}' internal/protocol/server.go)"
swift_protocol_version="$(awk -F'[^0-9]+' '/supportedProtocolVersion/ {print $2; exit}' macos/Sources/ConnectorCore/Constants.swift)"
check_eq "$go_protocol_version" "1" "Go protocol.Version"
check_eq "$swift_protocol_version" "1" "Swift supportedProtocolVersion"

if (( fail != 0 )); then
  exit 1
fi
