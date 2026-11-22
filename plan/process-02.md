# Process 02: persistTimers: 最初の失敗時刻を再起動後も引き継ぐ

## goal

`internal/state` に `failure.json`（`{version, first_failure}`、`pause.json` と同じ状態ディレクトリ・0600・atomic rename）の `LoadFailure` / `SaveFailure` / `ClearFailure` を足す。`Machine` は最初の失敗で保存し、成功で消す。`engine.New` は `state.LoadFailure(...)` で起点を復元し、再起動しても猶予が続きから数えられるようにする。engine のテスト `TestEngineRestartKeepsFirstFailure`（失敗 → New し直す → 猶予を超えた時点で 1 回の失敗で猶予切れになる）を足す。

## files

- `internal/state/failure.go`
- `internal/state/machine.go`
- `internal/engine/engine.go`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard persistTimers +\S+ +未実装'; test $? -eq 1
go test ./internal/engine -count=1 -list 'RestartKeepsFirstFailure' | grep -q '^TestEngineRestartKeepsFirstFailure'
go test ./internal/state ./internal/engine -count=1
formal/access-gate/run.sh
```

## depends_on

process-01
