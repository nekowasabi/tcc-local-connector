# Process 300: OODA 総括と再評価

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-300.md` を起動した際の自己完結ブリーフ。

- **背景**: RESEARCH.md §5.4 は「Goバックエンドが小さく保てず、単なる薄いラッパーになる場合は分離コストが上回るため、段階1でプロトタイプを作り再評価する」と明記している。P01–P52・P100–P102・P200–P203 が完了した現時点が、この再評価を実施すべき地点である。また RESEARCH.md §18 の段階的検証計画（段階1〜7）は調査時点の仮説であり、実装完了後にどの項目が達成・未達・対象外だったかを分類し記録する必要がある。
- **目的**: ①`scripts/loc-report.sh` で Go/Swift の非テスト LOC を実測し、プロセス分離継続 or 統合提案の判定を出力する。②`docs/macos-verification-<date>.md` に §18 段階1〜6 の全検証項目を「達成/未達/対象外(理由)」に分類し未分類0件にする（段階7はブラウザ拡張として対象外固定）。③得られた知見を4箇所（.serena/memories/, stigmergy/, Memory, byterover）へ永続化する。
- **変更範囲**: `scripts/loc-report.sh`（新規）, `docs/macos-verification-2026-08-07.md`（新規）。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| Go 側非テスト LOC 閾値 | 1200 | 行 | `scripts/loc-report.sh` の判定基準（★ Constants には未登録の本 Process 固有の閾値。ユーザー指示に基づく） |

本 Process 固有の閾値（1200行）は PLAN-for-mac.md の ★ Constants 表には未登録のため、`scripts/loc-report.sh` 内のコメントに閾値の由来（本 Process の仕様）を明記する。

- **禁止事項**:
  - D-15（未導入 linter の記載禁止）: `rg -n 'golangci-lint|swiftlint|swift-format' scripts/loc-report.sh docs/macos-verification-2026-08-07.md` → 期待 0 件
  - D-14（`.xcodeproj`/`xcodebuild` 等の記載禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' scripts/loc-report.sh docs/macos-verification-2026-08-07.md` → 期待 0 件
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' scripts/loc-report.sh` → 期待 0 件

- **適用される横断方針（インライン展開）**:
  - **観測可能な判定**: `scripts/loc-report.sh` は「Go 側の非テスト LOC が1200行未満、かつ Go 側に状態機械・ルール評価・解析のいずれもが存在しない」場合のみ統合を提案し、それ以外は分離継続と明記する。判定根拠（LOC実測値と機能有無の確認結果）を出力に含め、曖昧な自然言語判定にしない。
  - **未分類0件の原則**: §18 段階1〜6 の全検証項目を「達成/未達/対象外(理由)」のいずれかに分類し、分類漏れを許さない。対象外の場合は必ず理由を付記する（例: Windows/WSL関連項目は「対象外: 本MVPはmacOS専用でありスコープ外」）。
  - **知見の永続化は4箇所すべてに触れる**: byterover は `command -v brv` で存在確認し、存在しない場合はスキップ（エラーにしない）。4箇所それぞれについて「保存済(パス明記)」または「スキップ(理由明記)」を記録し、未記録0件にする。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
本 Process は実装 Process（P01–P52, P100–P102, P200–P203）がすべて完了した後に実行する総括・再評価 Process である。`scripts/loc-report.sh` は Go/Swift の非テスト LOC を計測するシェルスクリプトであり、`go_nontest_loc` と `swift_nontest_loc` を数値で出力し、1200行閾値と Go 側の機能有無（状態機械・ルール評価・解析）に基づいてプロセス分離の妥当性を判定する。`docs/macos-verification-2026-08-07.md` は RESEARCH.md §18 の段階1〜6（段階7はブラウザ拡張として対象外固定）の全検証項目を分類したチェックリストである。最後に、得られた知見を .serena/memories・stigmergy・Memory（CLAUDE.md）・byterover の4箇所へ永続化する Knowledge Phase を実行する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `scripts/loc-report.sh` | 新規全体 | Go/Swift非テストLOC計測、1200行閾値判定、統合提案/分離継続の出力 |
| `docs/macos-verification-2026-08-07.md` | 新規全体 | §18段階1〜6の全検証項目分類（達成/未達/対象外(理由)）、段階7の対象外明記、知見サマリ |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: scripts/loc-report.sh
symbols:
  - name: loc-report
    kind: script
    line_hint: top
