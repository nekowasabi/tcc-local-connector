# P01: 設定・ドメイン正規化・ルール集合

## Implementation Brief

既存の YAML 設定と `config.Config` を拡張し、`ensure` 専用の `browser.block` アクションから、正規化済み・重複なし・辞書順のドメイン集合を生成できるようにする。既存の NDJSON protocol version 1、既存 macOS 動作、既存 `Plan.Actions` の意味は変更しない。`browser.block` は既存の `Plan.Actions` に混ぜず、P02 がポリシー状態へ変換できる純粋なルール API として分離する。

設定ファイルの表記 `config.yaml` は、現行正本 `~/.config/tcc-local-connector/config.yml` を指す。別名探索は追加しない。HTTP、launchd、systemd、常駐デーモンはこの Process の対象外である。

### 入出力契約

- 入力: `config.Action{Type: BrowserBlockActionType, Domains: []string}`。`Domains` は YAML の `domains` 配列をそのまま保持する。
- 検証済み入力: 各要素を ASCII 小文字化し、末尾ドットを除去した DNS 名。URL、path、port、wildcard、IP、非 ASCII IDN、空ラベルは拒否する。IDN を使う場合は利用者が punycode を設定する。
- ルール出力: `BuildBrowserPolicy` は有効な `Evaluation` から `[]string` を返し、重複を除き辞書順に並べる。`ensure` の `browser.block` だけを対象にする。
- `dry_run`、`planned_domains`、固定通知タイトル「ブラウザ遮断（dry-run）」との合成は P02 の責務である。P01 は、dry-run で空にする前の予定集合として、同じ正規化済み集合を P02 が受け取れる契約を固定する。
- 既存の `BuildPlan` は既存アクションだけを扱い、`browser.block` を `Plan.Actions` に出力しない。

## 変更ファイルと行番号

行番号は現行正本の調査時点の目安であり、実装時はシンボルを優先して確認する。

| ファイル | 予定シンボル / 行番号 | 変更内容 |
|---|---|---|
| `internal/constants/constants.go` | `const` / 4-73 | `BrowserBlockActionType` と、設定・集合生成で使う上限定数を追加する。既存定数の値は変更しない。 |
| `internal/config/config.go` | `Action` / 57-70 | `Domains []string` と YAML/JSON タグを追加する。既存フィールドの型・タグは変更しない。 |
| `internal/config/validate.go` | 検証コード / 14-31、`validateRule` / 93-123、`validateAction` / 125-197 | `browser.block` の配置、空配列、件数上限、ドメイン正規化・形式検証を追加する。 |
| `internal/config/validate_test.go` | `TestValidationErrorCodes` 周辺 / 11-195 | 新しいエラー識別子と `ensure` 限定、形式・上限・正規化の検証を追加する。 |
| `internal/rules/browser_policy.go` | 新規ファイル | `BuildBrowserPolicy` と必要最小限の集合処理を追加する。既存 `Plan.Actions` の型・生成経路は参照するだけで変更しない。 |
| `internal/rules/plan.go` | `BuildPlan` / 39-65、`actions` / 67-75 | `browser.block` が既存アクション出力へ入らないことを明示的に維持する。必要な場合だけ分離 API との境界をコメントで補足する。 |
| `internal/rules/plan_test.go` | 既存 `BuildPlan` テスト / 26-184 | `TestPlan_NoBrowserBlockInActions`、複数ルールの和集合、重複除去、辞書順、空集合のテストを追加する。 |
| `config.example.yml` | `rules[].ensure` / 7-17 | `browser.block` と punycode を含むドメイン設定例を追加する。実在の遮断対象を増やす例ではなく、既存例を壊さない最小例にする。 |
| `docs/config-schema.md` | `rules` / 62-73、Action一覧 / 75-119、エラーコード / 120-138 | `browser.block.domains` の型・上限・正規化・拒否形式・`ensure` 限定と新しいエラー識別子を追記する。現行 `config.yml` 正本の説明を維持する。 |

## Symbol Targets

変更を許可するシンボルは次の範囲に限定する。

- `internal/constants/constants.go:const`
- `internal/config/config.go:Action`
- `internal/config/validate.go:validateRule`
- `internal/config/validate.go:validateAction`
- `internal/config/validate.go:browserDomain`
- `internal/rules/browser_policy.go:BuildBrowserPolicy`
- `internal/rules/plan.go:BuildPlan`
- `internal/rules/plan.go:actions`
- `internal/config/validate_test.go:TestValidationErrorCodes`
- `internal/rules/plan_test.go:TestPlan_NoBrowserBlockInActions`
- `internal/rules/plan_test.go:browser policy tests`

