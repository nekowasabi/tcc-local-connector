# PLAN: 宣言（SNS タスクの開始）しないと X と Slack を使えないようにする

## 1. 目的

今の tcc-local-connector は「指定した 12 パターンのタスク中だけ遮断する」加点方式で、それ以外の時間は X や Slack を自由に使える。利用者はタスク名に `SNS /` を含むタスクを TaskChute で開始（宣言）したときだけ、SNS と Slack を使えるようにしたい。最悪の状態は「設定しているのに漏れて X を無限に見続ける」こと。

`.claude/skills/verify-access-gate`（TLA+ モデル `formal/access-gate/AccessGate.tla`）が、設定だけで deny にしても漏れる経路が 10 あることを示した。各経路を塞ぐ 10 のガードを全部入れれば `NoEndlessX` と `NoEndlessSlack` が PASS する。利用者は 2026-11-22 に全ガードの採用を決めた。

## 2. 対象外

- 無タスク計時（「タスク未実施が一定時間で終了」）の別実装。deny ルールで、許可タスク以外は即座に遮断・終了するので置き換わる（モデルの `IdleAfter = 0`）。
- hosts の書き換え、Screen Time や MDM による OS 側の制限。
- 同じユーザー権限での意図的な迂回（`launchctl bootout`、バイナリや拡張の削除、config.yml を直接書いて再起動する前に last-good を消すなど）。
- Windows 版（`windows/`）。
- バックエンドだけが落ちた場合の再起動（モデルの範囲外。次の検証対象の候補）。

## 3. 制約

- macOS 26 / Swift 6.3 / Go 1.26 / Firefox ESR（README の前提）。
- 既存の 12 パターンのルール `default-tasks` の意味は変えない。
- 強制終了はしない（README の約束）。アプリの停止は今の `app.stop`（quit）と `LaunchWatcher` のまま。
- 状態ファイルは `~/.local/state/tcc-local-connector/` に 0600・atomic rename（`pause.json` と同じ書き方）。
- 各工程の終わりに `formal/access-gate/run.sh` が終了コード 0 であること。工程でコードの前提が変わったら、同じ工程で適合テスト（`formal/access-gate/conformance/`）の期待を直す。
- `go test -race ./internal/engine` の既存のデータ競合（`TestEngine_ReloadAndResumeRestoreEnsureWithoutWaitingInterval`）と `scripts/forbidden-audit.sh` の既存の超過は、この計画で直さない。verify は `-race` なしの `go test` を使う。

## 4. 検討した代替案と採用理由

- Why: 新しいキー `allow_task_name_contains` を足す案ではなく、既存の `task_name_not_contains` だけのルールを採用。理由: 今の `matchRule` と検証がそのまま受け付け、適合テストで確かめ済みで、コードの変更が要らない。
- Why: 猶予切れで制御を解く今の動き（`release_controls`）を残す案ではなく、`fail_closed` を既定にする案を採用。理由: tcc2 のログアウトだけで無限に見られる（`no-failClosed` の反例）。
- Why: 起動直後を「許可タスクなし」で始める案（startDeny）ではなく、失敗の起点の永続化（persistTimers）を採用。理由: モデルで persistTimers と failClosed があれば startDeny なしで PASS し、部品が 1 つ減る。
- Why: 一時停止を全廃する案ではなく、1 日 1 回・30 分を採用。理由: tcc2 障害時や誤遮断のときにローカルで抜ける道を残す（ateam で合意した安全弁）。

- 一時停止はバックエンドが受理した状態を確認してから所有者のハートビートを止める。拒否やアクション結果の応答で Firefox の遮断が一時的に外れるのを防ぐため、状態取得の応答で接続の健全性を判定する。

- dry-run への切替制限は、実行サイクルを開始したエンジンに適用する。単発の診断コマンド `run-once` は最初のサイクル前に dry-run を選んでアクションの実行を防ぐ。

- 最後に有効だった設定には、検証したファイル記述子から読んだバイト列をそのまま保存する。別の設定を指定できる単発コマンドでは、この保存と復元を行わない。起動時の復元は設定エラーを報告しつつ即時に適用する。

