---- MODULE LockNotice ----
\* Windows 版で、コネクタがアプリを終わらせたときの通知の数を数える。
\* 守る約束: アプリの 1 回の起動をコネクタが終わらせたら、利用者に出るロック通知はちょうど 1 回。
\* 1 刻み = LaunchWatchPollIntervalMilliseconds (2 秒)。アプリは 1 プロセスに抽象化する。
EXTENDS Integers

CONSTANTS
  Guards,     \* 設計のガードの集合 ⊆ {"execNotify","sharedNotify","watcherYields","goneReset"}
  Grace,      \* app.stop の grace_seconds を刻みに直した値 (既定 10 秒 = 5)
  Debounce,   \* goneReset の無い変種の抑止 (削除した LaunchLockNotifyDebounceMilliseconds = 10 秒 = 5)
  MaxT,       \* 探索する時刻の上限
  Runs,       \* プランが届いて StopApp が走る回数の上限
  Launches    \* 利用者がアプリを起動し直す回数の上限

VARIABLES
  app,     \* "none" | "win" (窓あり) | "tray" (窓なし常駐) | "closing" (終了中) | "gone"
  t,       \* 時刻
  ex,      \* WindowsPlanExecutor.StopApp: "idle" | "close" | "wait" | "notify"
  exStart, \* StopApp が始まった時刻 (grace の起点)
  exRuns,
  wp,      \* LaunchWatcher.Poll: "idle" | "due" | "act" | "notify"
  last,    \* LaunchWatcher._lastNotified[name] の時刻。-1 は記録なし
  seen,    \* アプリが消えた後、ウォッチャーが 0 件を一度見たか (人の起動し直しは 2 秒より遅い前提)
  notes,   \* この起動で出たロック通知の数
  launches

vars == <<app, t, ex, exStart, exRuns, wp, last, seen, notes, launches>>
Running(a) == a \in {"win", "tray", "closing"}

\* CloseMainWindow: 窓があれば WM_CLOSE を送って TRUE。アプリは終わるか、トレイに隠れるか、無視する
CloseResults == {"closing", "tray", "win"}

\* LaunchWatcher.NotifyIfDue。goneReset なら時間でなく「0 件を見たら解除」で 1 起動 1 回にする
Due == IF "goneReset" \in Guards THEN last = -1 ELSE (last = -1 \/ t - last >= Debounce)
NotifyIfDue == IF Due THEN last' = t /\ notes' = notes + 1 ELSE UNCHANGED <<last, notes>>

Init ==
  /\ app \in {"none", "win"}
  /\ t = 0 /\ ex = "idle" /\ exStart = 0 /\ exRuns = 0
  /\ wp = "idle" /\ last = -1 /\ seen = TRUE /\ notes = 0 /\ launches = 0

\* 利用者がアプリを起動する (止まっていて、ウォッチャーが 0 件を見た後)
Launch ==
  /\ app \in {"none", "gone"} /\ seen /\ launches < Launches
  /\ ex = "idle" /\ wp = "idle"
  /\ app' = "win" /\ notes' = 0 /\ launches' = launches + 1
  /\ UNCHANGED <<t, ex, exStart, exRuns, wp, last, seen>>

AppExit ==
  /\ app = "closing" /\ app' = "gone" /\ seen' = FALSE
  /\ UNCHANGED <<t, ex, exStart, exRuns, wp, last, notes, launches>>

\* --- WindowsPlanExecutor.StopApp (UI スレッド) ---
ExStart ==   \* Running(bundleId) が 0 件なら Skipped
  /\ ex = "idle" /\ exRuns < Runs /\ exRuns' = exRuns + 1
  /\ IF Running(app) THEN ex' = "close" /\ exStart' = t ELSE UNCHANGED <<ex, exStart>>
  /\ UNCHANGED <<app, t, wp, last, seen, notes, launches>>
ExClose ==   \* 各プロセスに CloseMainWindow (戻り値は見ない)
  /\ ex = "close" /\ ex' = "wait"
  /\ IF app = "win" THEN app' \in CloseResults ELSE UNCHANGED app
  /\ UNCHANGED <<t, exStart, exRuns, wp, last, seen, notes, launches>>