新規テスト用の小さな補助関数、変数名、ファイル内の関数分割は実装者に委ねる。ただし外部挙動・エラー識別子・公開データ形は下記仕様から変更しない。

## ローカル定数（正本は PLAN の ★ Constants）

この表は実装時に親計画を読み戻さずに使うための転記である。値を変更する場合は親計画の ★ Constants を先に変更し、この core を再生成する。

| 定数名 | 採用値 | 単位 | P01での用途 |
|---|---:|---|---|
| `BrowserBlockActionType` | `browser.block` | 文字列 | 公開アクション型 |
| `BrowserPolicyVersion` | `1` | 整数 | P02へ渡すポリシー契約の版 |
| `BrowserPolicyFileName` | `firefox-browser-policy.json` | ファイル名 | P02の出力先契約 |
| `BrowserStateDirRelative` | `TCCLocalConnector/BrowserPolicy` | 相対パス | P02の状態領域契約 |
| `BrowserBackendHeartbeatIntervalSeconds` | `5` | 秒 | P02の再発行契約 |
| `BrowserLivenessTTLSeconds` | `15` | 秒 | P04 Hostの有効期限契約 |
| `NativeHostPollIntervalMilliseconds` | `1000` | ミリ秒 | P04の監視契約 |
| `NativeMessageHeaderBytes` | `4` | bytes | P04のフレーム契約 |
| `NativeMessageMaxPayloadBytes` | `65536` | bytes | P04の上限契約 |
| `BrowserPolicyMaxDomains` | `128` | ドメイン | `domains` 件数上限 |
| `BrowserDomainMaxBytes` | `253` | bytes | 正規化後 DNS 名の上限 |
| `FirefoxNativeHostName` | `jp.takets.tcc_local_connector.firefox` | 識別子 | P04/P06の接続契約 |
| `FirefoxExtensionID` | `firefox-domain-blocker@tcc-local-connector.takets.jp` | 識別子 | P04/P06の接続契約 |
| `BrowserPrivateWindowPolicy` | `not_allowed` | manifest値 | P05の対象外契約 |
| `BrowserFailurePolicy` | `fail_open` | 文字列 | 不正・欠損時の空集合契約 |

P01が実際に追加・参照する設定検証上限は `BrowserPolicyMaxDomains` と `BrowserDomainMaxBytes`、アクション識別には `BrowserBlockActionType` を使う。その他は後続 Process と共有する契約値であり、P01で実装しない。

## 前提条件

- `config.Config`、`config.Rule`、`config.Action`、`rules.Evaluation`、`rules.BuildPlan` の既存公開形を基準にする。
- `config.Load` は既存の権限検査、YAMLの既知フィールド検査、`Validate` 呼出しを経由する。新しい検証もこの経路へ組み込む。
- `Action.Domains` は未指定なら `nil`、空配列なら長さ0として扱う。`browser.block` は1件以上のドメインを要求する。
- 既存アクション `app.*`、`process.*`、`command.run`、`notify` の検証と `Plan.Actions` への出力を変更しない。
- 既存 NDJSON protocol version 1 と macOS クライアントの受信形式を変更しない。
- 設定値の IDN は ASCII punycode で記述される。P01は punycode の生成器を追加しない。

## 実装手順

1. `Action.Domains` と `BrowserBlockActionType`、ドメイン・集合上限の定数を追加する。既存タグ、既存定数値、既存設定の既定値は保持する。
2. 検証エラー識別子を追加し、`validateRule` のグループ情報を保ったまま、`browser.block` が `ensure` 以外に現れた場合を拒否する。
3. `browser.block` の `domains` を検証する。空配列は `browser_domains_required`、128件超は `browser_domains_limit`、各要素の不正は `invalid_browser_domain` とする。検証成功時は要素を ASCII 小文字化し、末尾ドットを除去して `Action.Domains` を正規化済みに置き換える。
4. `BuildBrowserPolicy` を既存 `BuildPlan` から分離した純粋 API として実装する。入力は `Evaluation`、出力は正規化済みドメインの新しいスライスとし、入力の map/slice を変更しない。複数の有効ルールを横断して和集合化し、辞書順で返す。
5. `BuildPlan` の既存 `actions` が `browser.block` を出力しないことを `TestPlan_NoBrowserBlockInActions` で固定する。別テストで `BuildBrowserPolicy` の和集合、重複、辞書順、空集合を固定する。
6. `config.example.yml` と `docs/config-schema.md` に設定例、正規化、拒否形式、`ensure` 限定、エラー識別子、punycode 方針を追記する。文書の `config.yaml` 表記は現行正本 `~/.config/tcc-local-connector/config.yml` の説明として統一する。
7. P02へ渡す契約をテストとコメントで固定する。P01の予定集合は `BuildBrowserPolicy` の出力、P02は `dry_run=true` のとき適用集合を空にし、予定集合が前回から変化した場合のみ固定タイトルの既存 `notify` を1回合成する。P01では通知の実行・状態ファイル・heartbeatを実装しない。

