# Process 09: 残りのログを足し、モデルの「今のコード」を全ガード入りにする

## goal

native host の `readEffectivePolicy` が空のポリシーを返すとき、理由（policy_invalid / heartbeat_stale）付きで stderr に `policy_emptied` を 1 行出す。`formal/access-gate/run.sh` の `code` 変種を `Config = "deny"`・全ガード・`IdleAfter = 0` にして期待を PASS に変える（無タスク計時は即時遮断で置き換わるので `IdleAfter = 0`）。`.claude/skills/verify-access-gate/SKILL.md` の対応表を新しい関数名に合わせ、同期コミットを実装後の HEAD に進める。

## files

- `cmd/tcc-firefox-native-host/main.go`
- `formal/access-gate/run.sh`
- `.claude/skills/verify-access-gate/SKILL.md`

## verify

```sh
out=$(python3 -B formal/access-gate/guards.py --root .) || exit 2; printf '%s\n' "$out" | rg -q '^log +native host 空ポリシー +あり'
go test ./cmd/... -count=1
python3 -B formal/access-gate/guards.py --root . --require
formal/access-gate/run.sh
formal/access-gate/run.sh | rg -q '^code +NoEndlessX +PASS +\(期待 PASS\) ok'
formal/access-gate/run.sh | rg -q '^code +NoEndlessSlack +PASS +\(期待 PASS\) ok'
```

## depends_on

process-08
