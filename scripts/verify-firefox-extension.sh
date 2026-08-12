#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
evidence_dir="${FIREFOX_EXTENSION_EVIDENCE_DIR:-${repo_root}/.artifacts/firefox-extension-verification}"
mode="all"

usage() {
  printf 'usage: %s [--schema-drift] [--evidence-dir DIR]\n' "$0" >&2
}

while (($# > 0)); do
  case "$1" in
    --schema-drift)
      mode="schema-drift"
      shift
      ;;
    --evidence-dir)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      evidence_dir="$2"
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

mkdir -p -- "${evidence_dir}"
evidence_dir="$(cd -- "${evidence_dir}" && pwd)"
cd -- "${repo_root}"

run_gate() {
  local name="$1"
  shift
  printf '[RUN] %s\n' "${name}"
  if "$@" >"${evidence_dir}/${name}.log" 2>&1; then
    printf '[PASS] %s\n' "${name}"
  else
    local status=$?
    printf '[FAIL] %s (evidence: %s)\n' "${name}" "${evidence_dir}/${name}.log" >&2
    return "${status}"
  fi
}

verify_schema_drift() {
  python3 - "${repo_root}" <<'PY'
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
policy = (root / "internal/browserpolicy/policy.go").read_text()
heartbeat = (root / "macos/Sources/TCCLocalConnector/BrowserPolicyHeartbeat.swift").read_text()
constants = (root / "internal/constants/constants.go").read_text()
host = (root / "cmd/tcc-firefox-native-host/main.go").read_text()
protocol = (root / "docs/protocol-v1.md").read_text()

policy_keys = ["version", "generation", "enforce", "dry_run", "domains", "planned_domains", "updated_at"]
want = re.search(r'want := \[\]string\{([^}]*)\}', policy)
assert want, "policy strict-key declaration missing"
assert re.findall(r'"([a-z_]+)"', want.group(1)) == policy_keys, "policy schema drift"

payload = re.search(r'private struct Payload: Encodable \{(.*?)\n    \}', heartbeat, re.S)
assert payload, "heartbeat payload missing"
heartbeat_fields = re.findall(r'let ([A-Za-z][A-Za-z0-9]*):', payload.group(1))
assert heartbeat_fields == ["version", "updatedAt"], "heartbeat schema drift"
assert "generation" not in payload.group(1), "heartbeat must not contain generation"

required_constants = {
    "BrowserPolicyVersion": "1",
    "NativeMessageHeaderBytes": "4",
    "NativeMessageMaxPayloadBytes": "65536",
    "BrowserLivenessTTLSeconds": "15",
}
for name, value in required_constants.items():
    assert re.search(rf'\b{name}\s*=\s*{value}\b', constants), f"constant drift: {name}"

assert "BrowserLivenessTTLSeconds" in host, "Host must own liveness TTL evaluation"
assert "binary.LittleEndian" in host, "Native Messaging must use little-endian framing"
assert "既存の NDJSON protocol version 1 は変更しません" in protocol, "NDJSON v1 boundary missing"
assert "4 バイトの little-endian" in protocol, "Native Messaging framing boundary missing"
assert "NDJSON protocol version 1 に event は追加しません" in protocol, "browser.block event boundary missing"
print("schema drift: policy=7 heartbeat=2 generation=policy-only ttl=Host framing=4-byte-separate")
PY
}

verify_w05_installation() {
  local temp_home manifest manifest_dir
  temp_home="$(mktemp -d)"
  trap 'rm -rf -- "${temp_home}"' RETURN
  HOME="${temp_home}" bash scripts/install-firefox-native-host.sh --app dist/TCCLocalConnector.app
  HOME="${temp_home}" bash scripts/install-firefox-native-host.sh --check --app dist/TCCLocalConnector.app
  manifest="${temp_home}/Library/Application Support/Mozilla/NativeMessagingHosts/jp.takets.tcc_local_connector.firefox.json"
  manifest_dir="$(dirname -- "${manifest}")"
  [[ "$(stat -f '%Lp' "${manifest}")" == "600" ]]
  [[ "$(stat -f '%Lp' "${manifest_dir}")" == "700" ]]
  HOME="${temp_home}" bash scripts/uninstall-firefox-native-host.sh --app dist/TCCLocalConnector.app
  [[ ! -e "${manifest}" ]]
  rm -rf -- "${temp_home}"
  trap - RETURN
}

printf 'started_at=%s\ncommit=%s\nrepo=%s\n' \
  "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
  "$(git rev-parse HEAD)" \
  "${repo_root}" >"${evidence_dir}/run-metadata.txt"

if [[ "${mode}" == "schema-drift" ]]; then
  run_gate schema-drift verify_schema_drift
  printf 'evidence_dir=%s\n' "${evidence_dir}"
  exit 0
fi

run_gate w01-config-rules go test ./internal/config ./internal/rules -race -count=1
run_gate w02-policy-engine go test ./internal/browserpolicy ./internal/engine -race -count=1
run_gate w03-swift swift test --package-path macos
run_gate w03-native-host go test ./cmd/tcc-firefox-native-host -race -count=1
run_gate w04-node node --test firefox-extension/tests
run_gate w04-web-ext web-ext lint --source-dir firefox-extension --no-config-discovery
run_gate w05-bundle bash scripts/make-app-bundle.sh
run_gate w05-installation verify_w05_installation
run_gate w06-schema-drift verify_schema_drift
run_gate w06-shellcheck shellcheck scripts/verify-firefox-extension.sh scripts/make-app-bundle.sh scripts/install-firefox-native-host.sh scripts/uninstall-firefox-native-host.sh
run_gate w06-go-regression go test ./... -race -count=1
run_gate w06-docs-readme-esr rg -q 'Firefox ESR' README.md
run_gate w06-docs-readme-config rg -q 'config\.yml' README.md
run_gate w06-docs-readme-browser-block rg -q 'browser\.block' README.md
run_gate w06-docs-manual-dry-run rg -q 'dry_run' MANUAL.md
run_gate w06-docs-manual-fail-open rg -q 'fail-open' MANUAL.md
run_gate w06-docs-manual-ttl rg -q '16\.000' MANUAL.md
run_gate w06-docs-manual-rollback rg -q 'ロールバック' MANUAL.md
run_gate w06-docs-protocol-native-messaging rg -q 'Native Messaging' docs/protocol-v1.md

printf 'Firefox extension verification passed (evidence: %s)\n' "${evidence_dir}"
