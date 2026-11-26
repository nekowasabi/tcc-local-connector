---
name: verify-lock-notice
description: tcc-local-connector の Windows 版で「コネクタがアプリを終わらせたら、利用者に出るロック通知はその起動 1 回につきちょうど 1 回（無言で消えない・二重に出ない）」を everystate（TLC 互換の TLA+ モデル検査器）で全探索する。windows/ConnectorCore/LaunchWatcher.cs（Poll・NotifyIfDue・Update・ILockNotifier・LaunchLockNotification）、windows/TCCLocalConnector/WindowsPlanExecutor.cs の StopApp、TrayApplicationContext.ConsumeBackendMessages の watcher.Update と executor.Execute の順序、Constants.cs の LaunchWatchPollIntervalMilliseconds・LaunchLockNotifyDebounceMilliseconds を変えるときに使う。Mac 版のロック通知（2bcbde1・c6f8cdb）を Windows に移植するとき、「Windows でアプリが黙って消えた」「ロック通知が 2 回出た」を調べるときにも使う。
---

# Windows のロック通知はアプリの起動 1 回につき 1 回（lock-notice）の検証

## 何を保証するか

| 名前 | 利用者への約束（1 文） | 破れたときの害 |
|---|---|---|
| `NoSilentStop` | コネクタがアプリを終わらせ、StopApp もウォッチャーも手を止めた時点で、その起動についてのロック通知が 1 回以上出ている | アプリが理由も分からず消え、クラッシュと区別できない |
| `NoDoubleNotice` | アプリの 1 回の起動について、ロック通知は 2 回以上出ない | 同じ停止で通知が重なり、ルール名の違う通知が並ぶと何に止められたか分からなくなる |
| `NeverStopped` / `NeverNotified` | （空振りの防止。FAIL が期待）アプリは実際に止まり、通知は実際に出る | PASS したら、モデルが何もしておらず上の PASS に意味がない |

## 実行

検査は 2 段に分ける。

### 変更のたびに流す（10 秒ほど）

```bash
formal/lock-notice/run.sh              # 適合テスト（dotnet）→ everystate で 12 通りを全探索
formal/lock-notice/run.sh --no-dotnet  # dotnet が無い環境ではモデルだけ流す
```

- 必要なもの: `everystate`（brew。確かめた版は 0.4.1）と dotnet SDK（ConnectorCore.Tests は net8.0。Linux や WSL でも動く）。Java も tla2tools.jar も使わない。
- `== 適合テスト` が落ちたら、`LaunchWatcher` がモデルの前提から変わっている。下の「変更が意図したものなら」へ進む。
- 結果は 1 行に 1 件、`<変種> <性質> <結果> (期待 <期待値>) ok|UNEXPECTED` の形で出る。1 行でも UNEXPECTED があれば終了コードは 1 になる。結果は everystate の終了コード（0 は PASS、12 は FAIL）で判定する。それ以外の値は ERROR になる（構文エラーなど）。
  - `current`: 今の Windows のコード（4 つのガードを実装済み）。両方の性質で PASS が期待。
  - `port-direct`: Mac の 2bcbde1 をそのまま移植した形（StopApp が終わらせたら直接通知し、ウォッチャーは 10 秒の抑止）。採らなかった案の比較用で、両方の性質で FAIL が期待（「直したこと」3・4）。
  - `design`: 4 つのガードをすべて入れた設計。`current` と同じ定数で、両方の性質で PASS が期待。
  - `no-<ガード>`: `design` からガードを 1 つだけ外したもの。そのガードが守る性質で FAIL が期待。
  - `reach`: FAIL が期待（空振りの防止）。
- 反例と出力は `formal/lock-notice/out/<変種>-<性質>.txt` に残る（git の管理外）。
- 結果は反例も PASS も `/statechart` スキルで読む。反例は最短の経路を手順の文にし、PASS は状態遷移図にする。`/statechart` には `formal/lock-notice/LockNotice.tla` と、`run.sh` の該当行と同じ定数を渡す。

### PR の前だけに流す（実機の Windows）

1. **症状を実機で観察して再現する。** モデルや適合テストより先に行う。Windows で `make win-release` の成果物を起動する。config.yml には、実行中のタスク名に合うルールで `app.stop` に `notepad.exe` を入れたものを使う。メモ帳を開いてからタスクを開始し、バルーン通知の数を数える。
   - 期待: メモ帳が消え、ルール名（「ロック中: <ルールID>」）と説明の 1 行目とプロセス名のバルーンがちょうど 1 回出る。
   - メモ帳が消えてから 2〜9 秒で開き直すと、ウォッチャーが閉じ、バルーンがもう 1 回だけ出る。
   - ガードを入れる前のコードでは、前者で通知が出ないことがあり、後者では通知が出なかった（下の「直したこと」1・3）。
   - 利用者の画面で行うときは、メモ帳以外のアプリを止めないことを先に断る。
