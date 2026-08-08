# Process 203: RESEARCH.md 追記

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-203.md` を起動した際の自己完結ブリーフ。

- **背景**: `RESEARCH.md` は実現性調査の記録であり、既に確定した本文（§4.1〜§4.5, §17）を持つ。その後の実装作業（P01/P02 等の実測）で、調査時点では判明していなかった事実（`tcc2 --version` が存在しない、`in-progress count: 1` が調査者の集計値であり実際の出力文字列ではない、等）が判明した。これらは既存の結論を覆すものではなく、既存記述への**注記の追加**として反映する必要がある。
- **目的**: 実測で判明した6件の注記を、既存本文を一切書き換えずに**追記のみ**で `RESEARCH.md` へ反映する。
- **変更範囲**: `RESEARCH.md`（既存ファイルへの追記のみ。削除行 0）。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:
  本 Process は文書作成でありコード定数を直接参照しない。ただし追記内容が参照する定数として `IDBlockDelimiter`（`" [ID: "`）/ `InProgressLinePrefix`（`"- [In Progress] "`）/ `TaskChuteQueryDays`（2）が既存の ★ Constants に定義されている旨を、追記文中で言及する場合がある（数値そのものは PLAN-for-mac.md を正本として参照する）。

- **禁止事項**:
  - **既存本文の書き換え禁止**（本 Process 固有の最重要禁止事項）: `git diff --numstat RESEARCH.md | awk '{print $2}'` → 期待「削除行 0」を示す出力（追記のみで既存行を1行も変更・削除しない）
  - D-08（`in-progress count` への依存禁止）はコードに対する禁止であり、本 Process では逆に「`in-progress count` が実データに存在しないことを注記する」ことそのものが目的であるため、本文中に文字列 `in-progress count` が**注記として**出現することは許容される（コードへの依存ではなく事実の記録）

- **適用される横断方針（インライン展開）**:
  - **A3（unverified情報に基づく行動の禁止）/ A7（Candor）**: 「調査文書に書かれた出力例が実際の出力と異なることがある」という事実を隠さず記録する。既存の§4.2本文にある `in-progress count: 1` という記述は、実測では応答テキストに一度も出現しないことを注記で明示する。
  - **既存資産の温存**: RESEARCH.md は複数の Process（P02, P200-202等）から参照される基礎資料であり、既存の章立て・記述順序を変更すると参照元との整合性が崩れる。追記は各節の末尾へのみ行う。
  - **patch_only の徹底**: `disjoint_guarantee: false` は「既存ファイルへの変更」を意味するが、変更の種類を「追記のみ」に限定することで、他 Process（RESEARCH.md を参照するがそれ自体を変更しない全 Process）との実質的な衝突を回避する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`RESEARCH.md` の既存5箇所（§4.1, §4.2, §4.3, §4.5, §17）に、実装作業で判明した実測事実の注記を追記する。既存本文（見出し・段落・箇条書き）は一切変更・削除しない。追記は各該当節の末尾に新しい段落または注記ブロックとして挿入する。`git diff --numstat RESEARCH.md` の削除行数が 0 であることを機械照合する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `RESEARCH.md` | §4.1 末尾（既存行144-193付近の後） | `tcc2 --version` 不在の注記、CLI版解決方法（Cellar パス）の注記を追記 |
| `RESEARCH.md` | §4.2 末尾（既存行184-217付近の後） | `in-progress count: 1` が調査者の集計値であり実データに存在しない旨の注記、実応答フォーマットの実測追記 |
| `RESEARCH.md` | §4.3 末尾（既存行196-219付近の後） | `get_user` の markdown 太字形式・`Start of Day` 符号不定の実測追記 |
| `RESEARCH.md` | §4.5 末尾（既存行239-244付近の後） | `tcc2 status` の出力形式とメールアドレス除外義務の実測追記 |
| `RESEARCH.md` | §17 末尾（既存行1219付近の前） | macOS のみを対象としWindows/WSLは後続とする旨、`PLAN-for-mac.md`/`plan-for-mac/` 参照の追記 |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: RESEARCH.md
symbols:
  - name: section-4.1-annotation
    kind: document-appendix
    line_hint: "§4.1 末尾"
  - name: section-4.2-annotation
    kind: document-appendix
    line_hint: "§4.2 末尾"
  - name: section-4.3-annotation
    kind: document-appendix
    line_hint: "§4.3 末尾"
  - name: section-4.5-annotation
    kind: document-appendix
    line_hint: "§4.5 末尾"
  - name: section-17-annotation
    kind: document-appendix
    line_hint: "§17 末尾"
patch_only: true
disjoint_guarantee: false
pre_flight_checks:
  - git_clean
```

**Notes**:
- `disjoint_guarantee: false` は本 Process が既存ファイル（`RESEARCH.md`）への変更であることを示す。他の全 Process は `RESEARCH.md` を参照するのみで変更しないため、実質的な同時編集衝突は発生しないが、`patch_only: true` により「追記5箇所以外への変更」を宣言違反として扱う。
- 5つの `symbols` はいずれも既存ファイル内の挿入位置（節末尾）を指し、新規シンボル（関数・型）ではなく文書内の追記ブロックを表す。

