# `stop.on_task_end` 要件定義

## 1. 背景・問題

`default.on_task_start` は新規 `task_id` の検出時に1サイクル1回だけ先行フェーズを計画する。終了時に一度だけ行う共通アクションは、通常 `rules` の `on_exit` に寄せるとタスク名マッチと優先度の影響を受けるため、開始時と同じくルール非依存の終了フェーズが必要である。

## 2. 目的・成功条件

- トップレベル任意設定 `stop` に、通常ルールから独立した後続フェーズを追加する。
- `stop.on_task_end` を、成功したタスク観測における消失 `task_id` の検出時に、1サイクル1回だけ実行する。
- 既存 `rules` の評価・priority・競合解決・protocol v1を維持する。
- stopアクションの失敗が通常ルールの実行を妨げない。dispatch順は default → rules → stop とする。

## 3. 設定スキーマ

トップレベル `stop` は任意であり、未定義は空の `on_task_end` と同義とする。`version: 2` を維持する。

```yaml
stop:
  on_task_end:
    - type: notify
      title: "タスク終了"
      message: "実行中タスクが終了しました"
      level: info
```

制約は `default.on_task_start` と同一とする。`browser.block` は拒否する。最大20アクション。YAML記載順に計画し、通常rulesとのpriority比較・dedupe対象にしない。

## 4. タスク終了判定

- `EndedTaskIDs = PreviousTaskIDs - CurrentTaskIDs`
- 1件以上なら当該サイクルでstopを1回だけ起動する。タスク数分に増幅しない。
- 未初期化時の終了差分は空とし、初回成功ポーリングではstopを起動しない。
- 順序・名前・メタデータのみの変化は終了とみなさない。
- 有効task_idの欠損または重複IDがあるサイクルは比較不能とし、stopを起動せず比較状態も更新しない。通常rules評価は継続する。
- 取得失敗、pause、grace/released、不正reloadでは比較状態を保持し、stopを起動しない。

## 5. 計画・dispatch

- 論理順は (1)取得成功 (2)開始/終了差分 (3)default計画/dispatch (4)通常rules評価/dispatch (5)stop計画/dispatch (6)結果記録。
- ActionIDはdefault→rules→stopの連番でサイクル内一意。
- stop内の1アクション失敗時、残りのstopアクションを続行する。
- dry-runでは計画・ActionID付与・比較集合更新を行い、副作用を発生させず `skipped` とする。同一タスク終了を dry-run 後に有効化しても遅延実行しない。
