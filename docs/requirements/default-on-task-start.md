# `default.on_task_start` 要件定義

## 1. 背景・問題

現在の設定モデルは `rules` の評価結果から計画を生成し、`ensure` を状態維持のために各ポーリングサイクルで評価する。設定のトップレベルは `Config` に定義された項目だけを受け付けるため、常時またはタスク開始時に一度だけ行う共通アクションを表現できない（`internal/config/config.go:15-22`）。また、現行計画はルールの優先度と対象キーで並べ替え、`command.run`、`process.start`、`process.stop` は計画変換で除外している（`internal/rules/plan.go:39-89,113-144`、`internal/engine/engine.go:285-310`）。

このため、タスク開始を契機に通知や補助プロセスを一度だけ起動する先行フェーズを追加する。既存 `rules` の優先度・競合解決を変更せず、取得失敗・pause・reload・dry-runを含む観測ライフサイクルを明確にする。

## 2. 目的・成功条件

### 2.1 目的

- トップレベル任意設定 `default` に、通常ルールから独立した先行フェーズを追加する。
- `default.on_task_start` を、成功したタスク観測における新規 `task_id` の検出時に、1サイクル1回だけ実行する。
- 既存 `rules` の評価・priority・競合解決・protocol v1を維持する。
- defaultアクションの失敗が通常ルールの実行を妨げないようにする。

### 2.2 成功条件

1. 指定された状態差分表の全ケースで、defaultの起動回数が決定論的に一致する。
2. 1サイクル中に新規タスクが複数あってもdefaultは1回だけである。
3. defaultのdispatchが通常rulesのdispatchより先である。
4. default未定義の設定では従来のplan差分がない。
5. dry-run、取得失敗、不正なtask_id、pause、reload、およびアクション失敗が安全に扱われる。
6. backendアクションが計画から黙って消えず、所有実行器へ到達する。

## 3. 用語

| 用語 | 定義 |
|---|---|
| サイクル | 1回のポーリング観測から計画・dispatch・結果記録までの単位。`cycle_id`で識別する。 |
| 成功ポーリング | タスク取得・解析に成功し、有効なtask_id集合を得たポーリング。 |
| `CurrentTaskIDs` | 当該成功ポーリングで観測した一意な有効task_id集合。 |
| `PreviousTaskIDs` | 直前の比較可能な成功ポーリングで保持した集合。 |
| タスク開始 | `NewTaskIDs = CurrentTaskIDs - PreviousTaskIDs` が1件以上となること。 |
| default | 通常 `rules` とは独立した、各サイクルで先に処理する設定ブロック。 |
| `default.on_task_start` | タスク開始サイクルに1回だけ計画されるアクション列。 |
| dispatch | 計画済みアクションを所有する実行器へ渡すこと。完了を意味しない。 |
| 観測セッション | プロセス起動から終了までの比較状態の有効期間。再起動をまたぐexactly-onceは保証しない。 |

## 4. スコープ

### 4.1 In Scope

- トップレベル任意キー `default` の設定・検証。
- `default.on_task_start` アクション列の計画、ActionID付与、dispatch、結果ログ。
- 成功ポーリング間のtask_id集合差分による開始判定。
- default→通常rulesの論理dispatch順序。
- defaultアクションのdry-run、失敗継続、timeout、セキュリティ制約。
- backendアクションを計画から所有実行器へ到達させる必須前提修正。
- 必要な設定・計画・エンジン・テストの変更。

### 4.2 Out of Scope

- プロセス再起動をまたぐexactly-once保証。
- タスク値を用いたテンプレート展開。
- 新規taskの件数に比例した実行（タスク数分の実行）。
- `default` における `browser.block` ポリシー。
- frontendアクションの完了待ち、または異なる実行器間の実効完了順保証。
- ポーリング間に開始・終了したタスクのリアルタイム検出。
- protocol v1の変更、新RPCの追加。

## 5. 設定スキーマ

トップレベル `default` は任意であり、未定義は空の `on_task_start` と同義とする。`version: 2` を維持する（現行スキーマのトップレベル定義は `docs/config-schema.md:15-24`）。

