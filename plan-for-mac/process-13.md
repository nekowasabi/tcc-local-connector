# Process 13: 台帳・PID 再利用テスト

## Implementation Brief（コピペ用）
> 別セッションで `/x @plan-for-mac/process-13.md` を起動した際の自己完結ブリーフ。

- **背景**: `internal/ledger`（P04）は `lstart`+`args` の複合照合で PID 同一性を検証し、PID 再利用時に無関係なプロセスへシグナルを送らないことを保証する。この保証が壊れると、ユーザーの Mac 上で無関係なプロセスが誤って終了させられる重大な事故になる。SC-06（PID 再利用状況で `process.stop` がシグナルを送らず台帳から削除し notify を出す）を専用のテスト強化 Process として固定する。
- **目的**: `internal/ledger/verify_test.go`, `internal/ledger/manage_test.go` に PID 再利用・台帳整合・重複起動防止のテストを追加する。
- **変更範囲**: `internal/ledger/verify_test.go`, `internal/ledger/manage_test.go` の拡張のみ。`internal/ledger/ledger.go` / `verify.go` / `manage.go` を含むプロダクションコードは一切変更しない。

- **ローカル定数（生成時転記。正本: PLAN-for-mac.md の ★ Constants）**:

| 定数名 | 値 | 単位 | この Process での用途 |
|---|---|---|---|
| `LedgerVersion` | 1 | - | `TestLoad_VersionMismatch_Discards` で不一致値を投入する基準 |
| `MinGraceSeconds` / `MaxGraceSeconds` | 1 / 120 | 秒 | `TestManagerStop_Refused_NoRetry` で grace_seconds 経過後も生存するケースの境界確認 |

- **禁止事項**:
  - D-01（強制終了禁止）: `rg -n 'forceTerminate\(|SIGKILL|signal\.SIGKILL|syscall\.SIGKILL|kill -9' internal/ledger` → 期待ヒット数 0（本 Process は SIGKILL 経路を持たないテストのみを追加する）
  - D-02（プロセス名だけによる終了禁止）: `rg -n '\bpkill\b|\bkillall\b' internal/ledger` → 期待ヒット数 0
  - D-09（マジックナンバー直書き禁止。ただし `*_test.go` は D-09 の検査対象外）: `rg -n '\b(60|180|30000|65536|1048576|86400|21600)\b' internal/ledger --glob '!*_test.go'` → 期待ヒット数 0
  - D-12（TODO/FIXME 残存禁止）: `rg -n 'TODO|FIXME' internal/ledger/verify_test.go internal/ledger/manage_test.go` → 期待ヒット数 0

- **適用される横断方針（インライン展開）**:
  - **behavior_scope**: 検証のみ。プロダクションコードの挙動を変更しない（`patch_only:true`）。`internal/ledger/ledger.go` / `verify.go` / `manage.go` に1行も差分を作らない。
  - **トレーサビリティ**: 対応する feature Process（P04）の全 `BEH-04-n` に少なくとも1テストが対応づくこと。対応表は各テストファイル冒頭のコメントに置く。
  - **security**: 実プロセスへ実際のシグナルを送信するテストを書かない。**注入可能な signaler インターフェース**の呼び出し回数のみで検証する。

- **出力順序**: 1) 実装（差分） 2) セルフレビュー 3) 修正 4) 再レビュー 5) 更新対象ドキュメントの確認 6) 品質ゲート実行

---
## Overview
`internal/ledger` は PID 再利用を防ぐための `VerifyEntry`（`lstart`+`args` 複合照合）と、SIGTERM のみの停止手順を実装する。本 Process はこの安全性を、実プロセスへの実シグナル送信という不安定かつ危険なテスト手法に頼らず、**注入可能な signaler の呼び出し回数**で検証する設計を固定する。`lstart` 不一致・`args` 不一致・プロセス消滅の各ケースで signaler の呼び出し回数が0であることを照合し、正常系では1回のみ呼ばれることを確認する。

## Affected Files
| ファイル | 行番号 | 変更内容 |
|---|---|---|
| `internal/ledger/verify_test.go` | 既存末尾に追記 | `VerifyEntry` の全分岐テスト（既存 BEH-04-01〜05 に加えた深掘りケース） |
| `internal/ledger/manage_test.go` | 既存末尾に追記 | `TestStop_PIDReused` を含む `Manager.Start`/`Stop`/`Reconcile` の全分岐テスト |