ExSeeGone == \* 250 ms ごとの IsRunning が false → Accepted
  /\ ex = "wait" /\ ~Running(app) /\ ex' = "notify"
  /\ UNCHANGED <<app, t, exStart, exRuns, wp, last, seen, notes, launches>>
ExKill ==    \* grace 切れで Kill → Accepted
  /\ ex = "wait" /\ Running(app) /\ t >= exStart + Grace
  /\ app' = "gone" /\ seen' = FALSE /\ ex' = "notify"
  /\ UNCHANGED <<t, exStart, exRuns, wp, last, notes, launches>>
ExNotify ==  \* 移植案 (Mac 2bcbde1): 実際に終わらせたときだけ通知
  /\ ex = "notify" /\ ex' = "idle"
  /\ IF "execNotify" \notin Guards THEN UNCHANGED <<last, notes>>
     ELSE IF "sharedNotify" \in Guards THEN NotifyIfDue
     ELSE notes' = notes + 1 /\ UNCHANGED last
  /\ UNCHANGED <<app, t, exStart, exRuns, wp, seen, launches>>

\* --- LaunchWatcher.Poll (タイマーのスレッド) ---
\* 時刻は、StopApp が反応し終えてから進む (250 ms ごとに見るので 2 秒の刻みを跨がない)
Tick ==
  /\ t < MaxT /\ wp = "idle" /\ ex \in {"idle", "wait"}
  /\ ~(ex = "wait" /\ (~Running(app) \/ t >= exStart + Grace))
  /\ t' = t + 1 /\ wp' = "due"
  /\ UNCHANGED <<app, ex, exStart, exRuns, last, seen, notes, launches>>
WSnap ==     \* _processes(name)。0 件なら次の名前へ
  /\ wp = "due"
  /\ IF "watcherYields" \in Guards /\ ex # "idle" THEN wp' = "idle" /\ UNCHANGED <<last, seen>>
     ELSE IF Running(app) THEN wp' = "act" /\ UNCHANGED <<last, seen>>
     ELSE /\ wp' = "idle" /\ seen' = TRUE
          /\ IF "goneReset" \in Guards THEN last' = -1 ELSE UNCHANGED last
  /\ UNCHANGED <<app, t, ex, exStart, exRuns, notes, launches>>
WAct ==      \* if (!CloseMainWindow()) Kill()。例外は握りつぶす
  /\ wp = "act" /\ wp' = "notify"
  /\ CASE app = "win"     -> app' \in CloseResults /\ UNCHANGED seen
       [] app = "tray"    -> app' = "gone" /\ seen' = FALSE
       [] app = "closing" -> app' \in {"closing", "gone"} /\ seen' = (app' = "closing" /\ seen)
       [] OTHER           -> UNCHANGED <<app, seen>>
  /\ UNCHANGED <<t, ex, exStart, exRuns, last, notes, launches>>
WNotify ==   \* 件数 > 0 を見た名前は、閉じられたかどうかに関係なく NotifyIfDue
  /\ wp = "notify" /\ wp' = "idle" /\ NotifyIfDue
  /\ UNCHANGED <<app, t, ex, exStart, exRuns, seen, launches>>

Next == Launch \/ AppExit \/ ExStart \/ ExClose \/ ExSeeGone \/ ExKill \/ ExNotify
        \/ Tick \/ WSnap \/ WAct \/ WNotify
Spec == Init /\ [][Next]_vars

TypeOK == app \in {"none", "win", "tray", "closing", "gone"} /\ notes \in 0..10

\* 約束 1: 1 回の起動で、ロック通知は 2 回以上出ない
NoDoubleNotice == notes <= 1
\* 約束 2: コネクタが終わらせて全員が手を止めたとき、通知は出ている (無言で消えない)
NoSilentStop == (app = "gone" /\ ex = "idle" /\ wp = "idle") => notes >= 1
\* 空振り防止 (FAIL が期待): アプリは実際に止まる / 通知は実際に出る
NeverStopped == app # "gone"
NeverNotified == notes = 0
====
