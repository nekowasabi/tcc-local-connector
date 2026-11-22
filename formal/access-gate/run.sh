#!/bin/bash
# 「許可タスクを宣言していない間、X と Slack を見続けられない」を検査する。
#  1. guards.py: 今のコードに残る漏れの行（目安。モデルの結果とは独立）
#  2. 適合テスト: モデルの定義が今のコードと一致するか（Go と拡張の JS）
#  3. TLC: 変種 × 性質を全探索し、期待値と照らす
#     code = 今のコードと今の config.yml / deny-config = 設定だけ deny にしたもの / design-* = ガード全部
#     no-<ガード> = そのガードだけ外したもの（そのガードの経路で、そのガードが守る性質だけが落ちるのが期待）
#     reach = 許可タスク中は見られること、遮断の後も許可タスクの開始で解けること（空振りでないことの確認。FAIL が期待）
# 使い方: formal/access-gate/run.sh            期待と違う結果が 1 つでもあれば終了コード 1
# 反例と TLC の出力は formal/access-gate/out/<変種>-<性質>.txt に残る。
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
JAR="$DIR/.tools/tla2tools.jar"
TLA_VERSION="v1.7.4"
TLA_SHA256="936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
status=0

python3 -B "$DIR/guards.py" --root "$ROOT"

echo "== 適合テスト"
(cd "$ROOT" && go test ./formal/access-gate/conformance -count=1) || { echo "適合テストが落ちた: モデルとコードがずれている" >&2; status=1; }
if ! node_out="$(cd "$ROOT" && node --test formal/access-gate/conformance/background_conformance.test.js 2>&1)"; then
  printf '%s\n' "$node_out" | grep -E '^not ok|^✖|AssertionError' >&2
  echo "拡張の適合テストが落ちた: モデルとコードがずれている" >&2; status=1
fi

if [ ! -f "$JAR" ]; then
  mkdir -p "$DIR/.tools"
  curl -sSL -o "$JAR" "https://github.com/tlaplus/tlaplus/releases/download/$TLA_VERSION/tla2tools.jar" || exit 2
fi
if [ "$(shasum -a 256 "$JAR" | cut -d' ' -f1)" != "$TLA_SHA256" ]; then
  echo "tla2tools.jar のハッシュが $TLA_VERSION と一致しない: $JAR" >&2
  exit 2
fi

GUARDS="failClosed pauseBounded noLoosening keepLastConfig ownerKeepAlive closeOpenTabs privateBlocked extPinned otherBrowsersStopped persistTimers"
guards_without() {  # 1 つ外したガードの集合（空文字なら全部）を TLA+ の集合の中身で出す
  local out="" g
  for g in $GUARDS; do
    [ "$g" = "$1" ] && continue
    [ -n "$out" ] && out="$out, "
    out="$out\"$g\""
  done
  echo "$out"
}
# 1 行 = 変種 Config Guards GraceTicks PauseMax IdleAfter IdleOnFailure 性質 期待 印
JOBS="$(mktemp)"
{
  echo "code deny ALL 1 1 0 hold NoEndlessX PASS -"
  echo "code deny ALL 1 1 0 hold NoEndlessSlack PASS -"
  echo "deny-config deny - 1 1 1 hold NoEndlessX FAIL -"
  echo "deny-config deny - 1 1 1 hold NoEndlessSlack FAIL -"
  echo "design-G1P1I1hold deny ALL 1 1 1 hold ALL PASS -"
  echo "design-G1P1I1count deny ALL 1 1 1 count ALL PASS -"
  echo "design-G1P1I0hold deny ALL 1 1 0 hold ALL PASS -"
  echo "design-G2P2I1hold deny ALL 2 2 1 hold ALL PASS -"
  echo "reach deny ALL 1 1 1 hold NeverXWhileAllowed FAIL -"
  echo "reach deny ALL 1 1 1 hold NeverSlackWhileAllowed FAIL -"
  echo "reach deny ALL 1 1 1 hold NoReopenX FAIL -"
  echo "reach deny ALL 1 1 1 hold NoReopenSlack FAIL -"
  # ガード 印 X が落ちるか Slack が落ちるか
  while read -r g mark x s; do
    echo "no-$g deny no-$g 1 1 1 hold NoEndlessX $x $mark"
    echo "no-$g deny no-$g 1 1 1 hold NoEndlessSlack $s $mark"
  done <<'TABLE'
failClosed action:TccDown FAIL FAIL
pauseBounded action:UserPause FAIL FAIL
noLoosening action:UserDryRun FAIL FAIL
keepLastConfig action:UserBreakConfig FAIL FAIL
ownerKeepAlive action:OwnerQuit FAIL FAIL
closeOpenTabs openWhileBlocked FAIL PASS
privateBlocked tab:private FAIL PASS
extPinned action:FirefoxRestart FAIL PASS
otherBrowsersStopped tab:other FAIL PASS
persistTimers action:OwnerQuit FAIL FAIL
TABLE
} > "$JOBS"

