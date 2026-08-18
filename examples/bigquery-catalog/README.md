# BigQuery カタログの同期

BigQuery のテーブルメタデータを `BigQuery Table` concept として、
データセットごとの一覧を `BigQuery Dataset` concept として ochakai に
投影する、毎日のジョブ。`shop.orders` について訊いたエージェントが、
カラムと、忘れてはいけないパーティションフィルタと、その後に人が下に
書き足したことを見つけられるようにする。

**これは例であって機能ではない。** ochakai はコネクタを持たず、今後も
持たない: サーバーの中に住む収集器は、ochakai が意図して持たないウェア
ハウスの資格情報を要るし、ここでのナレッジは収集ではなくキュレーション
されるものだからである(README の「断るもの」の表、設計ドキュメント
[0081](../../docs/design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1)。それは*あなたが*
カタログを埋めることを何ら妨げない — このジョブは外で、CLI が使うのと
同じ REST API の普通のクライアントとして、あなた自身のサービス
アカウントで動く。

## その前に: 初日の一回なら `ochakai seed`

ここにあるのは**毎日走る同期**である(receipt、attester、差分)。
最初の一回だけ、空のベースにテーブルを入れて眺めたいなら、Python も
receipt も要らない — 自分で撃ったクエリの答えを `ochakai seed` に渡すと、
draft の OKF バンドルになって出てくる([0085](../../docs/design/0085-the-empty-base-and-what-fills-it.md)):

```sh
bq query --max_rows=100000 --format=json --nouse_legacy_sql \
  'SELECT table_schema, table_name, column_name, data_type, is_nullable
     FROM `proj.dataset.INFORMATION_SCHEMA.COLUMNS` ORDER BY ordinal_position' |
  ochakai seed --project proj - | ochakai import -
```

`--max_rows` はここでは飾りではない。`bq query` は既定で先頭 100 行しか
出さず、打ち切りを何も言わない — カラムが 100 本を超えるデータセットは、
黙って一部のテーブルだけが入る。`seed` の「seeded N tables」を自分の
テーブル数と突き合わせること。

`seed` もウェアハウスには接続しない — 撃つのはあなたの `bq` であり、
ochakai が受け取るのはその出力だけである。出てくる draft の中身を
書き始めて、同期を毎日回したくなったときに読むのが以下である。

## スクリプトの入ったフォルダではなく、ナレッジとして出荷される

`bundle/` は 3 concept の OKF バンドルである。これを import すると、
ジョブは自分が書き込むナレッジベースの中で自分自身を説明する:

```sh
ochakai import examples/bigquery-catalog/bundle
```

**2 つのファイルはバケットを要る。** `OCHAKAI_GCS_BUCKET` の無い
インスタンスは、どのファイルにも `501` を返す
([0075](../../docs/design/0075-the-bundle-is-the-address-space.md) §1)
ので、上の import は 3 つの concept を書いた後、最初の `.py` で失敗する。
Cloud Run にはバケットがあるが `deploy/compose.yaml` には無く、そちらで
読む価値があるのは concept のほうである — コードは既に目の前にある:

```sh
tar czf - -C examples/bigquery-catalog/bundle --exclude='*.py' . |
  ochakai import -
```

| Concept | 型 | |
|---|---|---|
| [`skills/build-the-first-catalog`](bundle/skills/build-the-first-catalog.md) | Skill | **ここから** — day-one の順序: 小さく範囲を決め、住所を確定し、テーブルを投影し、一つ verify し、それからやっと computation を付ける |
| [`jobs/sync-bigquery-catalog`](bundle/jobs/sync-bigquery-catalog.md) | Attested Computation | `runtime: python`、パラメータ、receipt、そして投影が下すすべての判断 |
| [`jobs/draft-from-query-history`](bundle/jobs/draft-from-query-history.md) | Attested Computation | **コールドスタートのもう半分** — 何度も撃たれているクエリを golden query の draft として書き戻す |
| [`skills/run-a-python-job`](bundle/skills/run-a-python-job.md) | Skill | `executor` が指しているもの: どの identity で走らせるか、IAM ロール、receipt のフィールド |

concept と一緒に 2 つのファイルが旅をする。
[`sync-bigquery-catalog.py`](bundle/jobs/sync-bigquery-catalog/sync-bigquery-catalog.py)
は computation そのものである。`# Computation` フェンスに貼り込むのでは
なく concept の `computation` キーがファイルとして名指している —
インラインに書くには長すぎる computation のために
[OKF SPEC](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
§10.2 が与えている形である — そして concept と一緒にファイルとして旅を
するので、`ochakai get jobs/sync-bigquery-catalog --download .` が契約と
一緒にコードを持ち帰る。置かれているのは文書の隣、concept の名前の
ディレクトリの中で、そこは `ochakai export` がファイルを置く場所であり、
Web UI のファイルタブが参照せよと言う場所でもある(設計ドキュメント
[0075](../../docs/design/0075-the-bundle-is-the-address-space.md) §5)。

もう一つの [`catalog-sync.py`](bundle/attesters/catalog-sync.py) は
`attester.resource` が名指すもの: その実行が有効だったかを判定する
コードである。sanctioned な範囲で走ったか(項目の id はそこから導かれる
ので、間違った prefix で走った実行は黙って二つ目のカタログを作る)、
数が保存されるか、そしてカタログが一晩で崩れていないかを確かめる。
ネットワークも LLM も無く、ナレッジベースを読みもしない。receipt と
authorized な値が入力のすべてである。OKF リファレンスバンドルの
`attesters/sql_equality.py` に倣っており、あちらは SQL の実行について
最初の二つの問いを立てる。

同期そのものと同じくらい、**この形**が要点である。カタログを更新せよと
言われたエージェントが受け取るのは、*これらのパラメータを与えてこの
ファイルを走らせよ*と言う契約であり、SPEC §10.3 は「代わりに computation
を自分で書いてはならない」と明言している。自分の保守ジョブが自分の中の
concept になっているナレッジベースは、誰も読まない README の中に住むもの
が一つ減っている。

3 つの concept は **draft** として import される。これは意図的である。
自分のプロジェクトでジョブを走らせ、receipt がきれいに返ってきてから
verify すること — それはちょうど、IAM ロールを実際に付与したとおりに
直したくなる瞬間でもある。

## 何が既に訊かれているか: `draft-from-query-history`

テーブルが入っただけのベースは、**カラム名は答えるが、誰も何を訊いて
いるかは知らない**。倉庫は自分でそれを記録している — ジョブ履歴である。

`seed` と同じ形で受け取る。あなたが自分の identity でクエリを撃ち、
答えをパイプで渡す:

```sh
bq query --max_rows=100000 --format=json --nouse_legacy_sql \
  'SELECT query, user_email, creation_time, referenced_tables
     FROM `region-us`.INFORMATION_SCHEMA.JOBS
    WHERE creation_time >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 90 DAY)
      AND job_type = "QUERY" AND state = "DONE" AND error_result IS NULL' |
  python3 bundle/jobs/draft-from-query-history/draft-from-query-history.py |
  ochakai import -
```

ここでも `--max_rows` が要る。既定の 100 行では 90 日分のつもりが直近の
100 ジョブになり、「5 回以上」の敷居に届くものがほとんど無くなる —
`jobs_read` の数を見ること。

リテラルとコメントと空白だけが違う実行は**一つにまとまり**、5 回以上
撃たれたものが draft として着地する。**LLM は入っていない** — 「この
クエリは何の問いに答えるのか」だけは書けないので、各 draft は空の見出し
を持って出てくる。そこを埋めるのはあなたか、**あなたの**エージェントで
ある。サーバーは決定論のままである。

これで初日の一続きが揃う: `seed` でテーブル、この二つ目で問い、そして
[レビューキュー](../../docs/loop.md)で人が裁定する。全部 draft で着地
するのは、**何度も撃たれていることは「大事だ」の証拠であって「正しい」
の証拠ではない**からである。

## 何にも触らずに試す

```sh
pip install google-cloud-bigquery google-auth requests

python bundle/jobs/sync-bigquery-catalog/sync-bigquery-catalog.py \
  --project my-project --datasets shop \
  --ochakai-url https://ochakai-<hash>.run.app --dry-run
```

`--dry-run` は書き込むはずだった concept を印字するだけで、ochakai の
資格情報を一切要らない。それ以外のすべて — 毎日のスケジュール、IAM の
付与、人が書いたものをジョブが踏み潰さないための所有権の規則、そして
意図して外してあるもの(Looker、推奨テーブルのフラグ、削除)— は
2 つの concept の中にある。
