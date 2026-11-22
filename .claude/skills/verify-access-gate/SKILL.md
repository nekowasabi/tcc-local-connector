---
name: verify-access-gate
description: tcc-local-connector の「許可タスクを TaskChute で開始（宣言）していない間は、X などの SNS と Slack を見続けられない」を TLA+ で全探索する。モデルは engine のポーリング 1 周・一時停止・設定エラー・dry_run・アプリ終了と再起動・Firefox 拡張（native host・プライベートウィンドウ・開いたままのタブ・再起動で外れる一時アドオン）・別ブラウザ・LaunchWatcher を表し、漏れの経路ごとに 10 のガードを持つ。ガードを 1 つ外すと、その経路で X か Slack を無限に見続けられることを示す。既定拒否（deny ルール・allow_task_name_contains）や無タスク計時（idle）を実装・変更するとき、internal/engine/engine.go の RunCycleNow / LoadConfig / Pause / SetDryRun、internal/rules の Evaluate / BuildBrowserPolicy / BuildPlan、internal/state/machine.go、cmd/tcc-firefox-native-host、firefox-extension/background.js・manifest.json、macos の LaunchWatcher / MenuController / BrowserPolicyHeartbeat / MenuBarApp の一時停止を変えるとき、「設定しているのに X が見られた・Slack が開けた」を調べるときに使う。
---

# 宣言しないと SNS と Slack を使えない（access gate）の検証

## 何を保証するか

| 名前 | 利用者への約束（1 文） | 破れたときの害 |
|---|---|---|
| `NoEndlessX` | 許可タスクを実行していない間に X が見えている時間は、ポーリング `GraceTicks + PauseMax + 1` 回分を超えない | 設定しているのに、どこかの穴から X を無限に見続けてしまう（利用者が挙げた最悪の状態） |
| `NoEndlessSlack` | 許可タスクを実行していない間に Slack が起動している時間は、`GraceTicks + PauseMax + IdleAfter + 1` 回分を超えない | 同じく Slack を開きっぱなしにできる |
| `NeverXWhileAllowed` / `NeverSlackWhileAllowed` | （空振り防止。FAIL が期待）許可タスク中は X も Slack も使える | PASS したら遮断しすぎで、上の 2 つの PASS が無意味 |
| `NoReopenX` / `NoReopenSlack` | （空振り防止。FAIL が期待）遮断した後でも、許可タスクを開始すれば次のポーリングで解けて使える。一時停止や Quit で解いた場合は数えない | PASS したら一度遮断すると戻らない設計で、上の PASS が無意味 |

X は遮断対象ドメイン（`x.com`・`bsky.app` など）の代表、Slack は止めるアプリの代表。上限の `GraceTicks` は tcc2 障害の猶予、`PauseMax` は短い非常口で、どちらも設計で決めた長さだけ見えてよい。

## 10 のガード（モデルの定数 `Guards`）

`formal/access-gate/guards.py` が、各ガードが無いことを示す「漏れの行」を file:line で出す。

| ガード | 種類 | 入れる場所 | 内容 | 外すと落ちる性質（反例の経路） |
|---|---|---|---|---|
| `failClosed` | code | `engine.RunCycleNow` の失敗分岐、`config/validate.go` の `failure_policy`、`MenuController.isHealthyStatus` | 猶予切れでも空のポリシーを出さず「許可タスクなし」として評価する。heartbeat も止めない | 両方（tcc2 が落ち続ける `TccDown`） |
| `pauseBounded` | code | `MenuBarApp`、`engine.beginPause`、`state.LoadPauseDay` | 一時停止は最長 `PauseMax`、1 日 1 回 | 両方（`UserPause`） |
| `noLoosening` | code | `engine.SetDryRun`、`LoadConfig` の検証エラー | 動いている間の緩める変更（メニューの dry_run・壊れた設定の再読込）で遮断を外さない | 両方（`UserDryRun`） |
| `keepLastConfig` | code | `RunCycleNow` の `e.cfg == nil`、`engine.New` / `LoadConfig` | 壊れた設定のまま起動しても、最後に有効だった設定で動く | 両方（`UserBreakConfig` の後に再起動） |
| `ownerKeepAlive` | 環境 | `~/Library/LaunchAgents` の plist（`KeepAlive`） | メニューバーアプリを Quit・クラッシュしても次のポーリングまでに戻る | 両方（`OwnerQuit`） |
| `persistTimers` | code | `state.Machine.firstFailure` を `failure.json` へ | 再起動しても失敗の起点を復元する | 両方（tcc2 が落ちたまま `OwnerQuit` と再起動を繰り返す） |
| `closeOpenTabs` | code | `background.js` の `redirectBlockedTabs`、`manifest.json` の `tabs` 権限 | 遮断を採用したとき、開いている対象ドメインのタブを閉じるか読み直す | X（遮断前に開いた X のタブが残る） |
| `privateBlocked` | code | `manifest.json` の `incognito`、`onBeforeRequest` の `details.incognito` | プライベートウィンドウでも遮断する | X（`tab = "private"`） |
| `extPinned` | 環境 | Firefox の `distribution/policies.json` | 拡張を常設し、再起動で外れないようにする | X（`FirefoxRestart`） |
| `otherBrowsersStopped` | config | deny ルールの `app.stop`（`com.apple.Safari`・`com.google.Chrome`） | 別ブラウザは遮断と同じ判定で即 quit。起動も LaunchWatcher で止める | X（`tab = "other"`） |