OUT="$DIR/out"; mkdir -p "$OUT"; rm -f "$OUT"/*.txt
mkdir -p "$DIR/.tools"
WORK="$(mktemp -d "$DIR/.tools/work.XXXXXX")"; trap 'rm -rf "$WORK" "$JOBS"' EXIT

run_one() {
  local v="$1" config="$2" guards="$3" g="$4" p="$5" ia="$6" iof="$7" prop="$8" d gs invs
  d="$WORK/$v-$prop"; mkdir -p "$d/tmp"; cp "$DIR/AccessGate.tla" "$d/"
  case "$guards" in
    -) gs="" ;;
    ALL) gs="$(guards_without "")" ;;
    *) gs="$(guards_without "$(echo "$guards" | sed 's/^no-//')")" ;;
  esac
  if [ "$prop" = ALL ]; then invs="TypeOK NoEndlessX NoEndlessSlack"; else invs="TypeOK $prop"; fi
  {
    printf 'CONSTANTS\n  Config = "%s"\n  Guards = {%s}\n  GraceTicks = %s\n  PauseMax = %s\n  IdleAfter = %s\n  IdleOnFailure = "%s"\nSPECIFICATION Spec\n' "$config" "$gs" "$g" "$p" "$ia" "$iof"
    for i in $invs; do printf 'INVARIANT %s\n' "$i"; done
  } > "$d/AccessGate.cfg"
  (cd "$d" && java -Djava.io.tmpdir="$d/tmp" -XX:+UseParallelGC -XX:TieredStopAtLevel=1 -jar "$JAR" -workers 1 \
      -metadir "$d/states" -config AccessGate.cfg AccessGate.tla) > "$OUT/$v-$prop.txt" 2>&1
}

echo "== TLC"
while read -r v config guards g p ia iof prop want mark; do
  run_one "$v" "$config" "$guards" "$g" "$p" "$ia" "$iof" "$prop" &
  while [ "$(jobs -r | wc -l)" -ge 8 ]; do sleep 0.2; done
done < "$JOBS"
wait

while read -r v config guards g p ia iof prop want mark; do
  log="$OUT/$v-$prop.txt"
  if grep -q "No error has been found" "$log"; then got=PASS
  elif grep -q "is violated" "$log"; then got=FAIL
  else got=ERROR; fi
  note=""
  if [ "$got" = FAIL ] && [ "$mark" != "-" ] && [ "$want" = FAIL ]; then
    if path="$(python3 -B "$DIR/trace.py" "$log" "$mark")"; then note="経路 $mark"; else got="FAIL(別経路)"; note="$path"; fi
  fi
  ok="ok"; [ "$got" = "$want" ] || { ok="UNEXPECTED"; status=1; }
  printf '%-22s %-22s %-5s (期待 %s) %s %s\n' "$v" "$prop" "$got" "$want" "$ok" "$note"
done < "$JOBS"
exit $status