```yaml
version: 2
safety:
  dry_run: false
  allow_shell: false

default:
  on_task_start:
    - type: notify
      title: "タスク開始"
      message: "新しいタスクを検出しました"
      level: info
    - type: command.run
      executable: /absolute/path/to/notification.sh
      timeout_seconds: 30

rules:
  - id: focus-work
    priority: 100
    match:
      task_name_contains: ["集中作業"]
    ensure:
      - type: app.stop
        bundle_id: com.example.Distraction
```

### 5.1 スキーマ制約

- `default` はobject、`on_task_start` は配列で、最大20アクションとする。
- 許可するアクションは `app.start`、`app.stop`、`process.start`、`process.stop`、`command.run`、`notify` の6種とする。
- `browser.block` はMUST NOT許可とし、設定エラーにする。
- default内の相反操作（同一対象への相反するstart/stop等）は設定エラーにする。
- defaultアクションはYAML記載順に計画する。通常rulesとのpriority比較・dedupe対象にしない。
- `command.run.timeout_seconds` の省略値は30秒、指定時は1..300秒とする。
- 既存のアクション検証（絶対パス、実行可能性、bundle/process識別子、引数等）を継承する。

## 6. 状態・データモデル

### 6.1 比較状態

エンジンは観測セッション中、次を保持する。

```text
TaskStartState {
  initialized: bool
  previous_task_ids: Set<TaskID>
  session_id: opaque/session-local
}
```

- 未初期化→空集合は起動0回、比較状態を空集合として初期化する。
- 未初期化→非空集合は起動1回、比較状態を当該集合へ更新する。
- 成功ポーリング間では集合演算を行う。順序、名前、日付、その他メタデータの変化は開始とみなさない。
- 有効なtask_idの欠損または重複IDがあるサイクルは比較不能とし、defaultを起動せず比較状態も更新しない。
- 取得失敗では比較状態を保持する。`grace`/`released`への遷移でもリセットしない。

### 6.2 計画アクション

既存 `rules.PlannedAction` の計画表現を拡張し、少なくとも次を保持する（現行フィールドは `internal/rules/plan.go:11-33`）。

| 項目 | 要件 |
|---|---|
| `cycle_id` | 当該サイクル識別子。 |
| `action_id` | `<cycle_id>-<sequence>`。defaultと通常rulesを通じてサイクル内一意。 |
| phase | `default` または `rules`。 |
| action type | 設定されたアクション種別。 |
| reason | defaultは必ず `default.on_task_start`。 |
| sequence | defaultへ先に連番を割り当て、その後rulesへ続ける。 |

`bundle_ids`等の複数対象は展開後の個別アクションごとにActionIDを付与する。

## 7. 機能要件

### 7.1 設定・検証

- **FR-001** システム MUST `default.on_task_start` をトップレベル任意設定として読み込む。
- **FR-002** システム MUST 不明なdefaultフィールド、未許可アクション、上限超過、相反操作を設定エラーとして拒否する。
- **FR-003** システム MUST `browser.block`をdefaultで拒否する。
- **FR-004** システム MUST default未定義を正常設定として扱い、従来plan差分を発生させない。

### 7.2 タスク開始判定

- **FR-010** システム MUST 成功ポーリングで得た有効なtask_id集合を `CurrentTaskIDs` とする。
- **FR-011** システム MUST `NewTaskIDs = CurrentTaskIDs - PreviousTaskIDs` を計算する。
- **FR-012** `NewTaskIDs`が1件以上なら、システム MUST当該サイクルでdefaultを1回だけ起動する。
- **FR-013** 複数の新規task_idがあっても、システム MUSTタスク数分に増幅しない。
- **FR-014** システム MUST順序・名前・メタデータのみの変化を開始とみなさない。
- **FR-015** 初回成功ポーリング時にタスクが既に実行中なら、システム MUST defaultを1回起動する。
- **FR-016** システム MUST観測セッション内で比較状態を更新し、再起動をまたぐexactly-onceを保証しない。

### 7.3 例外時の比較状態

