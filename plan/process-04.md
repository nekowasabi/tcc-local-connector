# Process 04: noLoosening: 動作中に遮断を緩める変更を受け付けない

## goal

`engine.SetDryRun(true)` は `dry_run=false` で動いている間は拒否する（`false` への切替だけ受ける）。`engine.LoadConfig` の検証エラーでは `publishEmptyBrowserPolicy` を呼ばず、今のポリシーと `e.cfg` を保つ（通知は今のまま出す）。engine のテスト `TestEngineRefusesLoosening` を足す。

## files

- `internal/engine/engine.go`
- `internal/engine/engine_test.go`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard noLoosening +\S+ +未実装'; test $? -eq 1
go test ./internal/engine -count=1 -list 'RefusesLoosening' | grep -q '^TestEngineRefusesLoosening'
go test ./internal/engine -count=1
formal/access-gate/run.sh
```

## depends_on

process-03
