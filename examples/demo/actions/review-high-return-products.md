---
type: Attested Computation
title: 返品率の高い商品の見直し
description: 返品率が閾値を超えた商品を洗い出し、取り扱い見直しの決定草案を起こすアクション
tags: [returns, products, action, bigquery]
generated: { by: human:sato@example.co.jp, at: 2026-08-03T05:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-05T01:15:00Z }
status: stable
runtime: bigquery
parameters:
  - { name: since, type: DATE, required: true }
  - { name: min_sold, type: INT64, required: true }
  - { name: threshold, type: FLOAT64, required: true }
executor:
  resource: /skills/run-an-action.md
  receipt: [job_id, executed_sql, row_count, draft_id]
attester:
  resource: https://git.example.co.jp/analytics/attesters/sql_equality.py
question: 取り扱いを見直すべき商品はどれか?
---

このバンドルにひとつだけあるアクションである。数字を読むための計算では
なく、決定の入口にあたる。Palantir で言う Action type がこのバンドルで
どうなるかは[オントロジー](/glossary/ontology.md)にある。パラメータと
検証はこの concept が持ち、実行の手順は
[アクションの実行](/skills/run-an-action.md)が持ち、決定そのものは draft
として書き戻され、裁定は人に残る。

やることは、[返品率](/metrics/return-rate.md)が `threshold` を超えた
[商品](/tables/products.md)を、母数 `min_sold` 以上のものに絞って列挙する
ことである。

# Computation

```sql
SELECT
  p.id                                        AS product_id,
  p.name,
  p.brand,
  p.category,
  COUNT(*)                                    AS resolved,
  COUNTIF(oi.status = 'Returned')             AS returned,
  ROUND(COUNTIF(oi.status = 'Returned') / COUNT(*), 3) AS return_rate
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
JOIN `bigquery-public-data.thelook_ecommerce.products` AS p
  ON p.id = oi.product_id
WHERE oi.status IN ('Complete', 'Returned')
  AND oi.created_at >= TIMESTAMP(@since)
GROUP BY 1, 2, 3, 4
HAVING COUNT(*) >= @min_sold
   AND COUNTIF(oi.status = 'Returned') / COUNT(*) >= @threshold
ORDER BY return_rate DESC
```

# 検証: 実行してよいパラメータの範囲

範囲の外なら、実行せずに人へ返すこと。

- `min_sold` は **5 以上 10 以下**。下限は母数の小さい商品を弾くためで
  ある。上限があるのは、このデータセットでは母数がそもそも小さいから
  である。注文が商品数に対して薄く広がるので、SKU あたりの決着済み明細は
  90 日窓で最大 5 件、全期間を見ても最大 12 件しかない。`threshold` を
  満たした候補に限れば母数は最大 9 件だったので、10 を超える下限を要求
  すると、窓をどれだけ広げても候補は 0 件になる。空のリストは決定の材料
  にならない。下限を低く取る代わりに、`threshold` を高く取る。
- `since` は少なくとも 90 日遡る。窓が短いと、季節商品が返品率の高い商品
  として出てしまう。
- `threshold` は **0.45 以上**。[返品率](/metrics/return-rate.md)の全体値
  がこのデータセットでは 4 分の 1 から 3 分の 1 のあたりを動くので、一般的
  な EC の感覚で 0.05 と置くと候補が何百行も出る。それは決定ではなく
  レポートである。全体値の倍あたりが、決定にかけられる大きさの候補が出る
  ところである。

母数が一桁だということは、率が荒いということでもある。5 件中 3 件が返品
された商品は 0.6 になるが、6 割返ってくる商品だという意味ではない。この
出力は見に行く候補であって結論ではなく、決定草案にもそう書く必要がある。
母数の小ささを書かない草案は、人に間違った確信を渡す。

# 副作用の境界

この計算の出力は候補リストであり、決定ではない。実行者は
[アクションの実行](/skills/run-an-action.md)の手順どおり、候補と根拠を
決定草案としてナレッジベースに draft で書き、`draft_id` を receipt で返す。
商品の取り扱いを実際に変えるのは商品チームで、動くのは草案が人に verify
されてからである。エージェントにその権限は無く、ochakai にも無い。ここに
あるのは、誰が何を根拠にそう決めたかを後から追える形だけである。
