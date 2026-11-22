---- MODULE AccessGate ----
\* 「許可タスクを宣言（TaskChute で開始）していない間、X と Slack を見続けられない」ことを検査する。
\* 1 回の Tick = engine.RunCycleNow 1 回（ポーリング間隔 1 つ分）。Tick の間に利用者と環境の操作が何回でも入る。
\* 対応するコードは .claude/skills/verify-access-gate/SKILL.md の対応表を見る。
EXTENDS Naturals

CONSTANTS
    Config,         \* "current" = 利用者の今の config.yml（12 パターンの間だけ遮断）/ "deny" = task_name_not_contains で許可タスク以外を遮断
    Guards,         \* 漏れの経路ごとの防ぎ（下の GuardNames）。今のコード ea4d3e8 には 1 つも無い
    GraceTicks,     \* polling.failure_grace_seconds をポーリング間隔で割った数
    PauseMax,       \* pauseBounded のときの一時停止の長さ（Tick 数）
    IdleAfter,      \* 許可タスク以外が何 Tick 続いたら Slack を止めるか（今のコードは 0 = 即時）
    IdleOnFailure   \* tcc2 取得失敗の Tick を無タスク計時に "count" するか "hold" するか

GuardNames == {"failClosed", "pauseBounded", "noLoosening", "ownerKeepAlive",
               "closeOpenTabs", "privateBlocked", "extPinned", "otherBrowsersStopped", "persistTimers", "keepLastConfig"}
TaskKinds == {"none", "allowed", "listed", "other"}
Tabs == {"none", "ff", "private", "other"}

XBound == GraceTicks + PauseMax + 1
SlackBound == GraceTicks + PauseMax + IdleAfter + 1
Cap == SlackBound + 1
Min(a, b) == IF a < b THEN a ELSE b
Has(g) == g \in Guards

VARIABLES
    task,       \* TaskChute で今実行中のタスクの種類（真実）
    tccUp,      \* tcc2 の取得が成功するか
    fails,      \* 連続した取得失敗の回数（state.Machine.failures / firstFailure）
    paused, pauseLeft, pauseUsed,
    cfgBroken,  \* config.yml が壊れている（検証エラー）
    cfgNil,     \* engine.cfg が nil（壊れた設定のまま起動した）
    dry,        \* safety.dry_run（メニューの切替 SetDryRun を含む）
    owner,      \* メニューバーアプリ（バックエンドの親・LaunchWatcher・heartbeat）が動いている
    policy,     \* browser-policy.json が x.com を enforce している
    stopSet,    \* 直近の plan の enforce_stop_bundle_ids に Slack が入っている
    idle,       \* 許可タスク以外が続いた Tick 数（IdleAfter で頭打ち）
    extLoaded,  \* Firefox に拡張が読み込まれている
    extBlock,   \* 拡張の blockedDomains に x.com がある
    tab,        \* X が見えている場所
    slack,      \* Slack が起動している
    xLeak,      \* 許可タスク以外で X が見えていた連続 Tick 数
    slackLeak,  \* 許可タスク以外で Slack が起動していた連続 Tick 数
    allowedUnblock, allowedRelease  \* 遮断中に許可タスクの取得が成功して x.com / Slack の止めが解けたことがある

vars == <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner,
          policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>

\* rules/evaluate.go matchRule。deny は task_name_not_contains だけのルール（タスク 0 件でも有効になる）
RuleActive(t) == IF Config = "current" THEN t = "listed" ELSE t # "allowed"
\* LaunchWatcher と ensure の app.stop はメニューバーアプリの中でしか動かない
StopActive == owner /\ stopSet
Blocked(w) == CASE w = "ff"      -> extLoaded /\ extBlock
                [] w = "private" -> extLoaded /\ extBlock /\ Has("privateBlocked")
                [] w = "other"   -> Has("otherBrowsersStopped") /\ owner /\ policy  \* 別ブラウザは (A) 側: 遮断と同じ判定で即 app.stop

TypeOK ==
    /\ Guards \subseteq GuardNames
    /\ task \in TaskKinds /\ tccUp \in BOOLEAN /\ fails \in 0..(GraceTicks + 1)
    /\ paused \in BOOLEAN /\ pauseLeft \in 0..PauseMax /\ pauseUsed \in BOOLEAN
    /\ cfgBroken \in BOOLEAN /\ cfgNil \in BOOLEAN /\ dry \in BOOLEAN /\ owner \in BOOLEAN
    /\ policy \in BOOLEAN /\ stopSet \in BOOLEAN /\ idle \in 0..IdleAfter
    /\ extLoaded \in BOOLEAN /\ extBlock \in BOOLEAN /\ tab \in Tabs /\ slack \in BOOLEAN
    /\ xLeak \in 0..Cap /\ slackLeak \in 0..Cap /\ allowedUnblock \in BOOLEAN /\ allowedRelease \in BOOLEAN