- **FR-020** 取得失敗時、システム MUST defaultを起動せず、`PreviousTaskIDs`を保持する。
- **FR-021** A→失敗→Aでは、システム MUST再起動しない。
- **FR-022** A→失敗→Bでは、復旧時にBを新規としてMUST defaultを1回起動する。
- **FR-023** grace/released状態でも、システム MUST比較集合をリセットしない。
- **FR-024** 有効task_id欠損または重複IDを検出した場合、システム MUST defaultを起動せず、比較状態を更新しない。通常rules評価は継続し、警告を記録する。
- **FR-025** pause中、システム MUSTタスク取得およびdefault起動を行わない。resume後はpause前集合と比較する。

### 7.4 reload・dry-run

- **FR-030** config reloadだけでは、システム MUST defaultを起動しない。
- **FR-031** default追加を含むreloadが実行中でも、システム MUST過去の開始を遡及実行しない。
- **FR-032** 不正reload時、システム MUST現行設定および比較状態を保持する。
- **FR-033** default未設定でも、システム MUST比較状態を通常どおり更新する。
- **FR-034** dry-runでは、システム MUST計画生成・ActionID付与・比較集合更新を行い、副作用を発生させず、各アクションを`skipped`として記録する。
- **FR-035** 同一タスク中にdry-run falseへreloadしても、システム MUST過去にskipしたdefaultを遅延実行しない。

### 7.5 計画・dispatch

- **FR-040** 成功観測サイクルの論理順を、(1)取得成功、(2)開始差分、(3)default計画/dispatch、(4)通常rules評価/dispatch、(5)結果記録とする。
- **FR-041** defaultアクションはYAML順に計画し、通常rulesとのpriority比較・dedupeをMUST NOT行う。
- **FR-042** 通常rulesは従来のpriority順・競合解決を維持し、設定記載順実行へ変更してはならない。
- **FR-043** defaultと通常rulesを通じActionIDをサイクル内一意にし、default側へ先の連番を割り当てる。
- **FR-044** dispatch順はdefaultが通常rulesより先であることをMUST保証する。ただし実効完了順は保証しない。
- **FR-045** frontendアクションの完了待ちをMUST要求しない。default失敗、timeout、frontend未報告でも通常rulesを必ず続行する。
- **FR-046** default内の1アクション失敗時、残りのdefaultアクションをMUST続行する。

### 7.6 backend実行到達

- **FR-050** `command.run`、`process.start`、`process.stop`を計画変換で黙って落としてはならず、所有実行器へ到達させる。
- **FR-051** 現行 `TestBuildPlan_NoUnsupportedActions` は、これらのbackendアクションが計画に含まれ実行器へ到達することを検証する内容へ更新する。
- **FR-052** default追加に伴う変更で、既存rule内で休眠していたbackendアクションが動き始める高リスクを明示し、回帰テストと移行注意を提供する。

### 7.7 結果・ログ

- **FR-060** backend結果を破棄せず、`cycle_id`、ActionID、phase、action type、status、error code、durationを構造化ログへ記録する。
- **FR-061** 通知へ秘密情報またはstdout/stderr全文を出力してはならない。既存の要約通知方針を継承する。
- **FR-062** `command.run` timeout省略時は30秒、指定時は1..300秒とし、timeout後は`failed`/`timeout`として記録して後続処理を続行する。

## 8. 非機能要件

