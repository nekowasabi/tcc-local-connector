#!/bin/bash
# Windows 版の「コネクタがアプリを終わらせたら、ロック通知はちょうど 1 回」を検査する。
#  1. 適合テスト: モデルの「今のコード」の前提が windows/ConnectorCore の LaunchWatcher と一致するか
#  2. everystate (TLC 互換のモデル検査器): 変種 × 性質を全探索し、期待値と照らす
# 使い方: formal/lock-notice/run.sh    期待と違う結果が 1 つでもあれば終了コード 1
# 反例は formal/lock-notice/out/<変種>-<性質>.txt に残る。
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
status=0

if [ "${1:-}" != "--no-dotnet" ]; then
  echo "== 適合テスト"
  dotnet test "$ROOT/windows/ConnectorCore.Tests" --filter "FullyQualifiedName~LockNoticeConformance" --nologo -v q \
    || { echo "適合テストが落ちた: モデルとコードがずれている" >&2; status=1; }
fi

command -v everystate >/dev/null || { echo "everystate が無い (brew install everystate)" >&2; exit 2; }
everystate --help | head -1

ALL='"execNotify", "sharedNotify", "watcherYields", "goneReset"'
without() { echo "$ALL" | sed -E "s/\"$1\"(, )?//; s/, $//"; }
# 1 行 = 変種 | Guards | 性質 | 期待
JOBS="$(mktemp)"
cat > "$JOBS" <<EOF
current|$ALL|NoSilentStop|PASS
current|$ALL|NoDoubleNotice|PASS
port-direct|"execNotify"|NoSilentStop|FAIL
port-direct|"execNotify"|NoDoubleNotice|FAIL
design|$ALL|NoSilentStop|PASS
design|$ALL|NoDoubleNotice|PASS
no-execNotify|$(without execNotify)|NoSilentStop|FAIL
no-sharedNotify|$(without sharedNotify)|NoDoubleNotice|FAIL
no-watcherYields|$(without watcherYields)|NoDoubleNotice|FAIL
no-goneReset|$(without goneReset)|NoSilentStop|FAIL
reach|$ALL|NeverStopped|FAIL
reach|$ALL|NeverNotified|FAIL
EOF

OUT="$DIR/out"; mkdir -p "$OUT"; rm -f "$OUT"/*.txt "$OUT"/*.code
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK" "$JOBS"' EXIT
echo "== everystate"
while IFS='|' read -r v g prop want; do
  d="$WORK/$v-$prop"; mkdir -p "$d"; cp "$DIR/LockNotice.tla" "$d/"
  printf 'CONSTANTS\n  Guards = {%s}\n  Grace = 5\n  Debounce = 5\n  MaxT = 12\n  Runs = 2\n  Launches = 2\nSPECIFICATION Spec\nINVARIANT TypeOK\nINVARIANT %s\n' "$g" "$prop" > "$d/LockNotice.cfg"
  (cd "$d" && everystate -deadlock -workers 1 -config LockNotice.cfg LockNotice.tla > "$OUT/$v-$prop.txt" 2>&1; echo $? > "$OUT/$v-$prop.code") &
done < "$JOBS"
wait
while IFS='|' read -r v g prop want; do
  log="$OUT/$v-$prop.txt"
  case "$(cat "$OUT/$v-$prop.code")" in 0) got=PASS ;; 12) got=FAIL ;; *) got=ERROR ;; esac
  ok=ok; [ "$got" = "$want" ] || { ok=UNEXPECTED; status=1; }
  printf '%-18s %-15s %-5s (期待 %s) %s\n' "$v" "$prop" "$got" "$want" "$ok"
done < "$JOBS"
exit $status
