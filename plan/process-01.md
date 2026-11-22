# Process 01: failClosed: tcc2 の猶予切れでも遮断を続ける

## goal

`failure_policy` に `fail_closed` を足して既定にする（`release_controls` も受け付ける）。`fail_closed` のとき `engine.RunCycleNow` の失敗分岐は、猶予が切れても `publishEmptyBrowserPolicy` と空の plan を出さず、`rules.Evaluate(cfg, nil)`（許可タスクなし）で `BuildBrowserPolicy` と `EnforceStopBundleIDs` を出す。状態は `degraded` のままにし、`MenuController.isHealthyStatus` が heartbeat を止めないようにする。猶予切れの時点で、`release_controls` なら `Event: "controls_released"`、`fail_closed` なら `Event: "fail_closed"` を 1 行ログに出す。`formal/access-gate/conformance/conformance_test.go` の `TestEngineReleasesOnGraceExpiry` を `TestEngineFailClosedOnGraceExpiry` に改め、猶予切れ後も x.com の遮断と Slack の enforce_stop が続くことを期待にする。

## files

- `internal/engine/engine.go`
- `internal/config/config.go`
- `internal/config/validate.go`
- `formal/access-gate/conformance/conformance_test.go`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard failClosed +\S+ +未実装'; test $? -eq 1
go test ./formal/access-gate/conformance -count=1 -list 'FailClosed' | grep -q '^TestEngineFailClosedOnGraceExpiry'
go test ./formal/access-gate/conformance ./internal/config ./internal/engine -count=1
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^log +engine 猶予切れ +あり'
formal/access-gate/run.sh
```

## depends_on

なし