patch_only: false
disjoint_guarantee: n/a
pre_flight_checks:
  - go_test_ok
  - swift_build_ok
---
file: docs/macos-verification-2026-08-07.md
symbols:
  - name: verification-checklist-document
    kind: document
    line_hint: top
patch_only: false
disjoint_guarantee: n/a
pre_flight_checks:
  - go_test_ok
  - swift_build_ok
```

**Notes**:
- `disjoint_guarantee: n/a` は本 Process が全 Process（P200-P203含む）完了後に実行される最終集約 Process であり、他 Process との並列実行を前提としないため。
- `pre_flight_checks` に `go_test_ok` / `swift_build_ok` を置くのは、LOC 計測や§18判定が「実装が緑である」前提の総括であり、赤い状態での総括は無意味なため。

## Verification Gates（P300）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P300-VG-01 | green | review | agent | true | `bash scripts/loc-report.sh` | 全 task_delta | 出力に `go_nontest_loc` と `swift_nontest_loc` が数値で含まれ、1200行閾値での判定（>= 1200で継続 / < 1200で統合提案）が明記される | GoalEvidence | failure_class:review, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01..SC-10 | SCOPE-01,QUALITY-01 |
| P300-VG-02 | green | review | agent | true | `docs/macos-verification-2026-08-07.md` の §18 段階1–6 チェックリストを確認 | 全 task_delta | 全項目が `達成`/`未達`/`対象外(理由)` のいずれかに分類され、未分類 == 0 | GoalEvidence | failure_class:review, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01..SC-10 | SCOPE-01,QUALITY-01 |
| P300-VG-03 | green | manual | agent | true | 4保存先（.serena/memories/, stigmergy/, Memory, byterover）それぞれの保存結果を確認 | 全 task_delta | 4 保存先それぞれについて「保存済(パス明記)」または「スキップ(理由明記)」が記録され、未記録 0 件 | GoalEvidence | failure_class:manual, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01..SC-10 | QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 300
- gate_ids: [P300-VG-01, P300-VG-02, P300-VG-03]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: §18 の検証項目一覧を変更したとき、または LOC 判定閾値（1200行）を変更したとき
- appendix: process-300.appendix.md（実行時に Read しない）

## Implementation Notes
### scripts/loc-report.sh の仕様
- Go 側と Swift 側の**非テスト** LOC を計測して出力する（`*_test.go` および `Tests/` 配下の Swift ファイルを除外する）。
- 出力に `go_nontest_loc` と `swift_nontest_loc` を数値で含めること。
- 判定基準: **Go 側の非テスト LOC が1200行未満、かつ Go 側に状態機械・ルール評価・解析のいずれもが存在しない**なら、プロセス分離のコストが共通化の利益を上回ったとみなし統合を提案する。それ以外は分離を継続する。判定結果を出力に明記する。
- 「状態機械・ルール評価・解析の存在」は `internal/state/`（状態機械）・`internal/rules/`（ルール評価）・`internal/tcc2/`（解析）ディレクトリの存在とファイル数で機械的に判定する（曖昧な自己申告にしない）。

### §18 段階1〜6 の達成状況の分類
- 段階1（Goバックエンド境界）: 設定解析・状態機械のOS非依存テスト、改行区切りJSON要求応答、版不一致拒否、不正JSON/空行/過大メッセージ/途中切断、応答順序入れ替わり、タイムアウト・取消、バックエンド異常終了と再起動上限、標準入力EOFでの正常終了、1万回状態更新でのリーク0、の各項目を分類する。
- 段階2（MCP取得）: MCP初期化、`tools/list`、`get_taskchute`、`[In Progress]`抽出、タスク切替検出、認証切れ、`tcc2`異常終了後の再接続、日付境界、複数実行中タスク、の各項目を分類する。
- 段階3（ルール評価）: `task_name_contains`、`task_name_not_contains`、優先順位、競合、`on_enter`一回実行、`on_exit`一回実行、`ensure`冪等性、設定再読込、不正設定拒否、の各項目を分類する。
- 段階4（GUIアプリ）: 未起動から起動、重複防止、通常終了、終了拒否、起動通知即時是正、同一バンドルID複数プロセス、の各項目を分類する（Windows実行ファイル識別・UAC権限差は「対象外: 本MVPはmacOS専用」）。
- 段階5（管理対象CLI）: 起動と終了、PID再利用対策、子プロセス、タイムアウト、重複起動防止、アプリ再起動後の状態復元、未保存状態を持つnvim、引数注入拒否、の各項目を分類する（`wsl.exe`関連・tmux未導入・Windows/WSLパス境界・WSL再接続は「対象外: 本MVPはmacOS専用でありWSLはスコープ外（RESEARCH-wsl.md参照）」）。
- 段階6（常駐UIと一時停止）: 現在タスク表示、最終取得時刻、15分・1時間一時停止、スリープ跨ぎ自動復帰、完全終了、ログイン時起動からの復帰、再起動ループ抑制、の各項目を分類する（Windows通知領域・Explorer再起動関連は「対象外: 本MVPはmacOS専用」）。
- 段階7（ブラウザ拡張）: 全項目を一括で「対象外: 別コンポーネント」に分類する（RESEARCH.md §17.2で決定済み）。
- 未分類0件を機械照合するため、`docs/macos-verification-2026-08-07.md` 内で各項目に `達成`/`未達`/`対象外(理由: ...)` のいずれかのラベルを付す形式にする。

## Knowledge Persistence（process-300 専用）
得られた知見を以下の4箇所に保存する。
1. **.serena/memories/** — `mcp__serena__write_memory` で保存。ファイル名は `lessons-{topic}` 形式。
2. **stigmergy/** — `stigmergy/lessons.jsonl` に追記、または `stigmergy/patterns/{category}.md` に記録。
3. **Memory** — 重要度が高い場合は `~/.claude/CLAUDE.md` またはプロジェクト固有の `CLAUDE.md` に追記。
4. **byterover** — `command -v brv` で存在確認し、存在する場合のみ `brv save` 等で保存。存在しない場合はスキップ（エラーにしない）。

保存すべき知見の候補:
1. 「調査文書に書かれた出力例が実際の出力と異なることがある（`in-progress count` の件）。外部インターフェースは必ず実測する」
2. 「エラーにならない失敗モード（静かな誤動作）をセンチネルで明示的失敗に変換する設計」
3. 「未検証事項を『結果がどちらでも実装が変わらない』形に吸収する計画技法」
4. 「PATH の罠: `.app` / `wsl.exe --exec` いずれも既定 PATH にユーザーのシェル PATH を含まない。外部実行ファイルは検証済み絶対パスを渡す」

## Behavior Specification
対象外: 本 Process は文書作成でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（文書・スクリプトが満たすべき観測可能な条件）
- `bash scripts/loc-report.sh` の出力に `go_nontest_loc` と `swift_nontest_loc` が数値として含まれる。
- 出力に1200行閾値での判定（統合提案 or 分離継続）が明記される。
- `docs/macos-verification-2026-08-07.md` の§18段階1〜6の全項目が `達成`/`未達`/`対象外(理由)` のいずれかに分類され、未分類0件。
- 段階7の全項目が「対象外: 別コンポーネント」に分類される。
- Knowledge Phase の4保存先それぞれについて「保存済(パス明記)」または「スキップ(理由明記)」が記録され、未記録0件。

### Left to Implementation（文章表現・実装の自由度）
- `scripts/loc-report.sh` の LOC 計測方法（`wc -l` を素朴に使うか `cloc` 相当のロジックを自作するか。実測ツールチェーンに `cloc` は含まれないため素朴な `wc -l` ベースの実装を推奨するが強制はしない）。
- `docs/macos-verification-2026-08-07.md` のチェックリスト表現形式（Markdown チェックボックスか表か）。
- 知見保存の文言・粒度。

## Red Phase
- [x] `test ! -f scripts/loc-report.sh` と `test ! -f docs/macos-verification-2026-08-07.md` を実行し、両ファイルが未作成であることを確認する（文書帯における Red Phase = 文書・スクリプトが存在しないことの確認）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（作成前の存在確認は正式ゲート化していない）/ status / command_or_action: `test ! -f scripts/loc-report.sh && test ! -f docs/macos-verification-2026-08-07.md` / exit_code / expected: 0（両方未作成） / observed / attempt）

## Green Phase
- [x] `scripts/loc-report.sh` を実装し、`go_nontest_loc`/`swift_nontest_loc`/閾値判定を出力させる
- [x] `docs/macos-verification-2026-08-07.md` を作成し、§18段階1〜6の全項目を分類する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P300-VG-01 / status / command_or_action: `bash scripts/loc-report.sh` / exit_code / expected: 0（`go_nontest_loc`/`swift_nontest_loc`が数値で出力され判定明記） / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P300-VG-02 / status / command_or_action: `docs/macos-verification-2026-08-07.md` の §18 段階1–6 チェックリストを確認 / exit_code: n/a / expected: 未分類0件 / observed / attempt）

## Refactor Phase
- [x] D-15/D-14/D-12 の grep が期待ヒット数（いずれも0件）と一致することを確認する
- [x] §18段階1〜6の分類とRESEARCH.md §18の原文を突き合わせ、項目の取りこぼしがないことをレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（禁止事項節記載の grep を Refactor Phase の証跡として使う）/ status / command_or_action: `rg -n 'TODO|FIXME|XXX|TBD|未定|後で決める|要検討' scripts/loc-report.sh` / exit_code / expected: ヒット0件 / observed / attempt）

## Knowledge Phase: 知見の永続化
- [x] .serena/memories/ に教訓を保存（mcp__serena__write_memory）
- [x] stigmergy/ に教訓・パターンを記録
- [x] 重要度が高い知見を Memory（CLAUDE.md）に追記
- [x] `command -v brv` を確認し、利用可能なら byterover に保存（`brv` が利用可能であり、`brv curate` でコンテキスト保存を実行）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P300-VG-03 / status / command_or_action: `command -v brv && brv curate "Progress map partial completion verification artifacts" -f PLAN-for-mac.md -f plan-for-mac/process-07.md -f internal/protocol/server.go` / exit_code / expected: 0 / observed: 0 / attempt

## Manual Verification（最大3件）
1. **LOC判定の妥当性確認（executor: human）**: 操作=`bash scripts/loc-report.sh` の出力する `go_nontest_loc` を `find internal -name '*.go' -not -name '*_test.go' | xargs wc -l` 等で独立に再計算し照合する → 期待=一致 → データ状態の確認方法=数値の目視比較
2. **§18分類の網羅性確認（executor: human）**: 操作=RESEARCH.md §18 の原文の検証項目箇条書きと `docs/macos-verification-2026-08-07.md` の分類項目数を突き合わせる → 期待=段階1〜7の全項目が1件も欠落なく対応する → データ状態の確認方法=項目数の目視カウント

## Dependencies
- Requires: P200, P201, P202, P203
- Blocks: なし
