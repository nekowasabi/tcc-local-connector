# WSL2要素技術 動作検証

検証日: 2026-08-01
検証環境: Ubuntu on WSL2 / Linux 6.6.114.1-microsoft-standard-WSL2 / x86_64

## 1. 結論

Windows側のC#常駐フロントから`wsl.exe`を介して、WSL2内のGoバックエンドを標準入出力で長時間接続する構成は、主要な要素技術について実現可能と判断する。

今回、次を実動作で確認した。

- `wsl.exe --distribution ... --user ... --exec ...`によるLinux ELFバイナリの起動
- 改行区切りJSONによる双方向通信
- 1,000件の同時要求
- 応答順序の入れ替わり
- 8 KiBのJSON要求
- 要求の取消
- 不正JSONと過大入力からの復旧
- 標準入力EOFによる正常終了
- `SIGTERM`による正常終了
- Goバックエンドから`tcc2 mcp`を子プロセスとして起動
- MCP初期化、`tools/list`、`get_taskchute`の実呼出し

未検証なのは、Windows実機のC# / WPFフロント、Windowsのスリープ・ログアウト、C#プロセスの異常終了後にLinuxプロセスが残らないこと、およびWindowsアプリ操作要求の往復である。

## 2. 観察済み環境

```text
WSL_DISTRO_NAME=Ubuntu
Linux kernel=6.6.114.1-microsoft-standard-WSL2
Go=1.26.0 linux/amd64
tcc2=0.0.26
wsl.exe=/mnt/c/Windows/system32/wsl.exe
tmux=利用可能
nvim=利用可能
```

`wsl.exe`による固定引数の起動も成功した。

```text
wsl.exe --distribution Ubuntu --user takets --cd <repo> --exec /bin/printf WSL_EXEC_OK
```

## 3. 試作したGoバックエンド

実装:

- `cmd/tcc-local-connector-backend/main.go`
- `internal/protocol/server.go`
- `internal/tcc2/probe.go`

起動形式:

```text
tcc-local-connector-backend serve --stdio \
  --tcc2-executable /absolute/path/to/tcc2
```

プロトコル:

- 1行1JSON
- `version`、`id`、`method`、`params`
- 起動時に`ready`イベントを送信
- `health`
- `echo`
- `sleep`
- `cancel`
- `tcc2_probe`
- 64 KiBの要求上限
- 標準出力はプロトコル専用
- 標準エラーは診断専用

## 4. 自動テスト結果

自動テストは次の境界を含む。

- 正常な`health`と`echo`
- Unicodeを含むJSON
- 並行要求の順序入れ替わり
- 実行中要求の取消
- 使用中の要求識別子の重複
- 不正JSON後の継続
- 非対応プロトコル版
- 過大入力後の継続
- `bufio.Reader`の内部バッファを超える正常入力
- `tcc2_probe`
- 入力待ち中のコンテキスト取消
- 改行なしの最終要求
- EOF時の実行中要求取消
- 250件の並行要求
- MCP初期化と`tools/list`の解析
- MCP応答識別子の不一致
- 過大なMCP応答
- コマンドライン引数検証

実行コマンド:

```text
go test -v -timeout 30s ./...
go test -race -count=10 -timeout 90s ./...
go vet ./...
go build -trimpath -o .tmp-bin/tcc-local-connector-backend \
  ./cmd/tcc-local-connector-backend
```

`go test -race -count=10`は全パッケージで成功した。

## 5. `wsl.exe`越しの実プロセス検証

再実行用:

```text
python3 scripts/verify-wsl-backend.py \
  --backend .tmp-bin/tcc-local-connector-backend \
  --requests 1000
```

実測結果:

```json
{"case":"direct","clean_exit":true,"elapsed_ms":178,"requests":1004,"tool_count":28}
{"case":"wsl.exe","clean_exit":true,"elapsed_ms":312,"requests":1004,"tool_count":28}
```

この時間は1回のローカル実測値であり、性能保証値ではない。

両経路で次を確認した。

- `ready`受信
- `health=ok`
- 1,000件の`health`
- 8 KiBの`echo`
- `sleep`取消
- `tcc2_probe`
- `get_taskchute`の存在
- EOF後の終了コード0
- 標準エラー出力なし

## 6. `tcc2 MCP`実呼出し

WSL2内の`tcc2 0.0.26`に対して次を実行した。

1. `initialize`
2. `notifications/initialized`
3. `tools/list`
4. `tools/call(get_taskchute)`

結果:

```text
initialize_ok=true
tool_count=28
has_get_taskchute=true
get_taskchute call_ok=true
content_items=1
content_bytes=3521
clean_exit=true
stderr_empty=true
```

検証時点では`[In Progress]`が含まれなかった。これは呼出し失敗ではなく、検証時点の返却内容に実行中タスク表記がなかったことを示す。

## 7. 発見した問題と対策

### 7.1 `wsl.exe --exec`のPATH

`wsl.exe --exec`で起動したプロセスのPATHには、Linuxbrewの次のディレクトリが含まれなかった。

```text
/home/linuxbrew/.linuxbrew/bin
```

そのため、Goバックエンドが単に`tcc2`を起動すると、次のエラーになった。

```text
exec: "tcc2": executable file not found in $PATH
```

対策として、Goバックエンドへ`--tcc2-executable`を追加し、設定で検証済みの絶対パスを渡せるようにした。ログインシェルを起動してPATHを補う方式は採用しない。

### 7.2 長い正常メッセージ

最初の読込実装では、`bufio.Reader`の内部バッファを超えた時点で、設定した64 KiB上限より小さいメッセージも過大と判定する問題があった。

対策として、上限まで断片を蓄積し、上限超過時だけ行末まで破棄して次の要求へ復旧する実装へ変更した。8 KiBの正常要求と過大要求後の復旧をテストした。

### 7.3 入力待ち中の終了

標準入力待ち中は、コンテキスト取消だけでは読込が解除されない可能性があった。

対策として、読込を別goroutineへ分離し、終了時に所有する入力を閉じて実行中要求を取り消すようにした。`SIGTERM`を送った実プロセスが終了コード0で停止することを確認した。

## 8. 残る検証

次はWindows側のC#検証ハーネスが必要である。

- C# `Process`からの起動、非同期読込、取消
- Windowsログオフ時の終了
- スリープ・復帰後の再接続
- C#異常終了時のLinuxバックエンド孤児化
- Explorer再起動後のtray復元
- C#への型付き`app.start` / `app.stop`要求
- Windowsアプリ終了拒否、UAC権限差
- GoバックエンドのWSL2への配置・更新・ロールバック

WSL2の停止・再起動を伴う検証は、この検証セッション自体が同じWSL2内で動作しているため実行していない。実施する場合は、セッション終了を前提にWindows側から手動で行う必要がある。
