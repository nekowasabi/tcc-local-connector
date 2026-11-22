#!/usr/bin/env bash
# firefox-extension を AMO の unlisted（自分だけが使う配布）として署名し、署名済み xpi を置く。
# API キーは https://addons.mozilla.org/ja/developers/addon/api/key/ で作り、
# ~/.config/tcc-local-connector/amo-jwt-issuer と amo-jwt-secret（0600）に 1 行ずつ置く。
# キーはコマンド行に出さず、web-ext の環境変数で渡す（ps に残さないため）。
# AMO は同じ version に二度署名しないので、manifest.json の version の末尾を 1 つ上げてから署名する。署名に失敗したら元の version に戻す。
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd -P)
creds="$HOME/.config/tcc-local-connector"
out="$HOME/.local/share/tcc-local-connector"
for f in amo-jwt-issuer amo-jwt-secret; do
  [ -s "$creds/$f" ] || { echo "$creds/$f がありません" >&2; exit 2; }
  chmod 600 "$creds/$f"
done
manifest="$root/firefox-extension/manifest.json"
work=$(mktemp -d)
cp "$manifest" "$work/manifest.json.before"
signed_ok=0
trap '[ "$signed_ok" = 1 ] || cp "$work/manifest.json.before" "$manifest"; rm -rf "$work"' EXIT
node -e '
const fs = require("fs"); const p = process.argv[1]; const s = fs.readFileSync(p, "utf8");
const m = s.match(/"version": "(\d+(?:\.\d+)*)"/); if (!m) { console.error("manifest.json に version がありません"); process.exit(2); }
const parts = m[1].split("."); parts[parts.length - 1] = String(Number(parts.at(-1)) + 1);
fs.writeFileSync(p, s.replace(m[0], `"version": "${parts.join(".")}"`)); console.log("version " + m[1] + " -> " + parts.join("."));
' "$manifest"
WEB_EXT_API_KEY=$(tr -d '[:space:]' < "$creds/amo-jwt-issuer") \
WEB_EXT_API_SECRET=$(tr -d '[:space:]' < "$creds/amo-jwt-secret") \
  npx --yes web-ext@10.8.0 sign --channel unlisted \
    --source-dir "$root/firefox-extension" --artifacts-dir "$work" \
    --ignore-files background.test.js
signed=$(ls "$work"/*.xpi | head -n 1)
signed_ok=1
mkdir -p "$out"
install -m 600 "$signed" "$out/firefox-domain-blocker.xpi"
echo "$out/firefox-domain-blocker.xpi"
