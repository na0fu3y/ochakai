# サンプル

- **[demo/](demo)** — 37 concept のナレッジベース丸ごと。コマンド一つで
  import できる。下で説明する。
- **[bigquery-catalog/](bigquery-catalog)** — 空のベースを BigQuery から
  埋める二つのジョブ。一つはテーブルのメタデータを毎日投影するもの、もう
  一つはジョブ履歴から golden query の draft を起こすものである(何が既に
  訊かれているかは、ウェアハウス側に記録が残っている)。どちらも Attested
  Computation として、その Python を隣に置いて出荷している。コネクタに
  よる取り込みをサーバーの外に留めると、こういう形になる。
- **[claude-code/](claude-code)** — 想起と書き戻しのループを、Claude Code
  の指示とフックにしたもの。ループの人の手が追いつかない側 — 答えられ
  なかった問いの棚卸し — を回す skill も同梱している。
- **[golden-query.md](golden-query.md)** — concept 一つだけ。
  `ochakai put -f` 用。

## demo/: import できるナレッジベース

Google の公開データセット
[`bigquery-public-data.thelook_ecommerce`](https://console.cloud.google.com/marketplace/product/bigquery-public-data/thelook-ecommerce)
(Looker が生成する架空の衣料品店)についての 37 concept を、
[OKF](https://github.com/GoogleCloudPlatform/open-knowledge-format)
v0.2 バンドルとして書いたものである。実在のデータセットなので、
**`# Computation` フェンスの SQL はどれも実際に走る**。

### 一つの問いに答えられるように作ってある

このバンドルは、データエージェントがよく訊かれる一つの問いに、最後まで
答えられるように組んである。**「売上が落ちた。なぜか、何をすればいいか」**
である。そしてこのデータでの正しい答えは、たいてい**「揺れの範囲で、
理由は無い。何もしなくてよい」**になる。

1. [売上](demo/metrics/revenue.md)は `受注点数 × 完了率 × 明細単価` に
   分解してあり、三つはそれぞれ Metric になっている。
2. [月次売上の要因分解](demo/computations/revenue-drivers.md)が、どの要因が
   何ポイント動かしたかと、その動きがふだんの揺れの何倍か(`z_`)を
   一度に出す。
3. [売上が落ちるときの要因](demo/insights/why-revenue-falls.md)に、43 か月を
   分解して分かったことがある。売上が前月を割った 13 か月のうち、揺れの幅を
   超えて落ちた月は一つも無かった。揺れを超えた要因があったときに次に
   走らせる計算、取れる行動、行き止まるデータも表にしてある。「取れる
   行動」の列がほとんど空であることも、そのまま書いてある。
4. 行き止まりは[不足しているデータ](demo/insights/missing-data.md)に集め、
   「何があれば答えられたか」まで書いてある。因果を言うのに要る介入の
   記録も、その一行である。
5. 手順は[メトリクスが沈んだときの調べ方](demo/skills/diagnose-a-metric.md)
   にある。エージェントが書き戻した診断の見本が二つ、receipt 付きの draft
   として入っている。人が verify するまで、それは信用されない。
   - [2024 年 5 月の売上減](demo/insights/cases/2024-05-revenue-dip.md):
     −13% でも揺れの範囲で、理由を探さずに止まった例。
   - [2026 年 3 月、受注が伸びて売上が伸びなかった件](demo/insights/cases/2026-03-flat-revenue.md):
     揺れを超えていたのは 2 月のほうで、3 月は戻っただけだった例。

### OKF のサンプルと同じ形

置き場所と型は OKF のサンプルのバンドルに合わせた。`datasets/` に
`BigQuery Dataset`、`tables/` に `# Schema`・`# Joins`・`# Metrics` を持つ
`BigQuery Table`、`computations/` に `Attested Computation` を置く。独自の
型は作っていない。

Palantir Foundry がオントロジーと呼ぶもののうち、object type にあたる型は
OKF に無く、テーブルの concept がその役をしている。link type は専用の
concept にせず、テーブルの `# Joins` 節のリンクと、自明でない結合に
ついての Insight([inventory_item_id は売れた在庫を指さない](demo/insights/inventory-item-id.md)
など)で持つ。関係の意味は、結び方より「結ぶとこう読める」のほうに
あるからである。対応表は `glossary/ontology` にあり、demo.ochak.ai で
`オントロジー` を検索すると出てくる。

全部そろえてから始める必要は無い。最初の五枚(データセット、テーブル、
指標、その計算、その読み方)から育てる順番は
[このベースでの concept の書き方](demo/skills/write-a-concept.md)にある。

動いているサーバーに読み込ませる:

```sh
OCHAKAI_URL=http://localhost:8080 ochakai import examples/demo
```

そのあと `ochakai search "なぜ売上が落ちているのか"`、
`ochakai get insights/why-revenue-falls`、
`ochakai list --links-to metrics/completion-rate`、
`ochakai list stale_after` を試すとよい。concept は普通の markdown リンク
で互いを指しているので、グラフは何も宣言しなくても既にそこにある。

**わざと均質にしていない。** 5 つの concept は draft なので(エージェントが
書いた診断の二つもそれに含まれる)レビューキューは空にならず、1 つの Attested Computation は deprecated(FY2025 のレポート
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
[0147](../docs/design/0147-search-and-the-default-a-base-was-made-with.md))。

このファイルが `demo/` の中に無いのは意図的である。OKF 適合はバンドル内の
予約名でない*すべて*の `.md` が `type` を持つ frontmatter を運ぶことを求めて
おり(SPEC §11)、免除されるのは `index.md` と `log.md` だけである。バンドル
の中に README があるとその規則を破り、`ochakai import` が毎回ファイルを一つ
スキップすることになる。`demo/` 以下のすべての `.md` は concept 文書なので、
import は 37 concept を報告し、何もスキップしない。

### データは実在し、チームは架空である

SQL と、テーブル・列・enum についての記述は、実在する公開データセットに
対するものである。走らせて確かめられるし、確かめるべきものとして書いてある
(バンドル自身の方針として、動く数字は concept に書かず、承認された計算が
receipt 付きで出す)。このデータセットは履歴ごと毎日作り直されるので、
ある月が前月を割ったかどうかも日によって変わる。変わらないのは構造の
ほうで、個々の月についての数字は、診断の見本のように receipt と一緒に
しか書いていない。

一方で判断の側はすべて作り物である。`example.co.jp` も、登場する二人も、
売上計上ポリシーも、アクションの閾値も、誰の業務でもない。これは教材で
あり、concept の*形*を読んで真似するためのものである。この判断を自分の店
に持ち込むためのものではない。エージェントが実際に使うナレッジベースに
これを import すると、自分の店ではない店の決めごとを教え込むことになる。
