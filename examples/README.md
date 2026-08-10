# サンプル

- **[demo/](demo)** — 10 concept のナレッジベース丸ごと。コマンド一つで
  import できる。下で説明する。
- **[bigquery-catalog/](bigquery-catalog)** — 空のベースを BigQuery から
  埋める二つのジョブ。テーブルのメタデータを毎日投影するものと、**ジョブ履歴
  から golden query の draft を起こすもの**(何が既に訊かれているかは、倉庫が
  自分で記録している)。どちらも Attested Computation として、その Python を
  隣に置いて出荷している。コネクタによる取り込みがサーバーの外に留まると、
  こうなる。
- **[claude-code/](claude-code)** — 想起と書き戻しのループを、Claude Code
  の指示とフックにしたもの。
- **[golden-query.md](golden-query.md)** — concept 一つだけ。
  `ochakai put -f` 用。

## demo/: import できるナレッジベース

架空のオンラインショップの売上についての 10 concept を、
[OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
v0.2 バンドルとして書いたもの。ナレッジベース一つの姿が丸ごと見える程度の
中身がある: メトリクスとその裏にある attested computation、両者が乗って
いる用語とテーブルカタログの concept、その数字を決めるポリシー、クエリの
走らせ方を言う skill、そして系列の*読み方*を言う `Insight` — どの
semantic layer も持たないナレッジを運ぶ型である。

動いているサーバーに読み込ませる:

```sh
OCHAKAI_URL=http://localhost:8080 ochakai import examples/demo
```

そのあと `ochakai context "なぜ売上が落ちているのか"`、
`ochakai list --links-to glossary/completed-order`、
`ochakai list stale_after` を試すとよい。concept は普通の markdown リンク
で互いを指しているので、グラフは既にそこにある — 何も宣言していない。

**わざと均質にしていない**: 2 つの concept は **draft** なのでレビュー
キューは空にならず、1 つの `Reference` は **deprecated** で、1 つの draft
は `stale_after` を既に過ぎているので stale フィードにも住人がいる。
一ヶ月使った本物のナレッジベースは、こう見える。

このファイルが `demo/` の中に無いのは意図的である。OKF 適合はバンドル内の
予約名でない*すべて*の `.md` が `type` を持つ frontmatter を運ぶことを
求めており(SPEC §11)、免除されるのは `index.md` と `log.md` だけである。
バンドルの中に README があるとその規則を破り、`ochakai import` が毎回
ファイルを一つスキップすることになる。`demo/` 以下のすべての `.md` は
concept 文書なので、import は 10 concept を報告し、何もスキップしない。

### 数字はでっち上げである

そこにある数値はすべて作り物である: ベースライン、季節性のパーセント、
しきい値、テーブル名とプロジェクト名、`example.co.jp`、登場する二人とも。
何一つ測っていない。これは教材であり、concept の*形*を読んで真似する
ためのものであって、中身を真似するためのものではない。エージェントが
実際に使うナレッジベースにこれを import すると、存在しない店について
自信満々の出鱈目を教え込むことになる。
