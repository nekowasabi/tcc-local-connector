# Process 101: Swift 6 厳格並行性とビルド警告ゼロ

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-101.md` を起動した際の自己完結ブリーフ。

- **背景**: P08/P09/P51 が実装する Swift 側コード（`ConnectorCore`/`TCCLocalConnector`）は Swift 6.3（実測）の strict concurrency が既定で有効な環境でビルドされる。`Sendable` 違反やデータ競合の可能性がある箇所はコンパイラ警告として検出されるが、警告を放置したまま各 Process の Verification Gates（自パッケージ内の `swift test`）だけを通しても、警告そのものの有無はチェックされない。
- **目的**: `swift build --package-path macos -c release` の警告数を0件にし、`swift test --package-path macos` を exit 0 で通す。`.xcodeproj`/`xcodebuild`/XCUITest を導入しないという Scope の対象外条件を維持したまま、SwiftPM のみで完結する検証を行う。
- **変更範囲**: なし（読み取り専用の検証 Process）。警告が見つかった場合の修正は当該コードを所有する Process（P08/P09/P51 のいずれか）の担当範囲に差し戻す。
- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

  | 定数名 | 値 | 単位 | この Process での用途 |
  |---|---|---|---|
  | （本 Process 固有の新規ローカル定数なし） | — | — | 既存の Swift 側 ★ Constants（`macos/Sources/ConnectorCore/Constants.swift`）を変更しない |

- **禁止事項**: 該当する Don'ts のみ抜粋。
  - D-14 `.xcodeproj`/`xcodebuild`/XCUITest の導入禁止 — `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' . --glob '!docs/**' --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **期待 0 件**（本 Process の検証コマンドも SwiftPM の `swift build`/`swift test` のみを使う）
  - D-15 未導入 linter の記載禁止 — `rg -n 'golangci-lint|swiftlint|swift-format' . --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` → **期待 0 件**（実測 F12 で swiftlint/swift-format は未導入）

- **適用される横断方針（インライン展開）**:
  - **品質ゲート**: `xcodebuild` は使用しない（pbxproj は差分レビュー不能という PLAN-for-mac.md の判断による）。SwiftPM の `swift build -c release`/`swift test` のみで検証を完結させる。
  - **Swift lint の扱い**: swiftlint/swift-format は実測 F12 で未導入のため、Swift 側の lint ゲートは「対象外」と明記する（下記 Behavior Specification 参照ではなく、本 Implementation Brief と Overview に明記）。警告ゼロの検証は Swift コンパイラ自身の診断（`warning:` の grep カウント）で代替する。
  - **修正責務の分離**: 警告が見つかった場合、修正は指摘箇所を所有する Process の Affected Files の範囲内で行う。本 Process 自体は「検証のみ」であり、ついでに無関係なコードを直すことはしない（surgical changes 原則）。
  - **該当 Don'ts**: D-14/D-15（上記）。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---

## Overview

本 Process は新規コードを書かない。P08/P09/P51 が実装した Swift 側コードベース全体（`macos/Sources/...`）に対して、SwiftPM の標準コマンドのみで静的な健全性を検証する読み取り専用の Process である。

Swift 6.3（実測）は strict concurrency checking が既定で有効であり、`Sendable` プロトコルに準拠しない型をアクター境界を越えて共有しようとする箇所や、`@MainActor` の境界違反はコンパイラが警告（将来的にはエラー）として検出する。`swift build --package-path macos -c release` を実行し、その標準出力・標準エラーに含まれる `warning:` の行数を数え、0件であることを確認する。これにより「コンパイラが検出可能な並行性の問題が1件も残っていない」ことを機械的に照合する。

`swift test --package-path macos` は P08/P09/P51 が追加したテスト一式を実行し、exit 0 を確認する。これは各 Process が個別に要求する Verification Gates と重複するが、本 Process ではリポジトリ全体を一度に実行することで、パッケージ横断の統合的な健全性（例えばテストターゲット間の命名衝突）も同時に確認する。

**Swift 側の lint ゲート（swiftlint/swift-format によるスタイル検査）は対象外**である。実測 F12 でこれらのツールがこの環境に導入されていないことが確認済みであり、存在しないコマンドを検証手順に書くと計画が実行不能になる（D-15 で機械照合）。スタイルの一貫性はコードレビュー（意図一致レビュー、FINAL-VG-04）に委ねる。

## Affected Files（パス・行番号・変更内容）

| パス | 種別 | 変更内容 |
|------|------|---------|
| （なし） | 読み取り専用 | 本 Process はファイルを変更しない。検証対象は `macos/Sources/...`/`macos/Tests/...` 配下の全 Swift ファイル（P08/P09/P51 が生成済み） |

## Symbol Targets

```yaml
# n/a（読み取り専用の検証 Process）。新規・変更対象のシンボルは存在しない。
```

### Notes
- `disjoint_guarantee: n/a`（読み取り専用のため他 Process との衝突が構造的に発生しない）。
- `frontend_scope: true`（本 Process は Swift/macOS 側のみを対象とし、Go 側は P100 が担当する）。

## Verification Gates（P101）

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P101-VG-01 | green | test | agent | true | `swift build --package-path macos -c release 2>&1 \| rg -c 'warning:'` | 全 task_delta（Swift 側） | ヒット == 0 | GoalEvidence（`rg -c` の出力数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08,SC-09 | QUALITY-01 |
| P101-VG-02 | green | test | agent | true | `swift test --package-path macos` | 全 task_delta（Swift 側） | exit == 0 | GoalEvidence（exit_code と最終行を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-07,SC-08,SC-09 | QUALITY-01 |

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 101
- gate_ids: [P101-VG-01, P101-VG-02]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants
- regeneration_required_when: ★ Constants・gate・横断方針を変更したとき
- appendix: process-101.appendix.md（実行時に Read しない）

## Implementation Notes

**なぜ `warning:` の grep カウントを合否条件にしたか（lint ツールを使わなかったか）**:
```
// Why: swiftlint/swift-format は実測 F12 で未導入であり、代替として導入すると
// D-15（未導入 linter の記載禁止）に抵触する新しい依存を持ち込むことになる。
// Swift 6 の strict concurrency はコンパイラ自身が Sendable 違反・data race の
// 可能性を warning として報告するため、追加ツールなしで「並行性の問題が0件」を
// 機械的に確認できる。rg -c によるカウントは exit code だけでは拾えない
// 警告メッセージの有無を数値として固定する。
```

**注意**: Swift 6 の strict concurrency は 6.3 環境で既定有効。`Sendable` 違反と data race をコンパイラに検出させる設計であり、本 Process はその検出結果（warning 出力）を集計するのみで、独自の並行性解析ロジックは実装しない。

## Behavior Specification
対象外: 静的性質（コンパイラ警告の有無・並行性検査の合否）の検証であり、`macos/Sources` の外部挙動（UI 表示・BackendClient の通信内容）を一切変更しないため。

## Red Phase: テスト作成と失敗確認
対象外: 本 Process は新規コードもテストも書かない読み取り専用の検証 Process であり、Red（意図的な失敗）を作る対象が存在しない。P08/P09/P51 の各 Process がそれぞれの Red Phase を既に完了している前提で本 Process は実行される。

## Green Phase: 最小実装と成功確認
- [x] `swift build --package-path macos -c release` を実行し、標準出力・標準エラーの `warning:` 出現数が0であることを確認
- [x] `swift test --package-path macos` を実行し exit 0 を確認
- [x] 警告が見つかった場合、指摘箇所を所有する Process の Affected Files を特定し差し戻す（本 Process では修正しない）

✅ **Phase Complete**（GoalEvidence）
- gate_id: P101-VG-01 / status: / command_or_action: `swift build --package-path macos -c release 2>&1 | rg -c 'warning:'` / exit_code: / expected: ヒット==0 / observed: / attempt:
- gate_id: P101-VG-02 / status: / command_or_action: `swift test --package-path macos` / exit_code: / expected: exit==0 / observed: / attempt:

## Refactor Phase: 品質改善
対象外: 本 Process はコードを変更しないため、リファクタリング対象が存在しない。D-14/D-15 grep の再実行のみが本フェーズに相当する。

✅ **Phase Complete**（GoalEvidence）
- gate_id: P101-VG-01 / status: / command_or_action: `rg -n 'xcodebuild|\.xcodeproj|XCUIApplication|XCUIElement' . --glob '!docs/**' --glob '!.git/**' --glob '!RESEARCH*.md' --glob '!PLAN-for-mac.md' --glob '!plan-for-mac/**'` / exit_code: / expected: ヒット==0 / observed: / attempt:

## Manual Verification
対象外: 自動テストで十分（`warning:` カウントと `swift test` の exit code のみで機械的に合否判定できるため、人間の確認を要する曖昧さが残らない）。

## Dependencies
- Requires: P09
- Blocks: なし
