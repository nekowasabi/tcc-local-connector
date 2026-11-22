"""AccessGate.tla の Guards ごとに、今のコードに「漏れの行」が残っているかを表示する。

各ガードについて、そのガードが無いことを示すコード（fail-open の行）を正規表現で探す。
見つかれば「未実装」と file:line を出す。見つからなければ「漏れの行なし」（実装済みか、書き方が変わった。対応表で読んで確かめる）。
config と環境のガードは利用者の config.yml と LaunchAgent / Firefox の配置を見る（--config で差し替え）。
目安であり、モデルの結果とは独立。--require を付けると未実装が 1 つでもあれば終了コード 1。
"""
import argparse
import re
import sys
from pathlib import Path

# (ガード, 種類, ファイル, 正規表現, 説明, 判定)
# 判定 present = 正規表現（漏れの行）が見つかれば未実装 / absent = 正規表現（実装で入る名前）が見つからなければ未実装
CODE = [
    ("failClosed", "code", "internal/engine/engine.go", r"if transition\.ReleaseControls \{\s*\n\s*_ = e\.publishEmptyBrowserPolicy\(\)", "猶予切れで空のポリシーを出す", "present"),
    ("failClosed", "code", "internal/config/validate.go", r'FailurePolicy != "release_controls"', "failure_policy は release_controls しか許さない", "present"),
    ("pauseBounded", "code", "macos/Sources/TCCLocalConnector/MenuBarApp.swift", r"pauseUntilNextDayStart", "一時停止は明日の開始時刻まで", "present"),
    ("noLoosening", "code", "internal/engine/engine.go", r"e\.cfg\.Safety\.DryRun = dryRun", "メニューから dry_run に切り替えられる", "present"),
    ("keepLastConfig", "code", "internal/engine/engine.go", r"LastGoodConfig", "壊れた設定で起動すると毎周空のポリシー（最後に有効だった設定 LastGoodConfig を使っていない）", "absent"),
    ("noLoosening", "code", "internal/engine/engine.go", r"if len\(errs\) > 0 \{\s*\n\s*e\.mu\.Unlock\(\)\s*\n\s*_ = e\.publishEmptyBrowserPolicy\(\)", "設定エラーの再読込で空のポリシー", "present"),
    ("closeOpenTabs", "code", "firefox-extension/manifest.json", r'"permissions": \[(?![^\]]*"tabs")', "tabs 権限が無い（開いているタブを閉じられない）", "present"),
    ("privateBlocked", "code", "firefox-extension/manifest.json", r'"incognito": "not_allowed"', "プライベートウィンドウで拡張が動かない", "present"),
    ("privateBlocked", "code", "firefox-extension/background.js", r"details\.incognito === true", "プライベートの読み込みを見ない", "present"),
    ("persistTimers", "code", "internal/engine/engine.go", r"state\.LoadFailure\(", "最初の失敗時刻がメモリだけ（再起動で 0 に戻る。state.LoadFailure を呼んでいない）", "absent"),
    ("pauseBounded", "code", "internal/engine/engine.go", r"constants\.PauseMaxSeconds", "engine.Pause が一時停止の長さを制限していない", "absent"),
]

# fail-open の経路で 1 行ログを出すか（黙って劣化する経路を実ログで見分けるため）
LOGS = [
    ("engine 猶予切れ", "internal/engine/engine.go", r'Event:\s*"(controls_released|fail_closed)"'),
    ("engine 一時停止", "internal/engine/engine.go", r'Event:\s*"paused"'),
    ("native host 空ポリシー", "cmd/tcc-firefox-native-host/main.go", r"policy_emptied"),
]


def line_of(text: str, pos: int) -> int:
    return text.count("\n", 0, pos) + 1