## Behavior Specification

### 変換仕様

| behavior_id | 入力 | 出力 | post_state / 不変条件 | test_ref |
|---|---|---|---|---|
| P01-BEH-01 | `browser.block` の `domains: ["X.COM.", "sub.x.com"]` | `Action.Domains == ["x.com", "sub.x.com"]` | 大文字をASCII小文字化し、末尾ドットを1つ除去する | `TestValidateBrowserBlock_NormalizesDomains` |
| P01-BEH-02 | 有効な `browser.block` が複数の有効ルールに存在 | `BuildBrowserPolicy` が重複なし・辞書順の和集合を返す | 入力 `Evaluation` は変更されず、優先度による上書きはない | `TestBuildBrowserPolicy_UnionSortedUnique` |
| P01-BEH-03 | 有効なルールに `browser.block` がない | 空の `[]string` | 既存アクションの計画には影響しない | `TestBuildBrowserPolicy_Empty` |
| P01-BEH-04 | `browser.block` が `on_enter` または `on_exit` にある | 検証エラー `browser_block_requires_ensure` | 設定は受理せず、既存 `Plan.Actions` に流さない | `TestValidateBrowserBlock_EnsureOnly` |
| P01-BEH-05 | 空配列、129件、URL/path/port/wildcard/IP/非ASCII/空ラベル | 各入力に固定エラー識別子 | 不正設定から正規化済み集合を生成しない | `TestValidateBrowserBlock_RejectsInvalidDomains` |
| P01-BEH-06 | `dry_run=true` と予定集合の変化 | P02が適用集合を空にし、予定集合を通知合成へ渡せる | P01の集合出力自体は予定集合として変わらない。通知タイトルは「ブラウザ遮断（dry-run）」に固定 | `TestPlan_NoBrowserBlockInActions` と P02契約テスト |

### ドメイン検証規則

- ASCII 小文字化後、末尾ドットを除去する。空文字になった場合は拒否する。
- 全体のバイト長は `BrowserDomainMaxBytes` 以下とする。
- ラベルを `.` で分割し、空ラベルを拒否する。各ラベルは ASCII の英数字または `-` だけで構成し、先頭・末尾を `-` にしない。
- URLスキーム、`/` を含む path、`:` を含む port、`*` などの wildcard、IPv4/IPv6 の IP 表記を拒否する。
- 非 ASCII 文字を拒否する。IDN は punycode を入力する。
- `domains` は文字列配列のみを受理し、空配列は必須値エラーとする。

### P02への集合契約

`BuildBrowserPolicy` の戻り値は、設定検証済みの `Evaluation` から得た「予定集合」である。P02は次を保証する。

- P01は正規化済みの予定集合を返す。適用集合への変換はP02、policyのlivenessとheartbeatの欠損・破損・期限切れ判定はP04の責務である。
- `dry_run=true` ならP02が適用集合を空にし、予定集合を維持する。
- `dry_run=true` で予定集合が非空かつ前回fingerprintから変化した場合だけ、既存 `notify` にタイトル「ブラウザ遮断（dry-run）」と予定ドメインを1回渡す。集合が空または不変なら通知しない。
- pause、取得失敗grace超過、`release_controls`、設定不正、policy書込み失敗ではP02が `enforce=false` の空policyを出す。Hostによるheartbeat TTL判定はP04だけが行う。
- P01は上記状態判定、ファイル出力、heartbeat、Native Messagingを実装しない。

## Correctness Criteria

