# High priority learnings

- `internal/state` の `Transition` 実装は、`grace_expired` の判定日時を `firstFailure` 基準で見るため、テスト時刻を固定しないと期待値がずれる。
- process マップの未完了タスクは、前提が既に満たされている場合でも「失敗確認」系の項目は赤字タスク扱いのまま残りやすい。
