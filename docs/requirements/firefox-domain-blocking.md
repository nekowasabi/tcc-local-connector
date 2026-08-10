# Firefoxドメイン遮断機能 要件・実装案

## 問題

特定のタスクを実行中に、気が散るサイトを閲覧してしまうことを防ぐ。URLの転送ではなく、対象ドメインへの通常の閲覧を遮断し、Firefox内の説明ページを表示する。

## 採用方針

- 対象ブラウザはFirefoxのみ。
- 通常ウィンドウを対象とし、プライベートウィンドウは対象外とする。
- 遮断単位はURLパスではなくドメインとする。
- `example.com` を指定した場合、`example.com` とそのサブドメインを対象とする。
- ネイティブホストが切断された場合は許可する。
- `config.yml` に遮断設定がない場合は通常のブラウジングを許可する。
- `launchd` / `systemd` による常駐デーモンは使用しない。
- Firefox拡張の `runtime.connectNative()` で、必要なときだけネイティブホストを起動する。
- 遮断時は拡張内の説明ページを表示する。
- `dry_run: true` の場合は遮断せず、遮断予定の通知だけを表示する。

## 設定案

タスクごとの `ensure` に遮断ドメインを定義する。`ensure` は現在の望ましい状態を表すため、対象タスクが有効な間は遮断ポリシーを維持できる。

```yaml
rules:
  - id: focus-work
    match:
      task_name_contains: ["集中作業"]
    ensure:
      - type: browser.block
        domains:
          - x.com
          - youtube.com
          - news.example.com
```

### 設定仕様

- `type: browser.block` は新しいアクション種別とする。
- `domains` は1件以上必須とする。
- ドメインは小文字へ正規化し、末尾のドットを除去する。
- URL、パス、ポート、ワイルドカードは受け付けない。
- ルールが複数有効な場合、遮断ドメインは和集合とする。
- 有効な遮断ルールがなくなった場合、空の遮断ポリシーをFirefoxへ送る。

## システム構成

```text
Firefox拡張
  ├─ webRequest.onBeforeRequest
  │    └─ 遮断対象ドメインをキャンセル
  ├─ webNavigation.onBeforeNavigate
  │    └─ blocked.html を表示
  └─ runtime.connectNative()
       └─ Firefox Native Messaging Host
            └─ tcc-local-connector のタスク取得・ルール評価
```

### Firefox拡張

- 通常ウィンドウの対象リクエストだけを監視する。
- `webRequestBlocking` で対象ドメインのメインフレーム要求を遮断する。
- 遮断時は `blocked.html` に対象ドメインと理由を表示する。
- 拡張起動時の初期状態は許可とする。
- ネイティブホスト切断時は保持中の遮断リストを消去し、許可へ戻す。
- プライベートウィンドウ用の追加権限や専用処理は実装しない。

### Native Messaging Host

- Firefoxの `runtime.connectNative()` から起動される。
- `~/Library/Application Support/Mozilla/NativeMessagingHosts/` にホストマニフェストを配置する。
- HTTPサーバーは起動しない。
- Firefoxとの標準入出力をNative Messaging形式で処理する。
- 既存バックエンドの改行区切りJSONとは形式が異なるため、専用ホストまたは変換層を追加する。
- Firefoxとの接続が終了したらプロセスを終了する。
- `config.yml` が存在しない、設定が無効、または対象タスクがない場合は許可ポリシーを返す。

## 機能要件

### FR-01 タスクによる遮断

対象タスクが有効になった場合、設定されたドメインをFirefox拡張へ通知する。

### FR-02 ドメイン一致

`example.com` は `example.com` と `*.example.com` に一致し、`example.com.evil.test` には一致してはならない。

### FR-03 遮断

遮断中に対象ドメインへ移動した場合、対象リクエストを完了させず、`blocked.html` を表示する。

### FR-04 許可への復帰

対象タスクが終了した場合、遮断リストを空にして通常のブラウジングへ戻す。

### FR-05 切断時の許可

Native Messaging Hostが切断された場合、Firefox拡張は対象ドメインを遮断しない。

### FR-06 未定義時の許可

設定に`browser.block`が存在しない場合、Firefox拡張はすべての対象ドメインを許可する。

### FR-07 dry run

`dry_run: true` の場合、遮断ポリシーをFirefoxへ適用せず、遮断予定のドメインだけを通知する。

## 非機能要件

- 常駐デーモンを前提にしない。
- ネイティブホストはFirefoxとの接続がない状態で常駐しない。
- ネイティブホストが使用できない場合、通常のブラウジングを妨げない。
- Native Messaging Hostは許可されたFirefox拡張IDからの接続だけを受け付ける。
- 設定ファイルの権限は所有者のみ読み書き可能な状態を維持する。
- ドメイン照合は大文字小文字によらず、末尾ドットの有無によらず同一結果になる。

## 受け入れ条件

- Given `集中作業` が有効で `x.com` が設定されているとき、When Firefoxで`https://x.com`を開くと、Thenページ内容を表示せず`blocked.html`を表示する。
- Given `集中作業` が有効で `x.com` が設定されているとき、When `https://sub.x.com`を開くと、Then遮断する。
- Given `集中作業` が有効で `x.com` が設定されているとき、When `https://example.com`を開くと、Then遮断しない。
- Given 対象タスクが終了したとき、When `https://x.com`を開くと、Then通常どおり表示する。
- Given Native Messaging Hostが停止したとき、When`https://x.com`を開くと、Then通常どおり表示する。
- Given `config.yml` に`browser.block`がないとき、When任意のサイトを開くと、Then通常どおり表示する。
- Given `dry_run: true` のとき、When対象タスクが有効になっても、Thenサイトを遮断せず通知だけ表示する。
- Givenプライベートウィンドウであるとき、When対象サイトを開くと、Then本機能による遮断を保証しない。

## 実装工程案

1. 設定・アクション定義に`browser.block.domains`を追加する。
2. ルール計画で遮断ドメインの和集合を生成する。
3. Native Messaging Hostのフレーム変換層を追加する。
4. Firefox拡張の接続・遮断ポリシー受信を実装する。
5. `webRequestBlocking` と説明ページを実装する。
6. macOS用Native Messaging Hostマニフェストのインストール処理を追加する。
7. `dry_run`、切断時許可、設定未定義時許可をテストする。
8. Firefox通常ウィンドウで手動検証する。

## リスクと対策

| リスク | 対策 |
|---|---|
| Native Messaging Hostが起動できない | 許可へフォールバックし、メニューとログへ通知する |
| 古い遮断状態が残る | 切断イベントで遮断リストを即時消去する |
| サブドメイン判定の誤り | ドメイン境界を`.`単位で比較するテストを追加する |
| プライベートウィンドウの挙動差 | 対象外として明示し、専用権限を要求しない |
| 既存メニューバーアプリとの競合 | 既存のHTTPサーバーを変更せず、Native Messaging専用の入口を追加する |

## 未決事項

- 初期リリースで遮断する具体的なドメイン一覧。
- `blocked.html` に表示する説明文と、タスク名を表示するかどうか。
- Native Messaging Hostの実行ファイルを既存バックエンドと共用するか、専用実行ファイルにするか。
