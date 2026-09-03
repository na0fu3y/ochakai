# サンプル

- **[demo/](demo)** — 18 concept のナレッジベース丸ごと。コマンド一つで
  import できる。下で説明する。
- **[bigquery-catalog/](bigquery-catalog)** — 空のベースを BigQuery から
  埋める二つのジョブ。一つはテーブルのメタデータを毎日投影するもの、もう
  一つはジョブ履歴から golden query の draft を起こすものである(何が既に
  訊かれているかは、ウェアハウス側に記録が残っている)。どちらも Attested
  Computation として、その Python を隣に置いて出荷している。コネクタに
  よる取り込みをサーバーの外に留めると、こういう形になる。
- **[claude-code/](claude-code)** — 想起と書き戻しのループを、Claude Code
  の指示とフックにしたもの。
- **[golden-query.md](golden-query.md)** — concept 一つだけ。
  `ochakai put -f` 用。

## demo/: import できるナレッジベース

Google の公開データセット
[`bigquery-public-data.thelook_ecommerce`](https://console.cloud.google.com/marketplace/product/bigquery-public-data/thelook-ecommerce)
(Looker が生成する架空の衣料品店)についての 18 concept を、
[OKF](https://github.com/GoogleCloudPlatform/open-knowledge-format)
v0.2 バンドルとして書いたものである。実在のデータセットなので、
**`# Computation` フェンスの SQL はどれも実際に走る**。

中身は、ナレッジベース一つの姿が丸ごと見える程度に揃えてある。メトリクス
とその裏にある attested computation、両者が乗っている用語とテーブル
カタログの concept、数字を決めるポリシー、クエリの走らせ方を言う skill、
系列の*読み方*を言う `Insight`、そしてアクションが一つずつある。アク
ションは、候補を出す計算と、決定草案の書き戻しと、人の裁定で止まる副作用
の契約を、一枚の concept に持つ。

Palantir Foundry がオントロジーと呼ぶもの(object・property・link の意味層
と、action・function の動力層)をこの分業でどう持つかは、バンドル自身が
`glossary/ontology` という concept で説明している。対応表はそこにあり、
demo.ochak.ai で `オントロジー` を検索すると出てくる。

動いているサーバーに読み込ませる:

```sh
OCHAKAI_URL=http://localhost:8080 ochakai import examples/demo
```

そのあと `ochakai search "なぜ売上が落ちているのか"`、
`ochakai list --links-to glossary/completed-order`、
`ochakai list stale_after` を試すとよい。concept は普通の markdown リンク
で互いを指しているので、グラフは何も宣言しなくても既にそこにある。

**わざと均質にしていない。** 2 つの concept は draft なのでレビューキュー
は空にならず、1 つの Attested Computation は deprecated(FY2025 のレポート
が乗っていた受注ベースの合計)で、1 つの draft は `stale_after` を既に過ぎ
ているので stale フィードにも住人がいる。一ヶ月使った本物のナレッジベース
は、だいたいこう見える。

**書かれているのは日本語である。** ochak.ai のデモは日本語でやると決めた
からで、[demo.ochak.ai](https://demo.ochak.ai) が見せているのもこのバンドル
である。日本語なのは人が読む側、つまり題・説明・本文・`question`・呼び名
(`売上` / `受注額` / `GMV` の区別)と注文状態の和名までで、ウェアハウス
から来る名前(`sale_price`、`status = 'Complete'`、`traffic_source` の値、
`# Computation` フェンスの SQL)は英語のままである。列は英語で、判断は
日本語で下る。日本語話者のチームのナレッジベースは実際にこうなるので、
デモもそうしてある。上の `ochakai search "なぜ売上が落ちているのか"` が
答えを返すのはこのためで、`売上` のような二文字語が索引で引けることが何を
意味するかも、ここで実際に見える(設計ドキュメント
[0080](../docs/design/0080-search-and-how-a-deployment-embeds.md))。

このファイルが `demo/` の中に無いのは意図的である。OKF 適合はバンドル内の
予約名でない*すべて*の `.md` が `type` を持つ frontmatter を運ぶことを求めて
おり(SPEC §11)、免除されるのは `index.md` と `log.md` だけである。バンドル
の中に README があるとその規則を破り、`ochakai import` が毎回ファイルを一つ
スキップすることになる。`demo/` 以下のすべての `.md` は concept 文書なので、
import は 18 concept を報告し、何もスキップしない。

### データは実在し、チームは架空である

SQL と、テーブル・列・enum についての記述は、実在する公開データセットに
対するものである。走らせて確かめられるし、確かめるべきものとして書いてある
(バンドル自身の方針として、動く数字は concept に書かず、承認された計算が
receipt 付きで出す)。

一方で判断の側はすべて作り物である。`example.co.jp` も、登場する二人も、
売上計上ポリシーも、アクションの閾値も、誰の業務でもない。これは教材で
あり、concept の*形*を読んで真似するためのものである。この判断を自分の店
に持ち込むためのものではない。エージェントが実際に使うナレッジベースに
これを import すると、自分の店ではない店の決めごとを教え込むことになる。