2. 上の操作を Slack（`slack.exe`。閉じるとトレイに隠れる）でも行う。

## 4 つのガード（モデルの定数 `Guards`）

| ガード | 内容 | 外すと落ちる性質（反例の経路） |
|---|---|---|
| `execNotify` | StopApp が実際に終わらせたとき（IsRunning が false になったとき、または Kill の後）に通知する。Mac の `MacPlanExecutor.stopApp` と同じ | `NoSilentStop`: StopApp の WM_CLOSE でアプリがすぐ終わると、ウォッチャーが見る前に消え、誰も通知しない |
| `sharedNotify` | StopApp の通知も `LaunchWatcher.NotifyIfDue` を通し、抑止の状態を 1 つにする | `NoDoubleNotice`: ウォッチャーが件数を数えた後に StopApp が始まり、両方が通知する |
| `watcherYields` | StopApp がその名前を処理している間、ウォッチャーはその名前を飛ばす | `NoDoubleNotice`: ウォッチャーが通知した後、0 件を見て抑止を解き、遅れて消えを見た StopApp がもう一度通知する |
| `goneReset` | 抑止を 10 秒の時間ではなく、ウォッチャーが 0 件を見たときに解く（起動 1 回につき 1 回） | `NoSilentStop`: 2〜9 秒後に起動し直したアプリを閉じても、10 秒の抑止で通知が出ない |

4 つのガードは `windows/ConnectorCore/LaunchWatcher.cs`（`Claim`・`Release`・`ReportStopped`・`NotifyOnce`・`Poll` の 0 件の分岐）と `windows/TCCLocalConnector/WindowsPlanExecutor.cs`（`StopApp`）に実装してある。

## 直したこと（fa8084d の時点で見つけ、その後の作業ツリーで直した。実機では未再現）

1. ガード実装前のコードの `NoSilentStop`: StopApp が WM_CLOSE を送り、アプリがすぐ終わる。StopApp は Accepted を返すが通知しない。ウォッチャーの次の Poll は 0 件を見るだけなので、通知は 0 回になる。
2. ガード実装前のコードの `NoDoubleNotice`: アプリが WM_CLOSE を無視して窓を残したまま（例: 確認ダイアログ）だと、`CloseMainWindow` が true を返すので Kill に進まない。ウォッチャーは 2 秒ごとに閉じようとし、10 秒ごとに通知を繰り返す。
3. `port-direct`の `NoSilentStop`: ウォッチャーが閉じて通知した 2 秒後に利用者が起動し直す。次の Poll で閉じるが、10 秒の抑止の中なので通知が出ない。
4. `port-direct`の `NoDoubleNotice`: StopApp の WM_CLOSE でアプリがトレイに隠れる。ウォッチャーが Kill して通知し、StopApp も消えを見て通知するので、2 回になる。

## 本物のバグか、モデルの誤りか

- 反例は `/statechart` で手順の文にし、各ステップを下の対応表でコードに当てる。コードで同じ順序に起こせるなら、設計の穴として報告する。性質や上限を緩めて PASS させない。
- コードに無い操作がモデルにある（または逆）なら、モデルの誤り。対応表の行とモデルを直してから流し直す。
- `Launch` は `seen`（ウォッチャーが 0 件を一度見た後）を前提にしている。人がアプリを起動し直すのは 2 秒より遅い、という仮定。これを外して出た反例は、この仮定の範囲外になる。

## 黙って劣化する経路（確認項目）

- `LaunchWatcher.Poll` で Kill が例外を投げたら、通知を出さずに `ILockNotifier.WarnNotStopped` を呼ぶ。`BalloonLockNotifier` はそれを `_recentWarning`（「<名前> を終了できません」）に入れ、メニューの「警告表示」に出す。テストは `LaunchWatcherTests.Poll_KillFails_WarnsInsteadOfNotifying`。
- `StopApp` が `quit_refused` を返したら、`TrayApplicationContext.ConsumeBackendMessages` が同じ警告を入れる。
- プロセスが数えた後に消えたとき（`InvalidOperationException`）は劣化ではないので、何も残さない。
- モデルは Kill が必ず成功するとしている。

## 対応表（モデル → コード）

行番号はずれるので、関数名で引く。

