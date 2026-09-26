---
type: Insight
title: inventory_item_id は売れた在庫を指さない
description: order_items.inventory_item_id は外部キーの形をしているが、指す先はどれも未販売の個体。売れた個体とは product_id と sold_at = created_at で1対1に結べる
tags: [inventory, join, data-quality]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-13T06:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-18T01:00:00Z }
status: stable
---

[order_items](/tables/order-items.md) の `inventory_item_id` は、名前も型も
[inventory_items](/tables/inventory-items.md) への外部キーの形をしていて、
指す先の行も必ず存在する。しかし**指されている個体はどれも `sold_at` が
null の、まだ売れていない個体**で、3 割近くは明細より後に入荷している。
売れた個体のほうは、1 つも `inventory_item_id` から指されていない。
素直に JOIN すると、全行が別の個体につながる。

売れた個体と明細は、商品と時刻の組で 1 対 1 に対応する。個体の `sold_at`
は明細の `created_at` と秒まで一致する。

```sql
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
JOIN `bigquery-public-data.thelook_ecommerce.inventory_items` AS ii
  ON  ii.product_id = oi.product_id
  AND ii.sold_at    = oi.created_at
```

## 何が変わるか

- 入荷から売却までの日数は、この結合でしか出せない。
- 原価は `inventory_item_id` で引いてもたまたま合う(同じ商品の別の個体
  なので)が、入荷日は合わない。
- 「売れなかったのは在庫が無かったからか」には、この結合でも答えられない。
  在庫の数の履歴が無いからである([不足しているデータ](/insights/missing-data.md))。

## 確かめる

データセットは毎日作り直されるので、この性質が続いているかは走らせて
確かめる。どちらも 0 が返れば、ここに書いたとおりである。

```sql
SELECT
  (SELECT COUNTIF(ii.sold_at IS NOT NULL)
   FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
   JOIN `bigquery-public-data.thelook_ecommerce.inventory_items` AS ii
     ON ii.id = oi.inventory_item_id)                          AS points_at_sold_item,
  (SELECT COUNT(*) FROM (
     SELECT oi.id
     FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
     LEFT JOIN `bigquery-public-data.thelook_ecommerce.inventory_items` AS ii
       ON ii.product_id = oi.product_id AND ii.sold_at = oi.created_at
     GROUP BY oi.id
     HAVING COUNT(ii.id) != 1))                                AS items_without_one_match
```
