---
type: Attested Computation
title: 滞留在庫の処分提案
description: 売れ残りが積み上がり、最近も売れていない商品を拠点ごとに洗い出し、処分の決定草案を起こすアクション
tags: [inventory, products, action, bigquery]
generated: { by: human:sato@example.co.jp, at: 2026-08-24T05:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-27T01:15:00Z }
status: stable
runtime: bigquery
parameters:
  - { name: as_of, type: DATE, required: true }
  - { name: min_age_days, type: INT64, required: true }
  - { name: min_units, type: INT64, required: true }
executor:
  resource: /skills/run-an-action.md
  receipt: [job_id, executed_sql, row_count, draft_id]
attester:
  resource: https://git.example.co.jp/analytics/attesters/sql_equality.py
question: 処分を検討すべき在庫はどれか?
---

[inventory_items](/tables/inventory-items.md) の売れ残りを、商品と拠点の
単位に束ね、処分の決定草案にかける候補を出すアクションである。
[返品率の高い商品の見直し](/actions/review-high-return-products.md)と
同じく、パラメータと検証はこの concept が持ち、実行の手順は
[アクションを実行する](/skills/run-an-action.md)が持つ。

候補になるのは、`as_of` の時点で入荷から `min_age_days` 日以上売れ残って
いる個体が `min_units` 点以上あり、かつその期間に 1 点も売れていない
商品である。

[明細単価](/metrics/average-item-price.md)が下がって売上が落ちた月に、
在庫の側から取れる行動がこれである。売れ筋の単価は動かせないが、
売れない在庫に寝ている原価は減らせる。

# Computation

```sql
SELECT
  p.id                              AS product_id,
  p.name,
  p.brand,
  p.category,
  ii.product_distribution_center_id AS distribution_center_id,
  COUNT(*)                          AS aged_units,
  ROUND(SUM(ii.cost), 2)            AS aged_cost,
  MIN(DATE(ii.created_at))          AS oldest_received
FROM `bigquery-public-data.thelook_ecommerce.inventory_items` AS ii
JOIN `bigquery-public-data.thelook_ecommerce.products` AS p
  ON p.id = ii.product_id
WHERE ii.sold_at IS NULL
  AND DATE(ii.created_at) <= DATE_SUB(@as_of, INTERVAL @min_age_days DAY)
  AND ii.product_id NOT IN (
    SELECT s.product_id
    FROM `bigquery-public-data.thelook_ecommerce.inventory_items` AS s
    WHERE DATE(s.sold_at) > DATE_SUB(@as_of, INTERVAL @min_age_days DAY))
GROUP BY 1, 2, 3, 4, 5
HAVING COUNT(*) >= @min_units
ORDER BY aged_units DESC, aged_cost DESC
```

売れたかどうかは `sold_at` で見ている。`sold_at` は注文が入った時刻で、
キャンセルや返品になった注文の個体も売れたことになる。ここで知りたいのは
「注文が来ているか」なので、それでよい。

# 検証: 実行してよいパラメータの範囲

範囲の外なら、実行せずに人へ返すこと。

- `as_of` は実行する日の日付。`sold_at` が null かどうかは今日の状態なので、
  過去の日付を入れても過去の在庫にはならない。
- `min_age_days` は **180 以上 365 以下**。売れた個体はどれも入荷から
  60 日以内に売れているので、61 日を過ぎた個体はこの先も売れない。下限は
  61 でも意味は通るが、「その期間に 1 点も売れていない」の条件が短い窓
  ではほとんど効かず、候補が百を超える。
- `min_units` は **20 以上**。180 日と 20 点で候補は数十件になる。在庫は
  商品あたり薄く、売れ残りが 30 点を超える商品はほとんど無いので、上限は
  置いていない。候補が 0 件なら、それも結果である。

# 副作用の境界

出力は候補であって、処分の決定ではない。実行者は
[アクションを実行する](/skills/run-an-action.md)の手順どおり、候補の表と
束縛したパラメータと `job_id` を決定草案として `decisions/` の下に draft
で書き、`draft_id` を receipt で返す。値下げ・廃棄・拠点間の移動を実際に
行うのは在庫の担当で、草案が人に verify されてからである。

草案には、`aged_cost` が原価の合計であって、処分で戻ってくる金額でも
失う金額でもないことを書くこと。どれだけ戻るかは処分の方法で決まり、
このデータには無い。