- LaunchAgent が起動した直後にバックエンドを起動する。実機ではメニュー表示時の処理に任せると、アプリ本体だけが常駐し、バックエンドとハートビートが動かなかった。
- 状態取得は前回の応答を処理してから次の要求を出す。実機で、要求の直後に未着の応答を読みに行くとハートビートが消え、Firefox 側で有効なハートビートが無いと判定される状態になった。
- LaunchAgent の再登録は、直前の登録解除が終わるまで最大5秒間再試行する。実機では `bootout` が正常終了した直後の `bootstrap` が入出力エラーになることがあった。
- 通常版 Firefox が入っていたため、Mozilla の署名を確認した Firefox ESR を別のアプリとして配置した。拡張のポリシーは ESR に設定し、既存のポリシーがあれば追加対象の設定だけを更新する。
- 工程 07 の指定どおり ESR アプリ内の `distribution/policies.json` を使う。追加後の `codesign --verify --deep --strict` はアプリ内に認証対象外のファイルが入ったため失敗した。LaunchServices 経由の起動と拡張の読み込みは実機で確認した。 [Mozilla の macOS ポリシー設定ガイド](https://firefox-admin-docs.mozilla.org/guides/policies-configuration/)

- `sns-gate` は独立したルールとして追加する。許可タスク名が既存ルールにも一致した場合は、既存ルールの遮断も適用される。実機確認のタスク名には `SNS / X確認` を用いる。

- エンジンは一連の取得障害につき猶予切れを1回記録する。native host は、障害を理由とする空ポリシーを送信した時点で診断を1行出す。空の状態が続く間は、同じポリシーの送信とログ出力を繰り返さない。同期の記録はベースのコミットと検証した作業差分を併記する。

## 5. 工程一覧

| 番号 | 題 | 依存 | データ形状 | 検証面 |
|---|---|---|---|---|
| 01 | failClosed | なし | `polling.failure_policy: fail_closed \| release_controls`（既定 fail_closed） | lib |
| 02 | persistTimers | 01 | `failure.json`: `{version int, first_failure RFC3339}` | lib |
| 03 | pauseBounded | 02 | `constants.PauseMaxSeconds = 1800`、`pause-budget.json`: `{version int, last_pause_day "YYYY-MM-DD"}` | lib / ui（メニュー） |
| 04 | noLoosening | 03 | なし | lib |
| 05 | keepLastConfig | 04 | `last-good-config.yml`（config.yml の写し）、定数 `LastGoodConfig` | lib |
| 06 | closeOpenTabs・privateBlocked | 05 | manifest: `permissions += "tabs"`、`incognito: "spanning"` | lib（node テスト） |
| 07 | ownerKeepAlive・extPinned | 06 | LaunchAgent plist、Firefox `distribution/policies.json` の `ExtensionSettings` | cli |
| 08 | denyConfig・otherBrowsersStopped | 07 | config.yml に `sns-gate` ルール | cli（`config-test`） |
| 09 | 残りのログとモデルの更新 | 08 | なし | cli（`run.sh`） |

## 6. 終了条件

次がすべて終了コード 0:

```sh
python3 -B formal/access-gate/guards.py --root . --require
formal/access-gate/run.sh
go test ./internal/... ./cmd/... ./formal/... -count=1
node --test firefox-extension/background.test.js formal/access-gate/conformance/background_conformance.test.js
bash scripts/verify-swift.sh
```

加えて人間が実機で確かめる（`make dev` で入れ直した後。手順は `.claude/skills/verify-access-gate/SKILL.md` の「1. 症状を実機で観察して再現する」）: タスクなしで Firefox の `https://x.com` が `blocked.html` になり、Slack・Safari・Chrome は起動直後に終了する。`SNS / X確認` を開始すると、次のポーリング（10 秒以内）で使えるようになる。

## 7. 適用スキル

- `verify-access-gate`（各工程の後に `run.sh`。結果は `/statechart` で読む）
- `tcc-local-connector-config`（工程 08 の config.yml 編集）
- `systematic-debugging`（適合テストや TLC が想定外に落ちたとき）
