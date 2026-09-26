---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.users
title: users
description: 会員。2 割は一度も買っていない。traffic_source は獲得時のチャネルで、同名の列が events に別の意味で存在する
tags: [customers, marketing, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T04:20:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-29T01:10:00Z }
status: stable
---

会員である。1行は会員1人で、[orders](/tables/orders.md)と
[order_items](/tables/order-items.md)の両方が `user_id` で指してくる。
合成データなので、名前も email も実在しない。

**会員は買った客ではない。** 会員のおよそ 2 割は注文を 1 件も持たない。
`created_at` は会員登録の時刻で、初回購入ではない。[リピート購入率](/metrics/repeat-purchase-rate.md)
の分母を users から作ると、登録だけした客が混ざる。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `id` | INT64 | 主キー。人を識別するのはこの列だけ |
| `email` | STRING | **一意ではない**。名前から合成されていて、10 万人に対して重複の無い値は 8 万強。email で名寄せすると別人がまとまる |
| `traffic_source` | STRING | 獲得時のチャネル。下の注意を読むこと |
| `created_at` | TIMESTAMP | 会員登録。UTC |
| `gender` | STRING | `M` / `F`。買う商品の部門と完全に対応する([products](/tables/products.md)) |
| `country` / `state` / `city` | STRING | 客は 15 か国にいる。スペインは `Spain` と `España` の二つの綴りに割れている |
| `latitude` / `longitude` / `user_geom` | FLOAT64 / GEOGRAPHY | 同じ座標 |

## traffic_source は同じ名前の列が二つある

この列は獲得時のチャネル、つまりその客を最初に連れてきた経路で、値は
`Search` / `Organic` / `Facebook` / `Email` / `Display` の 5 つ。

[events](/tables/events.md) にも同じ名前の列があり、そちらは訪問ごとの
流入元で、値は `Email` / `Adwords` / `YouTube` / `Facebook` / `Organic`。
**値の集合も重なっていない。** 購入セッションの流入元が客の獲得チャネル
と同じ値になっているのは、全体の数%しかない。どちらの列で割ったかを
書かない「チャネル別売上」は、読む側が自分の思っている方で読む。

# Joins

- [orders](/tables/orders.md) — `o.user_id = u.id`。客を起点に `JOIN` すると
  注文 0 件の会員が消え、`LEFT JOIN` にすると残る。
- [events](/tables/events.md) — `e.user_id = u.id`。つながるのは購入
  セッションだけで、匿名のセッションはどの客にもつながらない。