## Symbol Targets
```yaml
file: internal/ledger/verify_test.go
symbols:
  - name: TestVerifyEntry_PsExecFailure_ReturnsFalseErr
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
disjoint_guarantee_evidence: "internal/ledger/*_test.go のみを変更する。W05 内の他 Process は internal/ledger に触れない。"
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/ledger/verify.go:VerifyEntry"
---
file: internal/ledger/manage_test.go
symbols:
  - name: TestStop_PIDReused
    kind: func
    line_hint: bottom
  - name: TestStop_ArgsMismatch_NoSignal
    kind: func
    line_hint: bottom
  - name: TestStop_ProcessGone_NoSignal
    kind: func
    line_hint: bottom
  - name: TestManagerStop_NormalTermination_OneSignal
    kind: func
    line_hint: bottom
  - name: TestManagerStop_Refused_NoRetry
    kind: func
    line_hint: bottom
  - name: TestManagerReconcile_RemovesStaleEntries
    kind: func
    line_hint: bottom
  - name: TestManagerStart_AlreadyRunning_NoDuplicate
    kind: func
    line_hint: bottom
  - name: TestLoad_VersionMismatch_Discards
    kind: func
    line_hint: bottom
patch_only: true
disjoint_guarantee: true
pre_flight_checks:
  - git_clean
  - go_build_ok
  - "symbol_exists:internal/ledger/verify.go:VerifyEntry"
```

## Verification Gates

| gate_id | phase | type | executor | required | command | input_scope | pass_criteria | evidence_spec | failure_policy | criterion_refs | policy_refs |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P13-VG-01 | green | test | agent | true | `go test ./internal/ledger -race -count=1 -cover` | P13 task_delta | exit==0 かつ coverage >= 80.0% | GoalEvidence（exit_code・coverage 数値を逐語引用） | failure_class:test, retryable:true, retry_budget_source:task_retry_budget, terminal_action:await_input | SC-06 | TEST-GREEN, SCOPE-01, QUALITY-01 |

> 禁則: 観測不能な合格宣言を書かない。固定リトライ回数を書かない（`retry_budget_source: task_retry_budget`）。局所 gate_id を使わない。

### execution_handoff
- task_id: T-20260807-macos-menubar-connector
- process: 13
- gate_ids: [P13-VG-01]
- retry_budget_source: task_retry_budget
- evidence_format: GoalEvidence

## 生成情報
- source_constants: PLAN-for-mac.md の ★ Constants（`LedgerVersion`, `MinGraceSeconds`, `MaxGraceSeconds`）
- regeneration_required_when: P04 の `VerifyEntry`/`Manager` のシグネチャ・signaler の抽象インターフェース・Verification Gates のいずれかが変更されたとき
- appendix: process-13.appendix.md（実行時に Read しない）

## Implementation Notes
- **`TestStop_PIDReused`**: `lstart` 不一致のケースで `Manager.Stop` を呼び出し、注入した fake signaler の呼び出し回数が **0** であることを出力に含めて確認する。台帳から該当 Entry が削除されることもあわせて確認する。
  // Why: PID 再利用時に誤って無関係なプロセスへシグナルを送る事故を防ぐという、本アプリで最も重大な安全性要件のテスト。実プロセスへの実シグナル送信で検証すると CI 環境で不安定になり、誤って無関係なプロセスに影響する危険もあるため、signaler を抽象化して呼び出し回数だけを数える設計にする。
- **`TestStop_ArgsMismatch_NoSignal`**: `args` 不一致でも同様に signaler の呼び出し回数が0であることを確認する。
- **`TestStop_ProcessGone_NoSignal`**: プロセス消滅（`ps` が非0終了）のケースでも signaler の呼び出し回数が0であることを確認する。
- **`TestVerifyEntry_PsExecFailure_ReturnsFalseErr`**: `ps` の実行自体が失敗した場合、`VerifyEntry` が `(false, err)` を返し、「自分のものではない」側に倒すことを確認する（BEH-04-05 の深掘り）。
- **`TestManagerStop_NormalTermination_OneSignal`**: 正常系（`lstart`/`args` が両方一致）で `Manager.Stop` を呼び出すと signaler が **1回だけ** 呼ばれる（SIGTERM 相当）ことを確認する。
- **`TestManagerStop_Refused_NoRetry`**: grace_seconds 経過後も対象プロセスが生存しているケースで `StopResult{status:"refused"}` を返し、呼び出し側が自動的に再試行しないことを確認する（テスト内で `Manager.Stop` を1回だけ呼び、signaler の呼び出し回数が1回のまま増えないことで裏付ける）。
- **`TestManagerReconcile_RemovesStaleEntries`**: `VerifyEntry` が false になる Entry が起動時 Reconcile ですべて削除されることを確認する。
- **`TestManagerStart_AlreadyRunning_NoDuplicate`**: 同一 `process_id` が既に running（`VerifyEntry==true`）のとき、`Manager.Start` が新しいプロセスを起動しないことを確認する。
- **`TestLoad_VersionMismatch_Discards`**: `LedgerVersion` と異なる `version` の台帳ファイルを読み込ませたとき、`Load` がエラーを返さず空の `Ledger` を返す（台帳を破棄）ことを確認する。