既定拒否そのものはコード変更なしで書ける（`Config = "deny"`）。`task_name_not_contains` だけのルールは検証を通り、タスク 0 件でも有効になる（`rules/evaluate.go` の `matchRule`。適合テスト `TestEngineFailClosedOnGraceExpiry` が実際に読み込ませて確かめる）。ただし設定だけでは上の 10 の穴が残る（`deny-config` 変種が FAIL）。

最初の版では全ガードを入れた設計案でも反例が出た。tcc2 が落ちている間に再起動するたび、`engine.New` が新しい `Machine` を作って失敗回数が 0 に戻り、猶予が永遠に切れない（モデルと `engine.New` を読んで見つけた。実機では未再現）。`persistTimers` はこの穴を塞ぐ。

無タスク計時（要望 (B)）は `IdleAfter` と `IdleOnFailure`（取得失敗の Tick を数えるか）で表す。`persistTimers` と `failClosed` があれば `hold`・`count` のどちらでも PASS する。`hold` は、`persistTimers` が無いと再起動を繰り返すだけで計時が永遠に進まない。

## 実行

### 1. 症状を実機で観察して再現する

Firefox ESR の隔離プロファイルで拡張の常設と遮断画面への切り替えを確かめられる。利用者の TaskChute と実アプリを使う受け入れ確認では、次の手順を使う。

1. メニューバーの TCC Local Connector が動いていて、`~/.config/tcc-local-connector/config.yml` が今の 12 パターンの設定であること。
2. 今の症状: TaskChute でタスクを何も開始せずに Firefox で `https://x.com` を開く → 開ける（ブロック画面にならない）。Slack も起動できる。これが直したい症状。
3. deny ルールと各ガードを実装した後: 同じ操作で Firefox は `blocked.html` に切り替わり、Slack は起動直後に終了する。許可タスクを開始すると X も Slack も使える。プライベートウィンドウ・Safari・Chrome・開いたままの X タブ・tcc2 を止めた状態（`tcc2 logout` かネットワーク断）・アプリの Quit と再起動でも、ポーリング数回以内に見えなくなる。
4. 実機の操作は利用者の Slack と Firefox を終了させる。利用者の画面で行うときは先に断る。

### 2. モデルを流す

```bash
formal/access-gate/run.sh                              # guards.py → 適合テスト → 32 個の TLC（約 20 秒）
python3 -B formal/access-gate/guards.py --require      # 実装を終えた後: 未実装が 1 つでもあれば終了コード 1
```

- 先頭の `guard ...` 行は、今のコードに残る漏れの行（目安）。モデルの結果とは独立。`log ...` 行は下の 4 を見る。
- `== 適合テスト` が落ちたら、モデルの「今のコード」の前提（下の対応表）からコードが動いた。下の「変更が意図したものなら」へ。
- TLC の各行は `<変種> <性質> <結果> (期待 <期待値>) ok|UNEXPECTED [経路 <印>]`。1 行でも UNEXPECTED なら終了コード 1。
  - `code` は `sns-gate` と全ガードを有効にした現行設計で、両方 PASS が期待。`deny-config` はガードなしで設定だけを切り替えた比較対象で、FAIL が期待。
  - `design-*`（ガード全部。猶予・一時停止・無タスク計時の長さと失敗時の扱いを変えた 4 通り）は全性質 PASS が期待。
  - `no-<ガード>` は、そのガードが守る性質だけが FAIL し、反例がそのガードの経路（`経路 <印>`）を通ることが期待。別の経路で落ちたら `FAIL(別経路)` で UNEXPECTED になる。
  - `reach` は FAIL が期待（許可タスク中は使えること、遮断の後も許可タスクの開始で解けることの確認）。
