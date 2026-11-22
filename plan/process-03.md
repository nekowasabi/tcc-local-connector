# Process 03: pauseBounded: 一時停止を 1 日 1 回・30 分までにする

## goal

`constants.PauseMaxSeconds = 1800` を足す。`engine.Pause` は `until` が今から 30 分を超えるとき、または同じ日（`PauseNextDayStartHour` 区切り）に一時停止を一度使っているとき拒否する（使用日は `pause-budget.json` に残す）。拒否と受理のどちらでも `Event: "paused"` を 1 行ログに出す。メニューから「明日の開始時刻まで一時停止」（`pauseUntilNextDayStart`）を消し、30 分を超える一時停止の選択肢も消す。engine のテスト `TestEnginePauseIsBoundedOncePerDay` を足し、1 時間の一時停止を前提にした既存テストは `state.SavePause` を直接使う形に直す。

## files

- `internal/engine/engine.go`
- `internal/state/pause.go`
- `macos/Sources/TCCLocalConnector/MenuBarApp.swift`
- `macos/Sources/TCCLocalConnector/MenuController.swift`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard pauseBounded +\S+ +未実装'; test $? -eq 1
go test ./internal/engine -count=1 -list 'PauseIsBounded' | grep -q '^TestEnginePauseIsBoundedOncePerDay'
go test ./internal/... ./cmd/... -count=1
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^log +engine 一時停止 +あり'
bash scripts/verify-swift.sh
formal/access-gate/run.sh
```

## depends_on

process-02