| モデル | コード |
|---|---|
| `app` の `win`・`tray`・`closing`・`gone` | `Process.GetProcessesByName` の件数と `MainWindowHandle`。`tray` は `CloseMainWindow` が false を返す状態 |
| `Tick`・`WSnap` | `LaunchWatcher.Update` が作る `System.Threading.Timer`（`LaunchWatchPollIntervalMilliseconds`）と、`Poll` の `_processes(name)` |
| `WAct` | `Poll` の `if (!process.CloseMainWindow()) process.Kill();` |
| `WNotify`・`NotifyIfDue`・`last` | `Poll` の `NotifyOnce(name)` と `_notified`（`goneReset`。10 秒の抑止 `LaunchLockNotifyDebounceMilliseconds` は削除済みで、`Debounce` は `port-direct` の比較用） |
| `WSnap` の 0 件の分岐 | `Poll` の `if (!_claimed.Contains(name)) _notified.Remove(name);`（所有の確認と解除を 1 つのロックで行う） |
| `watcherYields` | `Poll` の先頭で `_claimed` の名前を除くことと、`Claim` / `Release` |
| `ExStart`・`ExClose` | `WindowsPlanExecutor.StopApp` の `Claim` の後、`EndApp` の `Running` と `CloseMainWindow` |
| `ExSeeGone`・`ExKill` | `EndApp` の 250 ms ごとの `IsRunning`、grace 切れの `Kill` |
| `ExNotify` | `StopApp` の `if (outcome == ActionOutcome.Accepted) _watcher.ReportStopped(...)`（`sharedNotify`。`NotifyOnce` を通る） |
| 通知の文面 | `Update(PlanPayload)` が `app.stop` の `Title`・`Message` を名前ごとに持ち、`BalloonLockNotifier` が出す |
| Tick の条件（StopApp が反応してから時刻が進む） | `AppStopPollIntervalMilliseconds` = 250 ms が 2 秒より十分短いこと |
| 実行の順序 | `TrayApplicationContext.ConsumeBackendMessages` が `_watcher.Update` の後に UI スレッドで `_executor.Execute` を呼び、ウォッチャーはタイマーのスレッドで並行して動く |

適合テストは `windows/ConnectorCore.Tests/LockNoticeConformanceTests.cs` にある。`WAct` の 2 分岐、`goneReset`（0 件を見たときだけ解除）、`watcherYields`（所有中は飛ばし、数えている途中で所有が始まっても解除しない）、`sharedNotify`（StopApp の通知も 1 回に数える）を確かめる。`execNotify`（`StopApp` が `ReportStopped` を呼ぶこと）は `TCCLocalConnector` が WinForms で Linux から実行できないため、テストではなく対応表で読み合わせる。

2026-11-26 に、0 件の分岐の所有確認を外す変更と、`ReportStopped` が `NotifyOnce` を通らない変更をそれぞれ入れ、適合テストが落ちて `run.sh` が終了コード 1 になることを確かめた。

## 範囲と前提（PASS が保証しないこと）

- アプリは 1 プロセスに抽象化している。`slack.exe` のように複数のプロセスがあるアプリは、`goneReset` の「0 件」がすべてのプロセスが消えたときを指す、という前提になる。
- 定数は grace 10 秒、時刻は 12 刻み（24 秒）まで、StopApp は 2 回まで、起動し直しは 2 回まで。抑止 10 秒は `port-direct` などガードの無い変種だけが使う。
- Kill は必ず成功するとしている。利用者が自分でアプリを終了する操作と、`LaunchWatcher.Stop`（完全終了）はモデルに入れていない。
- 通知の文言（ルール名と説明）が正しいかは見ていない。数だけを見る。
- 一時停止の通知（cf6a5cd の移植）は対象外。

## 変更が意図したものなら

1. ガードを外したり変えたりしたら、`run.sh` の `current` の行の Guards を実装に合わせる。
2. `LaunchWatcher` の分岐を変えたら、適合テストとモデルの対応する分岐を一緒に直す。
3. `run.sh` を流し、全行が ok になることを確かめる。

## 対象と同期

同期コミット: **fa8084da1e6f1dc24bef40158bdf5da6126aef3f** と、その上の作業ツリー差分（`formal/lock-notice/`、適合テスト、4 つのガードの実装）。コミットしたら、対応表を読み合わせてから同期コミットをそのコミットに進める。

対象ファイル（同期コミットより後に変わっていたら、対応表とモデルを読み直す）:

- `windows/ConnectorCore/LaunchWatcher.cs`（`Update`・`Poll`・`NotifyIfDue`）
- `windows/ConnectorCore/Constants.cs`（`LaunchWatchPollIntervalMilliseconds`・`LaunchLockNotifyDebounceMilliseconds`・`AppStopPollIntervalMilliseconds`）
- `windows/TCCLocalConnector/WindowsPlanExecutor.cs`（`StopApp`）
- `windows/TCCLocalConnector/TrayApplicationContext.cs`（`ConsumeBackendMessages`）
- `windows/TCCLocalConnector/LaunchWatcher.cs`（`BalloonLockNotifier`）

確認: `git -C <repo> diff --stat fa8084da1e6f1dc24bef40158bdf5da6126aef3f -- <上のファイル>`

同期コミットを進めてよいのは、新しいコミットで対応表の各行をコードと読み合わせ、`run.sh` の全行が ok になったときだけ。