- `browser.block` の `domains` が `Action.Domains []string` として YAML から読み込める。
- 空、件数超過、DNS境界不正、URL/path/port/wildcard/IP/非ASCII/空ラベルが、指定されたエラー識別子で拒否される。
- 大文字と末尾ドットの正規化結果が常に同じ入力集合を同じ出力へ収束させる。
- 複数の有効ルールの集合は重複なし・辞書順であり、ルール優先度によって欠落・上書きされない。
- `on_enter` と `on_exit` の `browser.block` は受理されない。
- `BuildPlan` の `Plan.Actions` に `browser.block` が存在しない。既存 `app.*`、`notify`、`process.*`、`command.run` の既存挙動は回帰しない。
- `dry_run` の予定集合／適用集合／通知合成の境界が P02へ渡す入力・出力としてテストで確認可能である。
- `go test ./internal/config ./internal/rules -race -count=1` が成功する。
- `config.example.yml` と `docs/config-schema.md` が実装仕様と一致し、現行 `~/.config/tcc-local-connector/config.yml` の正本を指す。

## Left to Implementation

- `BuildBrowserPolicy` を新規ファイルに置くか既存 `plan.go` の末尾に置くか。
- 正規化処理を小さな非公開関数へ分割する方法と、テーブルテストの具体的な補助型。
- エラーの `Message` の自然文。ただし `Code`、`Path`、エラー発生条件は本書から変更しない。
- 辞書順比較に既存 Go の文字列比較を使うかどうか。

上記以外の公開挙動、設定データ形、エラー識別子、互換性、テスト条件は実装者の判断に残さない。

## Verification Gates

| gate_id | phase | type | executor | required | command | pass_criteria | evidence_spec | failure_policy | criterion_refs |
|---|---|---|---|---|---|---|---|---|---|
| P01-VG-01 | test | test | agent | true | `go test ./internal/config ./internal/rules -run 'Test(ValidateBrowserBlock|BuildBrowserPolicy|Plan_NoBrowserBlock)' -count=1` | 対象テストが存在し、終了コード0。無効入力、正規化、和集合、既存Plan分離が通過する | コマンド全文、終了コード、対象テストのPASS行を記録 | テスト失敗時は完了扱いにせず、該当シンボルと証跡を確認して修正後に再実行 | SC-01, SC-02, SC-03, SC-07 |
| P01-VG-02 | regression | test | agent | true | `go test ./internal/config ./internal/rules -race -count=1` | 終了コード0。既存設定検証、既存Plan生成、browser追加テストが全て通過する | 標準出力のPASS行と終了コードを記録 | 回帰失敗時は既存アクション・NDJSON互換を優先して原因箇所へ戻る。無関係な修正はしない | SC-02, SC-03, SC-07 |
| P01-VG-03 | quality | test | agent | true | `gofmt -d internal/constants/constants.go internal/config/config.go internal/config/validate.go internal/config/validate_test.go internal/rules/browser_policy.go internal/rules/plan.go internal/rules/plan_test.go && go vet ./internal/config ./internal/rules` | `gofmt -d` が空、`go vet` が終了コード0 | 両コマンドの出力、終了コード、空差分を記録 | 失敗時は対象ファイルだけを修正し、既存未追跡資材を変更しない | SC-07 |
| P01-VG-04 | docs | inspection | agent | true | `rg -n 'browser\.block|domains|invalid_browser_domain|browser_block_requires_ensure|config\.yml' config.example.yml docs/config-schema.md` | 例・スキーマ・エラー識別子・現行正本の説明が存在し、`config.yaml` 別名探索を示す記述がない | 一致行を記録し、仕様項目との対応を示す | 不足時は指定文書の該当節だけを更新し、実装コードや未追跡計画を変更しない | SC-02, SC-03, SC-08 |

## 生成情報

- 担当: P01
- 題名: 設定・ドメイン正規化・ルール集合
- 正本: `/Users/takets/repos/tcc-local-connector/PLAN-firefox-extension.md` の `★ Constants`、Success Criteria、Verification Contract
- 調査根拠: `internal/constants/constants.go:4`、`internal/config/config.go:57`、`internal/config/validate.go:118-160`、`internal/rules/plan.go:39-90`、`config.example.yml`、`docs/config-schema.md`
- Success Criteria 対応: SC-01 ドメイン境界、SC-02 検証/正規化、SC-03 ルール合成/dry_run、SC-07 既存回帰、SC-08 文書方針
- 再生成条件: `★ Constants`、Success Criteria、Verification Contract、P01の境界、エラー識別子、または横断方針を変更した場合は、この core を再生成する。
