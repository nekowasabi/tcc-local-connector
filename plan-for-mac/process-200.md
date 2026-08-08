# Process 200: README.md 新規

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-200.md` を起動した際の自己完結ブリーフ。

- **背景**: P01–P52・P100–P102 でバックエンド／フロントエンドの実装とその品質ゲートは揃うが、利用者向けの導入手順・設定手順・既知の制約・障害時対応をまとめた文書がまだ存在しない。`README.md` は本リポジトリに未作成であり、現状は `PLAN-for-mac.md`（内部向け実装計画）を読まないと `.app` を起動できない。
- **目的**: macOS MVP のビルド・配置・権限付与・段階投入・既知の制約・障害時 runbook を1ファイルに集約した `README.md` を新規作成する。未導入の linter（golangci-lint / swiftlint / swift-format）や存在しないツール（xcodebuild）を手順として記載しない。
- **変更範囲**: `README.md`（新規作成のみ。他ファイルは変更しない）。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `MinPollIntervalSeconds` / `DefaultPollIntervalSeconds` / `MaxPollIntervalSeconds` | 10 / 60 / 3600 | 秒 | 「ポーリング間隔について」節で既定値と下限を明記する |
| `DefaultFailureGraceSeconds` | 180 | 秒 | 「既知の制約」節で是正遅延の上限根拠として明記する |
| `ConfigForbiddenModeMask` | 0o022 | mask | 「設定の作り方」節で `config.yml` の推奨パーミッションの根拠として明記する |

- **禁止事項**:
  - D-15（未導入 linter の記載禁止）: `rg -n 'golangci-lint|swiftlint|swift-format' README.md` → 期待 0 件
  - D-14（`.xcodeproj`/`xcodebuild`/XCUITest の記載禁止）: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' README.md` → 期待 0 件
  - D-07（絶対パス直書き禁止）: `rg -n '/Users/takets|/opt/homebrew' README.md` → 期待 0 件（`<YOUR_PATH>` 等のプレースホルダ表記を使う）

- **適用される横断方針（インライン展開）**:
  - **security**: ad-hoc 署名（`codesign --sign -`）のみで公証はしない。Gatekeeper の「開発元を確認できません」警告が初回起動時に必ず出る前提で回避手順（右クリック→開く、またはシステム設定から許可）を明記する。
  - **PATH の罠**: `.app` から起動されたプロセスの PATH は launchd 由来で `/opt/homebrew/bin` を含まない。`task_source.executable` には検証済み絶対パスを書く必要があることを明記する（値自体は `<YOUR_PATH>` で示す。D-07 参照）。
  - **段階投入**: `dry_run: true` から始め、通知系→GUIアプリ制御→管理対象プロセス制御→コマンド実行→ログイン時起動の順に安全側から機能を有効化する運用を明記する。
  - **Candor（A7）**: 「`config.yml` を書き換えられる利用者は本アプリの権限で任意コードを実行できる」という性質を隠さずに明記する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`README.md` をリポジトリ直下に新規作成する。対象読者は「これから `.app` をビルドして自分の Mac に導入する利用者」であり、内部実装計画（`PLAN-for-mac.md`）の詳細には触れず、外部から観測可能な手順・制約・障害対応のみを記載する。実測ツールチェーン（go 1.26.1 darwin/arm64 / Swift 6.3 / Xcode 26.4 / staticcheck / plutil / codesign / screencapture / rg / git）以外のツールを手順に登場させない。未導入（golangci-lint / swiftlint / swift-format / xcodebuild）は一切記載しない。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `README.md` | 新規全体 | プロジェクト概要・前提環境・ビルド手順・設定の作り方・Gatekeeper 回避・ログイン時起動・段階投入・既知の制約・障害時 runbook・ポーリング間隔の10節 |

## Symbol Targets（YAML 風ブロック + Notes）
```yaml
file: README.md
symbols:
  - name: readme-document
    kind: document
    line_hint: top
patch_only: false
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
```

