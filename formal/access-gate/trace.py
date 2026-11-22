"""TLC の反例を読み、指定した経路を通っているかを確かめる。run.sh から呼ぶ。
使い方: trace.py <TLC の出力> <印>
  印 action:<名前>      反例にその操作がある（例 action:TccDown）
  印 tab:<場所>         X をその場所で見ている状態がある（例 tab:private）
  印 openWhileBlocked   拡張が x.com を遮断しているのに通常ウィンドウで X が見えている状態がある
一致すれば終了コード 0。反例の操作列を 1 行で標準出力に出す。
"""
import re
import sys

text = open(sys.argv[1], encoding="utf-8", errors="replace").read()
marker = sys.argv[2]
states = re.split(r"\nState \d+: ", text)[1:]
names = [(re.match(r"<(\w+)", s) or [None, "?"])[1] for s in states]
print(" -> ".join(names))
if marker.startswith("action:"):
    ok = marker.split(":", 1)[1] in names
elif marker.startswith("tab:"):
    ok = any('tab = "%s"' % marker.split(":", 1)[1] in s for s in states)
elif marker == "openWhileBlocked":
    ok = any('tab = "ff"' in s and "extBlock = TRUE" in s for s in states)
else:
    ok = False
sys.exit(0 if ok else 1)