## Behavior Specification
対象外: 本 Process は検証専用でありプロダクションコードの外部挙動を変更しない（behavior_scope: false、system_type: n/a）。

### Correctness Criteria（観測可能・固定する）
- `lstart` 不一致・`args` 不一致・プロセス消滅・`ps` 実行失敗のいずれのケースでも、注入した signaler の呼び出し回数は 0 である。
- `lstart` と `args` の両方が記録値と完全一致する場合にのみ、`Manager.Stop` は signaler を1回だけ呼び出す（SIGTERM 相当）。
- grace_seconds 経過後も対象プロセスが生存している場合、`StopResult{status:"refused"}` を返し、呼び出し側は自動的に再試行しない（signaler の呼び出し回数はそれ以上増えない）。
- `Manager.Reconcile` は `VerifyEntry` が false になった Entry をすべて台帳から削除する。
- `Manager.Start` は同一 `process_id` かつ `VerifyEntry==true` のとき新しいプロセスを起動しない。
- `LedgerVersion` と異なる `version` の台帳ファイルは破棄され、空の `Ledger` として扱われる。

### Left to Implementation（内部ヘルパ名・小さな関数分割・ローカル変数名のみ）
- signaler インターフェースの注入方法（テスト用のモック実装名）
- `ps` 実行を fake する方法（`exec.Command` の差し替えかインターフェース抽象化か。P04 の実装方針に従う）
- テーブル駆動テストの要素型名

## Red Phase
本 Process の Verification Gates には red フェーズ専用の gate_id は定義されていない。pre_state が P04 の green 状態であるため、テスト追加前に該当テスト名を `-run` しても「no tests to run」で exit 0 となり意図的な失敗を観測できない。Red Phase の完了は Green Phase の gate（P13-VG-01）と共有する。
- [x] `internal/ledger/verify_test.go` に `TestVerifyEntry_PsExecFailure_ReturnsFalseErr` を追加する
- [x] `internal/ledger/manage_test.go` に8個のテスト関数（`TestStop_PIDReused` を含む）を追加する

## Green Phase
- [x] `go test ./internal/ledger -race -count=1 -cover` を実行し、追加した全テストケースが PASS することを確認する
- [x] `TestStop_PIDReused` が実プロセスへ実シグナルを送らず、注入可能な signaler の呼び出し回数のみで `signals_sent=0` を検証していることを確認する
- [x] coverage が 80.0% 以上であることを確認する
- [x] ✅ **Phase Complete**（GoalEvidence: gate_id: P13-VG-01 / status / command_or_action: `go test ./internal/ledger -race -count=1 -cover` / exit_code / expected: 0 かつ coverage>=80.0% / observed / attempt）

## Refactor Phase
- [x] D-01（強制終了禁止）・D-02（プロセス名だけによる終了禁止）・D-09・D-12 の grep を実行し、禁止事項セクションに記載した期待ヒット数と一致することを確認する
- [x] `internal/ledger/ledger.go` / `verify.go` / `manage.go` に差分が生じていないことを `git diff --stat` で確認する
- [x] 専用の Verification Gate はここには定義しない。D-01/D-02/D-09/D-12 の grep 結果は P13-VG-01 の Green 判定に包含される。

## Manual Verification
対象外: 本 Process は自動テストで検証を完結するため人手による Manual Verification 項目はない（テスト帯: behavior_scope:false）。Verification Gates の executor は全件 agent であり、human executor は本 Process には存在しない。PID 再利用の実機再現は P04 の Manual Verification（human executor）が担う。

## Dependencies
- Requires: P04（`internal/ledger` の実装が green であること）
- Blocks: P16, P18（Wave Progress Map W05→W06 の順序により、W05 内の全 Process 完了が W06 の前提）