\* 性質: 宣言していない間に X / Slack を見続けられる長さに上限がある
NoEndlessX == xLeak <= XBound
NoEndlessSlack == slackLeak <= SlackBound
\* 空振り防止: 許可タスク中は見られる（この 2 つは FAIL するのが期待）
NeverXWhileAllowed == ~(task = "allowed" /\ tab # "none")
NeverSlackWhileAllowed == ~(task = "allowed" /\ slack)
\* 遮断した後でも、許可タスクを開始すれば次のポーリングで解けて使える（この 2 つも FAIL するのが期待）
NoReopenX == ~(allowedUnblock /\ task = "allowed" /\ tab # "none")
NoReopenSlack == ~(allowedRelease /\ task = "allowed" /\ slack)

Init ==
    /\ task \in TaskKinds /\ tccUp = TRUE /\ fails = 0
    /\ paused = FALSE /\ pauseLeft = 0 /\ pauseUsed = FALSE
    /\ cfgBroken = FALSE /\ cfgNil = FALSE /\ dry = FALSE /\ owner = TRUE
    /\ policy = FALSE /\ stopSet = FALSE /\ idle = 0
    /\ extLoaded = TRUE /\ extBlock = FALSE /\ tab = "none" /\ slack = FALSE
    /\ xLeak = 0 /\ slackLeak = 0 /\ allowedUnblock = FALSE /\ allowedRelease = FALSE

\* ---- 利用者と環境（Tick の間に何度でも） ----
UserSwitchTask == \E t \in TaskKinds : t # task /\ task' = t
    /\ UNCHANGED <<tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
TccDown == tccUp /\ tccUp' = FALSE
    /\ UNCHANGED <<task, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
TccUp == ~tccUp /\ tccUp' = TRUE
    /\ UNCHANGED <<task, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* background.js onBeforeRequest: main_frame の新しい読み込みだけを見る。private は manifest の incognito: not_allowed で拡張が動かない
UserOpenX == \E w \in {"ff", "private", "other"} : tab # w /\ ~Blocked(w) /\ tab' = w
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
UserCloseX == tab # "none" /\ tab' = "none"
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* LaunchWatcher.handleLaunch: enforce 中の bundle id は起動直後に terminate
UserLaunchSlack == ~slack /\ ~StopActive /\ slack' = TRUE
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, tab, xLeak, slackLeak, allowedUnblock, allowedRelease>>
UserQuitSlack == slack /\ slack' = FALSE
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, tab, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* engine.Pause: publishEmptyBrowserPolicy と空の plan。メニューは「明日の開始時刻まで」
UserPause == owner /\ ~paused /\ (Has("pauseBounded") => ~pauseUsed)
    /\ paused' = TRUE /\ pauseLeft' = PauseMax /\ pauseUsed' = TRUE
    /\ policy' = FALSE /\ stopSet' = FALSE /\ extBlock' = FALSE
    /\ UNCHANGED <<task, tccUp, fails, cfgBroken, cfgNil, dry, owner, idle, extLoaded, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
UserResume == owner /\ paused /\ paused' = FALSE /\ pauseLeft' = 0
    /\ UNCHANGED <<task, tccUp, fails, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* engine.LoadConfig の検証エラー: publishEmptyBrowserPolicy。e.cfg は前の設定のまま残る
UserBreakConfig == ~cfgBroken /\ cfgBroken' = TRUE
    /\ (IF owner /\ ~Has("noLoosening") THEN policy' = FALSE /\ extBlock' = FALSE ELSE UNCHANGED <<policy, extBlock>>)
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgNil, dry, owner, stopSet, idle, extLoaded, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* engine.Reload 成功: 設定を読み直し（dry_run はファイルの false に戻る）、すぐ 1 周する（ここでは次の Tick に任せる）
UserFixConfig == owner /\ (cfgBroken \/ cfgNil \/ dry) /\ cfgBroken' = FALSE /\ cfgNil' = FALSE /\ dry' = FALSE
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, owner, policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* engine.SetDryRun（メニューの dry-run 切替）
UserDryRun == owner /\ ~dry /\ ~Has("noLoosening") /\ dry' = TRUE
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, owner, policy, stopSet, idle, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* 起動直後（engine.New → LoadConfig は publishEmptyBrowserPolicy、Machine は失敗回数 0 から）。
\* persistTimers は無タスク計時と最初の失敗時刻（firstFailure）を再起動後も引き継ぐ
Start(i) == [policy |-> FALSE, stop |-> FALSE, idle |-> IF Has("persistTimers") THEN i ELSE 0]
\* Quit / クラッシュ: BrowserPolicyHeartbeat が消え、native host は空のポリシーを送る
OwnerQuit == owner /\ owner' = FALSE /\ extBlock' = FALSE
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, policy, stopSet, idle, extLoaded, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
OwnerStart == ~owner /\ owner' = TRUE
    /\ cfgNil' = (cfgBroken /\ ~Has("keepLastConfig")) /\ dry' = FALSE /\ fails' = (IF Has("persistTimers") THEN fails ELSE 0)
    /\ policy' = Start(idle).policy /\ stopSet' = Start(idle).stop /\ idle' = Start(idle).idle
    /\ UNCHANGED <<task, tccUp, paused, pauseLeft, pauseUsed, cfgBroken, extLoaded, extBlock, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
\* 一時的なアドオンは Firefox の再起動で外れる。セッション復元で通常タブの X は戻る
FirefoxRestart == extLoaded' = Has("extPinned") /\ extBlock' = FALSE
    /\ tab' = (IF tab = "private" THEN "none" ELSE tab)
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>
ExtReload == ~extLoaded /\ extLoaded' = TRUE /\ extBlock' = FALSE
    /\ UNCHANGED <<task, tccUp, fails, paused, pauseLeft, pauseUsed, cfgBroken, cfgNil, dry, owner, policy, stopSet, idle, tab, slack, xLeak, slackLeak, allowedUnblock, allowedRelease>>

\* ---- 1 回のポーリング ----
Eval(t, i) == \* 成功時: rules.Evaluate → BuildBrowserPolicy / BuildPlan.EnforceStopBundleIDs。dry_run は enforce しない
    LET active == RuleActive(t) /\ ~dry
        ni == IF t = "allowed" THEN 0 ELSE Min(i + 1, IdleAfter)
    IN [policy |-> active, stop |-> active /\ ni >= IdleAfter, idle |-> ni]

Tick ==
    LET up == owner \/ Has("ownerKeepAlive")
        restarted == ~owner /\ up
        nNil == IF restarted THEN cfgBroken /\ ~Has("keepLastConfig") ELSE cfgNil
        nDry == IF restarted THEN FALSE ELSE dry
        b == IF restarted THEN Start(idle) ELSE [policy |-> policy, stop |-> stopSet, idle |-> idle]
        bFails == IF restarted /\ ~Has("persistTimers") THEN 0 ELSE fails
        expire == paused /\ Has("pauseBounded") /\ pauseLeft <= 1
        nPaused == paused /\ ~expire
        nLeft == IF nPaused /\ Has("pauseBounded") THEN pauseLeft - 1 ELSE 0
        fetch == up /\ ~nPaused /\ ~nNil
        nFails == IF ~fetch THEN bFails ELSE IF tccUp THEN 0 ELSE Min(bFails + 1, GraceTicks + 1)
        released == fetch /\ ~tccUp /\ nFails > GraceTicks
        e == IF ~up THEN b
             ELSE IF nPaused \/ nNil THEN [policy |-> FALSE, stop |-> FALSE, idle |-> b.idle]
             ELSE IF tccUp THEN Eval(task, b.idle)
             ELSE IF released /\ Has("failClosed") THEN Eval("none", b.idle)
             ELSE IF released THEN [policy |-> FALSE, stop |-> FALSE, idle |-> b.idle]
             ELSE [policy |-> b.policy, stop |-> b.stop,
                   idle |-> IF IdleOnFailure = "count" THEN Min(b.idle + 1, IdleAfter) ELSE b.idle]
        \* MenuController.isHealthyStatus: released / paused / config_error では heartbeat を止める
        healthy == up /\ ~nPaused /\ ~nNil /\ (released => Has("failClosed"))
        nBlock == extLoaded /\ healthy /\ e.policy
        enforcing == up /\ e.stop
        t1 == IF up /\ e.policy /\ tab = "other" /\ Has("otherBrowsersStopped") THEN "none" ELSE tab
        t2 == IF nBlock /\ Has("closeOpenTabs") /\ (t1 = "ff" \/ (t1 = "private" /\ Has("privateBlocked")))
              THEN "none" ELSE t1
        nSlack == slack /\ ~enforcing
    IN /\ owner' = up /\ cfgNil' = nNil /\ dry' = nDry
       /\ paused' = nPaused /\ pauseLeft' = nLeft /\ fails' = nFails
       /\ policy' = e.policy /\ stopSet' = e.stop /\ idle' = e.idle
       /\ extBlock' = nBlock /\ tab' = t2 /\ slack' = nSlack
       /\ xLeak' = IF t2 # "none" /\ task # "allowed" THEN Min(xLeak + 1, Cap) ELSE 0
       /\ slackLeak' = IF nSlack /\ task # "allowed" THEN Min(slackLeak + 1, Cap) ELSE 0
       /\ allowedUnblock' = (allowedUnblock \/ (fetch /\ tccUp /\ task = "allowed" /\ extBlock /\ ~nBlock))
       /\ allowedRelease' = (allowedRelease \/ (fetch /\ tccUp /\ task = "allowed" /\ StopActive /\ ~enforcing))
       /\ UNCHANGED <<task, tccUp, pauseUsed, cfgBroken, extLoaded>>

Next == UserSwitchTask \/ TccDown \/ TccUp \/ UserOpenX \/ UserCloseX \/ UserLaunchSlack \/ UserQuitSlack
     \/ UserPause \/ UserResume \/ UserBreakConfig \/ UserFixConfig \/ UserDryRun
     \/ OwnerQuit \/ OwnerStart \/ FirefoxRestart \/ ExtReload \/ Tick

Spec == Init /\ [][Next]_vars
====