- **NFR-001 決定性**: 同一の成功ポーリング集合列と設定は同一の起動判定・ActionID列を生成しなければならない。
- **NFR-002 互換性**: config versionは2、protocol versionはv1を維持し、新RPCを追加してはならない（`docs/protocol-v1.md:1-28,131-133`）。
- **NFR-003 可用性**: defaultの失敗が通常rulesの評価・dispatchを停止させてはならない。
- **NFR-004 安全性**: dry-run判定はdispatch前に行い、dry-run中に副作用を発生させてはならない（現行実行器の境界は `internal/engine/engine.go:326-334`）。
- **NFR-005 実行ファイル保護**: `command.run`/`process.start`のexecutableは絶対パスかつ実行可能でなければならない。
- **NFR-006 shell保護**: shellは既定false。trueは`allow_shell`と厳格な設定ファイル権限を要求する（`internal/config/config.go:89-103`）。
- **NFR-007 ledger境界**: process制御は既存ledger境界を継承し、任意プロセスを停止してはならない（`internal/engine/engine.go:348-357`）。
- **NFR-008 入力隔離**: task値をアクションのテンプレートへ展開してはならない。
- **NFR-009 観測限界**: リアルタイム同時実行を要求せず、ポーリング間に開始・終了したtaskは検出されないことを仕様として明示する。
- **NFR-010 ログ衛生**: 構造化ログは監査可能である一方、秘密・stdout/stderr全文を漏えいさせてはならない。

## 9. 実行順序と保証境界

```text
poll success
  -> validate task_id set
  -> NewTaskIDs = Current - Previous
  -> if NewTaskIDs != empty: plan/dispatch default.on_task_start once
  -> evaluate/build/dispatch existing rules
  -> record backend/frontend results and structured logs
```

保証するのは上記のdispatch順だけである。frontendの処理完了、backendとfrontendの実効完了順、プロセス再起動をまたぐexactly-onceは保証しない。したがって、外部副作用を持つdefaultアクションは必要に応じて利用者側で冪等化する。

## 10. エラー・セキュリティ

| 事象 | 必須動作 |
|---|---|
| 取得失敗 | defaultを起動せず、比較集合を保持。通常の失敗猶予/状態遷移は継承。 |
| task_id欠損・重複 | defaultを起動せず、比較状態を更新せず、警告を記録。通常rulesは継続。 |
| 不正reload | 現設定・比較状態を保持し、エラーを返す。 |
| defaultアクション失敗 | 残りdefaultと通常rulesを継続。status/error codeを記録。 |
| command timeout | `failed`/`timeout`を記録し、通常rulesを継続。 |
| dry-run | dispatch前にskipし、副作用なし。 |
| shell有効化 | `allow_shell`と厳格権限を必須化。 |
| process.stop | ledgerに存在する管理対象だけを操作。 |
| browser.block指定 | 設定エラーとして拒否。 |

既存のabsolute executable、exec bit、shell false既定、shell trueの厳格権限、ledger境界を変更しない。アクションの引数、環境変数、通知内容に秘密を直接埋め込まない。

## 11. 後方互換性・移行注意

- `default`未定義の設定は従来どおり動作し、通常rulesのplan差分を発生させない。
- config version 2とprotocol v1を維持するため、既存クライアントに新しい必須フィールドを要求しない。
- 既存のrulesにある `command.run`、`process.start`、`process.stop` は、これまで計画から除外されていた場合でも本修正後に実行され得る。これは意図した到達性修正だが、既存設定の副作用を再確認するまでdry-runで検証すること。
- `ensure`をdefaultの代替として扱わない。`ensure`は毎サイクル状態維持であり、開始時一回の意味には流用しない。
- reloadでdefaultを追加しても、既に実行中のtaskへ遡及発火しない。

## 12. 受入条件

### 12.1 状態差分

| Given（前回→今回） | When | Then |
|---|---|---|
| 未初期化→空 | 成功ポーリング | default 0回、比較集合は空で初期化 |
| 未初期化→A | 成功ポーリング | default 1回 |
| 空→A | 成功ポーリング | 1回 |
| A→A | 成功ポーリング | 0回 |
| A→B | 成功ポーリング | 1回 |
| A→A+B | 成功ポーリング | 1回 |
| A+B→B | 成功ポーリング | 0回 |
| A+B→B+C | 成功ポーリング | 1回 |
| 順序/名前/メタデータのみ変化 | 成功ポーリング | 0回 |
| A→空→A | 各成功ポーリング | 2回（A再登場時に再度1回） |

### 12.2 失敗・制御状態