**Notes**:
- `README.md` は本計画で唯一の対象ファイルであり、他 Process（201/202/203/300）とファイルレベルで衝突しない。
- 本 Process はコードを一切変更しない。symbol_targets はファイル単位（`readme-document`）で表現する。

## Verification Gates（P200）
| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P200-VG-01 | green | conformance | agent | true | `test -f README.md && rg -c 'make-app-bundle.sh' README.md` | P200 task_delta | exit == 0 かつヒット >= 1 | GoalEvidence | failure_class:conformance, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01,SC-02,SC-03,SC-04,SC-05,SC-06,SC-07,SC-08,SC-09,SC-10 | SCOPE-01,QUALITY-01 |
| P200-VG-02 | green | grep | agent | true | `rg -n 'golangci-lint\|swiftlint\|swift-format\|xcodebuild' README.md docs/` | P200 task_delta | ヒット == 0 | GoalEvidence | failure_class:grep, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-01..SC-10 | DONT-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 200
- gate_ids: [P200-VG-01, P200-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants（ポーリング・猶予・パーミッション関連の値）・ビルド手順（`scripts/make-app-bundle.sh` の呼び出し順）・段階投入手順を変更したとき
- appendix: process-200.appendix.md（実行時に Read しない）

## Implementation Notes
以下10項目をこの順に `README.md` の節として記載する（見出し粒度は Left to Implementation）。
1. **プロジェクト概要**: TaskChute Cloud 2 の実行中タスクに応じて macOS のアプリ・プロセス状態を継続整合させるツールである旨を1〜2段落で説明する。
2. **前提環境**: macOS 26.5.1 以降 / Apple Silicon / Swift 6.3 / Xcode 26.4 / Go 1.26 / `tcc2` 導入済みかつログイン済み、を箇条書きで列挙する。
3. **ビルド手順**: `go build ./...` → `swift build --package-path macos -c release` → `bash scripts/make-app-bundle.sh` → `dist/TCCLocalConnector.app` を `/Applications` へ配置、の4ステップをコードブロックで示す。
   // Why: `xcodebuild` を使わず SwiftPM + シェルスクリプトのみで完結させる（D-14／未導入ツール回避）。
4. **設定の作り方**: `config.example.yml` を `~/.config/tcc-local-connector/config.yml` へコピーし mode 0600 に設定する手順、`task_source.executable` に `tcc2` の絶対パスを書く理由（PATH の罠）を明記する。値は `<YOUR_PATH>` で示す。
5. **ad-hoc 署名による Gatekeeper 警告の回避手順**: 初回起動時に警告が出る前提で、右クリック→開く、またはシステム設定からの許可手順を記載する。
6. **ログイン時起動の有効化手順**: 既定 OFF、メニューから明示的に ON にする操作、`SMAppService` を使うこと、ad-hoc 署名下で登録失敗しうる旨とその場合の表示（メニューに明示）を記載する。
7. **段階投入の推奨手順**: ①`dry_run: true` かつ `rules: []` で2〜3日 ②`dry_run: true` + ルール記述で1〜2日 ③`dry_run: false` で `notify`/`app.start` のみ ④`app.stop` 追加 ⑤`process.start`/`command.run` ⑥ログイン時起動を有効化、の6段階を番号付きリストで記載する。
8. **既知の制約**: 強制終了しない（拒否時は通知のみ）／ブラウザ URL 誘導は別コンポーネントで未対応／是正は最大 `DefaultFailureGraceSeconds`(180秒) 遅延だが禁止アプリの起動検出は3秒以内のイベント駆動／tmux・nvim 未対応／Windows 未対応／外部プロセスの一般的終了は不可／`config.yml` を書き換えられる利用者は本アプリの権限で任意コードを実行できる、を漏れなく列挙する。
9. **障害時 runbook（15〜30行）**: ①止めたい→メニュー>一時停止>明日まで、それでも止まらなければ Quit ②勝手に終了させられる→ログの `action_executed` で `rule_id` 特定→config.yml 修正→設定を再読込 ③現在タスクが取れない→診断で tcc2 版・最終取得時刻・エラーコード確認→`tcc2 status`→`tcc2 login` ④バックエンドが繰り返し落ちる→stderr 確認、版不一致なら同一タグで再ビルド ⑤完全に元に戻す→ログイン時起動 OFF→Quit→管理対象プロセスを手動確認・終了 ⑥ログと設定の場所、の6項目を簡潔に記載する。
10. **ポーリング間隔について**: 既定 `DefaultPollIntervalSeconds`(60秒)、`MinPollIntervalSeconds`(10秒) まで下げられるが TaskChute Cloud の利用条件は未確認のため既定を推奨する旨を記載する。

## Behavior Specification
対象外: 本 Process は文書作成でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（文書が満たすべき観測可能な条件）
- `README.md` が存在し、`make-app-bundle.sh` への言及が1件以上ある（ビルド手順が実在のスクリプトを指す）。
- `golangci-lint` / `swiftlint` / `swift-format` / `xcodebuild` のいずれの文字列も含まない。
- 「Implementation Notes」の10項目それぞれに対応する記載がある（項目の欠落がない）。
- 絶対パスの実値（`/Users/takets` や `/opt/homebrew` 等）を含まず、プレースホルダ表記を使う。
- 「`config.yml` を書き換えられる利用者は本アプリの権限で任意コードを実行できる」という性質の記載が1件以上ある。

### Left to Implementation（文章表現の自由度）
- 見出しレベル（`##` か `###` か）・箇条書きか番号付きリストかの選択。
- runbook の各項目の文言・語順。
- 前置きの分量・トーン。

## Red Phase
- [x] `test ! -f README.md` を実行し、`README.md` が未作成であることを確認する（文書帯における Red Phase = 文書が存在しないことの確認）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（README.md 作成前の存在確認は正式ゲート化していない）/ status / command_or_action: `test ! -f README.md` / exit_code / expected: 0（未作成） / observed / attempt）

## Green Phase
- [x] Implementation Notes の10項目すべてを満たす `README.md` を作成する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P200-VG-01 / status / command_or_action: `test -f README.md && rg -c 'make-app-bundle.sh' README.md` / exit_code / expected: 0 かつヒット >= 1 / observed / attempt）
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P200-VG-02 / status / command_or_action: `rg -n 'golangci-lint|swiftlint|swift-format|xcodebuild' README.md docs/` / exit_code / expected: ヒット 0 / observed / attempt）

## Refactor Phase
- [x] D-07（絶対パス直書き禁止）の grep（`rg -n '/Users/takets|/opt/homebrew' README.md`）が期待ヒット数（0件）と一致することを確認する
- [x] Implementation Notes の10項目とREADME.md の節見出しを突き合わせ、欠落がないことをレビューで確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: n/a（禁止事項節記載の grep を Refactor Phase の証跡として使う）/ status / command_or_action: `rg -n '/Users/takets|/opt/homebrew' README.md` / exit_code / expected: ヒット0件 / observed / attempt）

## Manual Verification（最大3件）
1. **新規チェックアウトからの手順追従（executor: human）**: 操作=クリーンな Mac（または新規ユーザーアカウント）で `README.md` の手順のみに従って `.app` をビルド・配置・起動する → 期待=手順の追加説明なしに `dist/TCCLocalConnector.app` が起動しメニューバーに常駐する → データ状態の確認方法=メニューバーアイコンの表示と `ps -ef | grep tcc-local-connector-backend` でのプロセス確認
2. **runbook の実効性確認（executor: human）**: 操作=README.md の「障害時 runbook」①〜⑥のいずれか1項目を実際に発生させて手順通りに対処する → 期待=記載手順のみで復旧できる → データ状態の確認方法=対処後にメニューバーの状態表示が正常に戻ること

## Dependencies
- Requires: P102
- Blocks: P300