def config_rules(path: Path):
    """ルールごとの本文（- id: から次の - id: まで）。PyYAML に頼らない粗い分割"""
    if not path.is_file():
        return None
    text = path.read_text(encoding="utf-8")
    return re.split(r"\n\s*- id:", text)[1:]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".")
    ap.add_argument("--config", default=str(Path.home() / ".config/tcc-local-connector/config.yml"))
    ap.add_argument("--require", action="store_true")
    args = ap.parse_args()
    root = Path(args.root)
    missing = set()
    for guard, kind, rel, pattern, why, mode in CODE:
        text = (root / rel).read_text(encoding="utf-8")
        m = re.search(pattern, text)
        if mode == "absent":
            if m:
                print(f"guard {guard:<21} {kind:<6} あり    {rel}:{line_of(text, m.start())}")
            else:
                missing.add(guard)
                print(f"guard {guard:<21} {kind:<6} 未実装  {rel}  {why}")
            continue
        if m:
            missing.add(guard)
            print(f"guard {guard:<21} {kind:<6} 未実装  {rel}:{line_of(text, m.start())}  {why}")
        else:
            print(f"guard {guard:<21} {kind:<6} 漏れの行なし  {rel}  （{why} が見つからない。実装済みか書き方が変わった）")

    if not (root / "firefox-extension/background.js").read_text(encoding="utf-8").count("tabs."):
        missing.add("closeOpenTabs")
        print("guard closeOpenTabs         code   未実装  firefox-extension/background.js  tabs API を使っていない")

    agents = list((Path.home() / "Library/LaunchAgents").glob("*tcc*local*connector*.plist"))
    keep = [a for a in agents if "KeepAlive" in a.read_text(encoding="utf-8", errors="ignore")]
    if keep:
        print(f"guard ownerKeepAlive        env    あり    {keep[0]}")
    else:
        missing.add("ownerKeepAlive")
        print("guard ownerKeepAlive        env    未実装  ~/Library/LaunchAgents に KeepAlive 付きの plist なし（Quit で遮断が消える）")

    # 署名済みの拡張がプロファイルにインストールされていれば、再起動しても外れない
    # （about:addons から自分で外すことはできる。これは意図的な迂回で範囲外）
    ext_id = "firefox-domain-blocker@tcc-local-connector.takets.jp"
    installed = sorted(Path.home().glob(f"Library/Application Support/Firefox/Profiles/*/extensions/{ext_id}.xpi"))
    if installed:
        print(f"guard extPinned             env    あり    {installed[0].parent.parent.name}")
    else:
        missing.add("extPinned")
        print("guard extPinned             env    未実装  Firefox のプロファイルに署名済みの拡張が入っていない（make addon で入れる）")

    rules = config_rules(Path(args.config))
    if rules is None:
        missing.update({"denyConfig", "otherBrowsersStopped"})
        print(f"guard denyConfig            config 不明    {args.config} が無い")
    else:
        deny = [r for r in rules if "task_name_not_contains" in r and "task_name_contains" not in r and "browser.block" in r]
        if deny:
            print(f"guard denyConfig            config あり    {args.config}  task_name_not_contains だけのルール")
        else:
            missing.add("denyConfig")
            print(f"guard denyConfig            config 未実装  {args.config}  許可タスク以外で有効になるルールが無い（12 パターンの間だけ遮断）")
        others = [r for r in deny if "com.apple.Safari" in r and "com.google.Chrome" in r]
        if others:
            print("guard otherBrowsersStopped  config あり    Safari と Chrome を app.stop")
        else:
            missing.add("otherBrowsersStopped")
            print("guard otherBrowsersStopped  config 未実装  deny ルールの app.stop に Safari と Chrome が無い")

    for name, rel, pattern in LOGS:
        found = re.search(pattern, (root / rel).read_text(encoding="utf-8"))
        print(f"log   {name:<21}        {'あり' if found else 'なし'}    {rel}")

    print(f"guards: 未実装 {len(missing)} 件: {' '.join(sorted(missing)) or '-'}")
    return 1 if args.require and missing else 0


if __name__ == "__main__":
    sys.exit(main())