## Verification Gates（P203）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P203-VG-01 | green | conformance | agent | true | `git diff --numstat RESEARCH.md \| awk '{print $2}'` | P203 task_delta | 出力が `0`（削除行 0 = 追記のみ） | GoalEvidence | failure_class:conformance, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-04 | SCOPE-01,DONT-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 203
- gate_ids: [P203-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: 実装作業で新たに実測事実が判明したとき（本 Process はその都度、追記のみで再実行される想定）
- appendix: process-203.appendix.md（実行時に Read しない）

## Implementation Notes
以下6項目を、既存本文の書き換えではなく注記の追加のみで反映する。
1. **§4.1 に注記**: `tcc2 --version` は存在しない（実測: `Error: unknown flag: --version`）。サブコマンドは completion / help / login / mcp / status のみ。CLI 版は `readlink -f /opt/homebrew/bin/tcc2` → `/opt/homebrew/Cellar/tcc2/0.0.21/bin/tcc2` の Cellar パスから解決する（Homebrew 以外の配置では取得不可。診断値であり制御入力ではない）。
2. **§4.2 に注記（最重要）**: `in-progress count: 1` は tcc2 の出力文字列ではなく調査者の集計値である。実測で応答テキスト全191行に `in-progress count:` で始まる行は1行も存在しない。この文字列に依存する実装は必ず失敗する。
3. **§4.2 に実応答フォーマットを追記**: `- [<Status>] <タスク名> (<HH:MM>[, Est: <dur>][, Act: <dur>]) [ID: <task_id>[, Section: <id>][, Project: <id>][, Mode: <id>][, Routine: <id>][, Tags: <…>]]`。状態タグ実測集合は Done / In Progress / Todo。ID の先頭はキー名を持たない位置引数で `task_` + 32桁小文字16進。見出しは `## <語> <語> YYYY-MM-DD` / `### <セクション名>` / `### HHMM-HHMM (HH:MM-HH:MM)` の3種。タスク名に全角括弧・鍵括弧を含む行、`]` の直後に意味のある空白がある行が実在する（左から `(` を探す実装や左トリムは壊れる）。`structuredContent` は存在せず `outputSchema` は null（テキスト解析以外の経路がない）。
4. **§4.3 に実測追記**: `get_user` は `- **<ラベル>:** <値>` の markdown 太字形式。`Timezone: Asia/Tokyo` / `Start of Day: -05:00:00` / `Default View ID: view_<hex32>`。`Start of Day` の符号解釈は実測から一意に決まらないため、2日範囲クエリ `[d-1, d]` で吸収する方針を採った。
5. **§4.5 に追記**: `tcc2 status` の出力形式は `Logged in as: <EMAIL>` / `Token expires at: YYYY-MM-DD HH:MM:SS` で exit=0。メールアドレスが取得できてしまうため、ログ・status・通知から除外する義務がある。
6. **§17 に追記**: 本 MVP は macOS のみを対象とし、Windows/WSL は後続とする。実装計画は `PLAN-for-mac.md` と `plan-for-mac/` を参照。

## Behavior Specification
対象外: 本 Process は文書作成でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（文書が満たすべき観測可能な条件）
- `git diff --numstat RESEARCH.md` の削除行数が 0（追記のみで既存行を変更・削除していない）。
- §4.1/§4.2/§4.3/§4.5/§17 それぞれに新しい注記段落が1つ以上追加されている。
- `in-progress count: 1` が調査者の集計値であり実データに存在しないという注記が§4.2に含まれる。
- 「メールアドレスをログ・status・通知から除外する義務」の記載が§4.5に含まれる。

### Left to Implementation（文章表現の自由度）
- 注記の文体（「【注記】」のようなラベルを付けるか、地の文に溶け込ませるか）。
- 追記段落の長さ・分割単位。

## Red Phase
- [x] `rg -c '調査者の集計値' RESEARCH.md` を実行し、該当注記がまだ存在しない（ヒット0件、または exit != 0）ことを確認する
- [x] `git diff --numstat RESEARCH.md | awk '{print $2}'` を追記前に実行し、出力が空（変更なし）であることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（追記前の存在確認は正式ゲート化していない）/ status / command_or_action: `rg -c '調査者の集計値' RESEARCH.md` / exit_code / expected: 0件または非ゼロ終了（未追記） / observed / attempt）

## Green Phase
- [x] Implementation Notes の6項目すべてを、既存本文の末尾への追記のみで反映する
- [x] `rg -c '調査者の集計値' RESEARCH.md` を実行し、1件以上ヒットすることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P203-VG-01 / status / command_or_action: `git diff --numstat RESEARCH.md \| awk '{print $2}'` / exit_code / expected: 出力が `0` / observed / attempt）

## Refactor Phase
- [x] `git diff --numstat RESEARCH.md | awk '{print $2}'` を再実行し、追記後も削除行数が0のままであることを確認する（自己レビューでの意図しない削除を検出する目的）
- [x] 6項目の注記がそれぞれ対応する節（§4.1/§4.2/§4.3/§4.5/§17）の末尾に配置されており、既存段落の途中に割り込んでいないことをレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P203-VG-01 / status / command_or_action: `git diff --numstat RESEARCH.md \| awk '{print $2}'` / exit_code / expected: 出力が `0` / observed / attempt）

## Manual Verification（最大3件）
1. **既存本文の無改変確認（executor: human）**: 操作=`git diff RESEARCH.md` を目視で確認し、`-` で始まる行（削除）が1行も存在しないことを確認する → 期待=削除行0行、追加行のみ → データ状態の確認方法=`git diff RESEARCH.md` の出力を目視

## Dependencies
- Requires: P102
- Blocks: P300