- tla2tools.jar v1.7.4 を `formal/access-gate/.tools/` に取得し、SHA-256 を照合する。Java 11 以上、python3、go、node が要る。
- 反例と TLC の出力は `formal/access-gate/out/<変種>-<性質>.txt`（git 管理外）。
- 結果（反例も PASS も）は必ず `/statechart` スキルで読む。反例は最短経路を手順として説明し、PASS は状態遷移図にする。`/statechart` には `formal/access-gate/AccessGate.tla` と、`out/` の該当変種と同じ定数の cfg を渡す。

### 3. 本物のバグか、モデルの誤りか

- `design-*` が FAIL: 反例を `/statechart` で手順にし、各ステップを下の対応表でコードに当てる。コードに同じ順序で起こせる手順なら設計の穴（例: 再起動で失敗回数が 0 に戻り猶予が切れない、を最初の版で見つけた）。直すのは設計で、性質や上限を緩めない。
- コードに無い操作がモデルにある（または逆）なら、モデルの誤り。対応表の行を直してから流し直す。
- 上限（`XBound`・`SlackBound`）は「猶予 + 非常口 + 1 回の反応遅れ」の設計値。反例を見ずに上限を足して PASS させない。

### 4. 黙って劣化する経路を確かめる

`guards.py` のログ欄で診断イベントの実装を確認する。

- 取得失敗の猶予切れは、設定に応じて `controls_released` または `fail_closed` を `backend.log` に記録する。同じ障害が続く間は、最初の猶予切れで記録する。
- 一時停止は `paused` に `accepted` または `rejected` を付けて `backend.log` に記録する。
- native host は無効なポリシーやハートビート期限切れで空のポリシーを送ったときに、理由付きの `policy_emptied` を標準エラーへ記録する。

## 対応表（モデル → コード）

モデル: `formal/access-gate/AccessGate.tla`。行番号はずれるので関数名で引く。

| モデル | コード |
|---|---|
| `RuleActive` | `rules.Evaluate` → `matchRule`。`current` は `task_name_contains`（今の 12 パターン）、`deny` は `task_name_not_contains` だけのルール |
| `Tick` | `engine.RunCycleNow` が一時停止の期限を確認してタスクを取得する。取得に失敗すると `recordFetchTransition` が失敗時刻を記録し、猶予切れの `fail_closed` では `Evaluate(cfg, nil)` を使う。取得に成功すると取得タスクで `Evaluate` と `BuildPlanWithLifecycle` を実行する。遮断は `applyBrowserPolicy` で反映する |
| `Eval` | 成功時の `BuildBrowserPolicy`（遮断）と `Plan.EnforceStopBundleIDs`（Slack の停止）。`dry` なら `publishBrowserPolicy(false, true, ...)` と `MenuController` の `plan.dryRun ? []` |
| `released` | `release_controls` 指定時の `Machine.Transition("grace_expired")` → `publishEmptyBrowserPolicy` と空の `plan` |
| `healthy` | `MenuController.isHealthyStatus`（active / fetching / degraded だけ heartbeat を続ける） |
| `extBlock` | native host `readEffectivePolicy`（ポリシーか heartbeat が 15 秒より古いと空）→ `background.js` `handleNativeMessage`。native host は 1 秒ごとなので Tick の中で同期する。取得失敗の猶予中もポリシーが古くならないのは、`engine.Run` が 5 秒ごとに `republishBrowserPolicy` するため |
| `Blocked` | `background.js` `onBeforeRequest`（通常・プライベートの `main_frame`）と `redirectBlockedTabs`（既存タブ）。`other` は deny ルールの `app.stop` と `LaunchWatcher.handleLaunch` |
| `UserLaunchSlack` / `StopActive` | `LaunchWatcher.update` と `handleLaunch`（起動直後に terminate）。毎周の `ensure` の `app.stop` は `MacPlanExecutor.stopApp` |
| `UserPause` / `UserResume` | `engine.Pause`（`publishEmptyBrowserPolicy` と空の plan）/ `engine.Resume`。`pause.json` と `pause-budget.json` は再起動後も有効 |
| `UserBreakConfig` / `UserFixConfig` | `engine.LoadConfig`（検証エラー時は有効な設定とポリシーを維持し、起動時は `last-good-config.yml` を読む）/ `engine.Reload` |
| `UserDryRun` | `engine.SetDryRun`（実行開始後は dry-run への緩和を拒否） |
| `OwnerQuit` / `OwnerStart` / `Start` | アプリ終了で `MenuController.quit` → `watcher.stop()`・`heartbeat.stop()`。LaunchAgent が再起動し、`engine.New` で失敗時刻と一時停止を復元して `LoadConfig` で設定を読む |
| `FirefoxRestart` / `ExtReload` | Firefox ESR の `ExtensionSettings` で拡張を常設し、通常・プライベートの両方で読み込む |

