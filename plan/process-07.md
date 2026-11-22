# Process 07: ownerKeepAlive と extPinned: アプリと拡張が落ちても戻る

## goal

`scripts/install-launch-agent.sh` を足し、`~/Library/LaunchAgents/jp.takets.tcc-local-connector.plist`（`/Applications/TCCLocalConnector.app/Contents/MacOS/TCCLocalConnector`、`RunAtLoad`・`KeepAlive` true）を書いて `launchctl bootstrap gui/$(id -u)` で読み込む。`scripts/install-firefox-policy.sh` を足し、`firefox-extension` を xpi に固めて Firefox ESR の `distribution/policies.json` に `ExtensionSettings`（id `firefox-domain-blocker@tcc-local-connector.takets.jp`、`installation_mode: force_installed`、`private_browsing: true`）と、ESR だけが受け付ける `xpinstall.signatures.required: false` を書く。両スクリプトは何度実行しても同じ結果になるようにする（読み込み済みなら `launchctl bootout` してから `bootstrap`、policies.json は上書き）。xpi は `~/.local/share/tcc-local-connector/firefox-domain-blocker.xpi` に置き、`install_url` は `file://` で指す。Firefox は `/Applications/Firefox*.app` から探し、無ければ非 0 で止まる。両スクリプトを一度実行し、README の Firefox 節を一時アドオンから常設に書き換える。

## files

- `scripts/install-launch-agent.sh`
- `scripts/install-firefox-policy.sh`
- `README.md`

## verify

```sh
bash scripts/install-launch-agent.sh
bash scripts/install-firefox-policy.sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard ownerKeepAlive +\S+ +未実装'; test $? -eq 1
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard extPinned +\S+ +未実装'; test $? -eq 1
launchctl print gui/$(id -u)/jp.takets.tcc-local-connector
```

## depends_on

process-06
