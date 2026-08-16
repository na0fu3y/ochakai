# Claude Code との統合

二つの層がある。弱いほうから:

1. **[CLAUDE.md](CLAUDE.md)** はエージェントにコマンドと「学んだことを
   書き戻す」習慣を教える。中身をあなたのプロジェクトの `CLAUDE.md` に
   コピーする。指示はエージェントが覚えていることに頼るので、出発点
   としては良いが、遵守は完全ではない。
2. **[hooks/](hooks)** はループを自動にする。フックを実行するのは
   Claude Code 自身なので毎回必ず発火し、エージェントの判断は挟まらない
   — メモリ層が使っているのと同じ手を、LLM 抜きでやる:
   - `ochakai-recall.sh`(**UserPromptSubmit**)は `ochakai search
     "<prompt>" --json` を実行し、返ってきた順位 — id・型・trust・説明の
     ポインタ行 — をエージェントが作業を始める前にコンテキストへ差し込み、
     何を指したかを下の Stop フックのために記録する。自動の想起である。
     ナレッジ本体は注入しない(設計ドキュメント
     [0108](../../docs/design/0108-the-context-pack-retires.md)):
     fetch はエージェント自身の選択で、`ochakai get` で取った concept は
     自分を指す concept を `linked_from` として連れてくる。
   - `ochakai-write-back.sh`(**Stop**)はデータ作業のセッションごとに
     一度、エージェントが止まる直前に割り込み、再利用できるクエリと
     メトリクスの気づきを保存するか(書き戻し)、想起で指された concept が
     実際に使って持ちこたえたか(`report_outcome`)を訊く — あるいは
     どちらでもないと判断させる。

## 導入

```sh
# マシンごとに一度: CLI をあなたのサーバーに向ける
ochakai use https://ochakai-<hash>.run.app

# プロジェクトごと
mkdir -p .claude/hooks
cp hooks/ochakai-*.sh .claude/hooks/
chmod +x .claude/hooks/ochakai-*.sh
# settings.json の "hooks" キーを .claude/settings.json にマージする
```

どちらのスクリプトも `jq` を要り、失敗しても黙る: 届かないナレッジ
ベースがプロンプトや停止を塞ぐことはない。

## 調整

| 環境変数 | 効果 |
|---|---|
| `OCHAKAI_RECALL_LIMIT` | 想起フックが差し込むポインタ行の最大数(既定 8) |

つまみはこれ一つで、そしてこれが効くつまみである: フックが使うのは
あなたのエージェントのコンテキストウィンドウだからである。出力は上限に
達した時点で concept の印字をやめ、いくつ残したかを言うので、下げて
失うのは到達範囲であって正しさではない — エージェントは知らされた
concept を `ochakai get` で取りに行ける。

想起フックはスラッシュコマンドを飛ばす。書き戻しフックは、トランス
クリプトがデータ作業に見える(SQL や ochakai の利用がある)セッション
でのみ、しかもセッションごとに最大一度だけ発火する。`stop_hook_active`
のおかげで二度続けて発火することはない。
