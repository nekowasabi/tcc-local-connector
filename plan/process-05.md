# Process 05: keepLastConfig: 壊れた設定で起動しても最後に有効だった設定で動く

## goal

`LoadConfig` が成功したら、読んだ設定のバイト列を状態ディレクトリの `last-good-config.yml`（0600、atomic rename）に保存する（定数 `LastGoodConfig`）。起動時に `config.yml` が検証エラーなら `last-good-config.yml` を `config.Load` で読み、使えればそれで動き、設定エラーの通知は出す。どちらも無いときだけ今の `config_error` の動きにする。engine のテスト `TestEngineStartsWithLastGoodConfig` を足す。

## files

- `internal/engine/engine.go`
- `internal/constants/constants.go`
- `internal/engine/engine_test.go`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^guard keepLastConfig +\S+ +未実装'; test $? -eq 1
go test ./internal/engine -count=1 -list 'LastGoodConfig' | grep -q '^TestEngineStartsWithLastGoodConfig'
go test ./internal/engine -count=1
formal/access-gate/run.sh
```

## depends_on

process-04