| Given | When | Then |
|---|---|---|
| Aを保持 | 取得失敗→A | default 0回、比較集合Aを保持 |
| Aを保持 | 取得失敗→B | 復旧サイクルで1回 |
| Aを保持 | grace/released | 比較集合をリセットしない |
| Aを保持 | pause→resume後にA | pause前集合との比較で0回 |
| task_id欠損/重複 | 当該サイクル | default 0回、比較状態不変、警告、通常rules継続 |

### 12.3 計画・失敗継続・dry-run

| Given | When | Then |
|---|---|---|
| default 2件、rules 2件 | タスク開始 | ActionIDはdefaultへ先に連番、全体で一意 |
| default内に失敗 | dispatch | 残りdefaultと通常rulesを続行 |
| default frontend未報告 | dispatch | 完了待ちせず通常rulesを続行 |
| dry_run=true | タスク開始 | 計画・ID・比較更新、全副作用なし、`skipped` |
| dry_run=true→false reload | 同一task継続 | skip済みdefaultを遅延実行しない |
| `browser.block` | config load | 設定エラー |
| default内相反操作 | config load | 設定エラー |
| timeout省略 | command.run | 30秒上限 |
| timeout=0/301 | config load | 設定エラー |

## 13. 禁止シナリオ

- `rules`を設定記載順で実行し、priority・競合解決を破壊すること。
- defaultを通常rulesのdedupe・priority競合へ混ぜ、defaultを消去すること。
- 1サイクル内の新規task数に比例してdefaultを複数回起動すること。
- 取得失敗、pause、grace/releasedを理由に比較集合を空へリセットすること。
- 不正なtask_idサイクルを成功観測として比較状態へ反映すること。
- default失敗やfrontend完了待ちを理由に通常rulesを停止すること。
- `command.run`、`process.start`、`process.stop`を計画変換で黙って捨てること。
- `browser.block`、task値テンプレート、クロス実行器の完了順を追加すること。
- dry-runでdispatch後に副作用を止めようとすること。
- stdout/stderr全文や秘密を通知へ出力すること。

## 14. リスクと対策

| リスク | 対策 |
|---|---|
| ポーリング間の短命taskを見逃す | リアルタイム保証外であることを文書化し、必要ならポーリング間隔を調整する。 |
| 再起動後の重複通知 | 観測セッション単位の制約を明示し、通知スクリプトを冪等化する。 |
| backend actionの休眠解除 | 既存設定を棚卸しし、dry-run・回帰テスト・リリースノートで周知する。 |
| task_id異常による誤発火 | 欠損/重複をrejectし、比較状態を更新しない。 |
| default失敗で本来の制御が止まる | defaultとrulesのエラー境界を分離し、rulesを必ず続行する。 |
| ActionID衝突・結果紐付け誤り | サイクル全体の単一連番と構造化ログで検証する。 |
| shell/実行ファイルによる任意コード実行 | 既存の権限、absolute path、exec bit、allow_shell、config modeを継承する。 |

## 15. 依存関係

- `internal/config` の設定型、既知フィールド検証、アクション検証。
- `internal/rules` の計画生成・競合解決・ActionID生成。
- `internal/engine` のポーリング、pause/reload、dispatch、結果処理。
- backend所有実行器（process ledger、command runner）とfrontend実行経路。
- `docs/config-schema.md` および本要件定義に整合するテスト。
- protocol v1の既存plan/report/log境界（`docs/protocol-v1.md:56-92`）。

## 16. 前提

- task_idは成功ポーリングで一意かつ空でない場合のみ比較可能である。
- 比較状態はプロセスメモリ内の観測セッション状態でよい。
- defaultのdispatchは同期完了を待たない。
- 既存のpolling failure policy、pause、reload、dry-runの基本状態遷移を変更しない。
- defaultアクションの設定者は外部副作用の冪等性を必要に応じて担保する。

## 17. 未決事項

- defaultアクションの構造化ログを既存 `event.plan`/`report_actions` のどの公開フィールドまで拡張するか。
- frontendアクションのdispatchを同期呼び出しにするか、既存の非同期実装を維持するか。
- 不正task_id時の警告コード名とstatus APIへの公開方法。
- `default` の設定スキーマを `docs/config-schema.md` の本体へ反映するタイミング。