適合テスト（`formal/access-gate/conformance/`）がこの表の一部を実コードで確かめる。2026-11-22 に、`matchRule` の `task_name_not_contains` の判定、`RunCycleNow` の猶予切れの `publishEmptyBrowserPolicy`、`onBeforeRequest` の incognito の判定、`onBeforeRequest` の遮断判定を 1 つずつ壊し、対応するテストが名前どおり落ちることを確かめた。

- `conformance_test.go` の `TestRuleActiveMatchesModel`: `RuleActive` の 2 設定 × 4 タスク種別を見る。
- `conformance_test.go` の `TestEngineFailClosedOnGraceExpiry`: deny 設定の検証、`Eval`、取得失敗の猶予切れ後も遮断とアプリの終了対象が有効なことを確かめる。
- `background_conformance.test.js`: `Blocked("ff")`、プライベートでの遮断、既存タブの切り替え、切断と空ポリシーでの解除を確かめる。

## 範囲と前提（PASS が保証しないこと）

- 有限の範囲: `GraceTicks`・`PauseMax` は 1〜2、`IdleAfter` は 0〜1。X のタブは 1 つに抽象化している（見えている場所を 1 つ持つ）。
- 同じユーザー権限での意図的な迂回は対象外。`launchctl unload`、バイナリや拡張の削除、hosts の手書き、バックエンドだけの kill などがこれに当たる。`ownerKeepAlive` と `extPinned` は環境の前提なので、PASS はそれらが実際に効いていることに依存する。
- バックエンドだけが落ちてアプリが残る場合は入れていない。heartbeat は止まり（`MenuController.synchronize`）遮断は消えるが、`BackendClient` がバックエンドを起動し直すかはこのコードから確かめていない（未検証）。次に足す経路の候補。
- `publishBrowserPolicy` の書き込み失敗（`PublishEmpty` に落ちる）は入れていない。ディスクの故障に当たる。
- `extPinned` は、署名検査を無効にした Firefox ESR の隔離プロファイルで有効になったことを確認した。通常版の Firefox は今回の導入対象外。
- `terminate()` は常に成功するとしている。Slack が終了を拒んだときは `MacPlanExecutor` が quit_refused を返すだけで、モデルにはない。
- Windows 版（`windows/`）はモデルに入れていない。
- 遮断ドメインの一覧の漏れ（`x.com` だけ書いて `twitter.com` を忘れる）は設定の問題で、モデルの範囲外。

## 変更が意図したものなら

1. ガードを実装したら、`guards.py` のその行が「漏れの行なし」になることを確かめる。
2. 同じ変更で適合テストが落ちるので、モデルの対応する分岐とテストの期待を新しいコードに合わせる。各テストは実コードの入出力でガードの動作を確かめる。
3. 全ガードを実装したら、`run.sh` の `code` 変種の定数（`Config` と `Guards`）を実装に合わせ、期待を PASS に変える。
4. `run.sh` を流し、全行 ok を確かめる。

## 対象と同期

同期対象: **ea4d3e80e6a1736a8ed3aa8ad07067353a3fa0ac** と、`PLAN.md` 工程 01〜09 の作業ツリー差分。

対象ファイル（このコミットより後に変わっていたら、対応表とモデルを読み直す）:

- `internal/engine/engine.go`（`RunCycleNow`・`LoadConfig`・`Reload`・`Pause`・`Resume`・`SetDryRun`・`New`）
- `internal/rules/evaluate.go`・`plan.go`・`browser_policy.go`
- `internal/state/machine.go`・`pause.go`、`internal/config/validate.go`（`failure_policy`・`validateRule`）
- `cmd/tcc-firefox-native-host/main.go`（`readEffectivePolicy`・`pollPolicy`）
- `firefox-extension/background.js`・`manifest.json`
- `macos/Sources/TCCLocalConnector/LaunchWatcher.swift`・`MenuController.swift`・`MacPlanExecutor.swift`・`BrowserPolicyHeartbeat.swift`・`MenuBarApp.swift`
- `internal/protocol/server.go`（pause）

確認: `git -C <repo> diff --stat ea4d3e80e6a1736a8ed3aa8ad07067353a3fa0ac -- <上のファイル>`。

同期コミットを進めてよいのは、新しいコミットで対応表の各行をコードと読み合わせ、`run.sh` が全行 ok になったときだけ。
