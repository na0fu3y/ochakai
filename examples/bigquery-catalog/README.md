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
[0001](../../docs/design/0001-architecture.md) §2)。それは*あなたが*
カタログを埋めることを何ら妨げない — このジョブは外で、CLI が使うのと
同じ REST API の普通のクライアントとして、あなた自身のサービス
アカウントで動く。

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
