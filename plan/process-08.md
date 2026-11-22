# Process 08: denyConfig と otherBrowsersStopped: SNS タスク以外は X と Slack を遮断する設定を有効にする

## goal

`tcc-local-connector-config` スキルで `~/.config/tcc-local-connector/config.yml` に `sns-gate` ルールを足す。`match.task_name_not_contains: ["SNS /"]`（大文字小文字は `containsAnyFold` が区別しない）、`ensure` は `app.stop`（`com.tinyspeck.slackmacgap`・`com.apple.Safari`・`com.google.Chrome`）と `browser.block`（`x.com`・`twitter.com`・`bsky.app`・`discord.com`）。既存の `default-tasks` と `block-x-during-browser-extension-check` は変えない。`config.example.yml` に同じ形の例を足す。

## files

- `~/.config/tcc-local-connector/config.yml`
- `config.example.yml`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard denyConfig +\S+ +未実装'; test $? -eq 1
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard otherBrowsersStopped +\S+ +未実装'; test $? -eq 1
go run ./cmd/tcc-local-connector-backend config-test --config "$HOME/.config/tcc-local-connector/config.yml"
go run ./cmd/tcc-local-connector-backend config-test --config config.example.yml
```

## depends_on

process-07
