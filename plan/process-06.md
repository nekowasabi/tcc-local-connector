# Process 06: closeOpenTabs と privateBlocked: 開いているタブとプライベートウィンドウも遮断する

## goal

`manifest.json` に `tabs` 権限を足し、`incognito` を `spanning` にする。`background.js` は新しいポリシーを採用したとき `browserApi.tabs.query({})` で対象ドメインのタブを探し、`tabs.update(id, {url: blockedUrl(host)})` でブロック画面へ移す。`onBeforeRequest` から `details.incognito === true` の素通しを外す。`background_conformance.test.js` の privateBlocked・closeOpenTabs の 2 件を「プライベートでも遮断する」「採用時に対象タブをブロック画面へ移す」に改め、`background.test.js` に同じ挙動のテストを足す。

## files

- `firefox-extension/manifest.json`
- `firefox-extension/background.js`
- `firefox-extension/background.test.js`
- `formal/access-gate/conformance/background_conformance.test.js`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard closeOpenTabs +\S+ +未実装'; test $? -eq 1
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard privateBlocked +\S+ +未実装'; test $? -eq 1
node --test firefox-extension/background.test.js formal/access-gate/conformance/background_conformance.test.js
formal/access-gate/run.sh
```

## depends_on

process-05
