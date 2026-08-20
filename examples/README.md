# サンプル

- **[demo/](demo)** — 18 concept のナレッジベース丸ごと。コマンド一つで
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

Google の公開データセット
[`bigquery-public-data.thelook_ecommerce`](https://console.cloud.google.com/marketplace/product/bigquery-public-data/thelook-ecommerce)
— Looker が生成する架空の衣料品店 — についての 18 concept を、
[OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
v0.2 バンドルとして書いたもの。実在のデータセットなので、
**`# Computation` フェンスの SQL はどれも本当に走る**。ナレッジベース
一つの姿が丸ごと見える程度の中身がある: メトリクスとその裏にある
attested computation、両者が乗っている用語とテーブルカタログの
concept、その数字を決めるポリシー、クエリの走らせ方を言う skill、系列
の*読み方*を言う `Insight`、そして**アクション** — 候補を出す計算と、
決定草案の書き戻しと、人の裁定で止まる副作用の契約を一枚に持つ
concept である。

Palantir Foundry がオントロジーと呼ぶもの — object・property・link の
意味層と、action・function の動力層 — をこの分業でどう持つかは、
バンドル自身が `glossary/ontology` という concept で説明している。
対応表はそこにあり、demo.ochak.ai で `オントロジー` を検索すると
出てくる。

動いているサーバーに読み込ませる:

```sh
OCHAKAI_URL=http://localhost:8080 ochakai import examples/demo
```

そのあと `ochakai search "なぜ売上が落ちているのか"`、
`ochakai list --links-to glossary/completed-order`、
`ochakai list stale_after` を試すとよい。concept は普通の markdown リンク
で互いを指しているので、グラフは既にそこにある — 何も宣言していない。

**わざと均質にしていない**: 2 つの concept は **draft** なのでレビュー
キューは空にならず、1 つの Attested Computation は **deprecated**
(FY2025 のレポートが乗っていた受注ベースの合計)で、1 つの draft は
`stale_after` を既に過ぎているので stale フィードにも住人がいる。
一ヶ月使った本物のナレッジベースは、こう見える。

**書かれているのは日本語である。** ochak.ai のデモは日本語でやると決めた
からで、[demo.ochak.ai](https://demo.ochak.ai) が見せているのもこのバンドル
である。日本語なのは人が読む側 — 題・説明・本文・`question`・呼び名
(`売上` / `受注額` / `GMV` の区別)と注文状態の和名 — であって、倉庫から
来る名前(`sale_price`、`status = 'Complete'`、`traffic_source` の値、
`# Computation` フェンスの SQL)は英語のままである。列は英語で、判断は
日本語で下る。日本語話者のチームのナレッジベースは実際にこうなるので、
デモもそうしてある。上の `ochakai search "なぜ売上が落ちているのか"`
が答えを返すのはこのためで、`売上` のような二文字語が索引で引けることが
何を意味するかは、ここで実際に見える(設計ドキュメント
[0080](../docs/design/0080-search-and-how-a-deployment-embeds.md))。

このファイルが `demo/` の中に無いのは意図的である。OKF 適合はバンドル内の
予約名でない*すべて*の `.md` が `type` を持つ frontmatter を運ぶことを
求めており(SPEC §11)、免除されるのは `index.md` と `log.md` だけである。
バンドルの中に README があるとその規則を破り、`ochakai import` が毎回
ファイルを一つスキップすることになる。`demo/` 以下のすべての `.md` は
concept 文書なので、import は 18 concept を報告し、何もスキップしない。

### データは実在し、チームは架空である

SQL と、テーブル・列・enum についての記述は、実在する公開データセット
に対するものである — 走らせて確かめられるし、走らせて確かめるべきもの
として書いてある(バンドル自身の方針として、動く数字は concept に
書かず、承認された計算が receipt 付きで出す)。一方で判断の側はすべて
作り物である: `example.co.jp` も、登場する二人も、売上計上ポリシーも、
アクションの閾値も、誰の業務でもない。これは教材であり、concept の
*形*を読んで真似するためのものであって、この判断を自分の店に持ち込む
ためのものではない。エージェントが実際に使うナレッジベースにこれを
import すると、自分の店ではない店の決めごとを教え込むことになる。
