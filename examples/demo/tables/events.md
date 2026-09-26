---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.events
title: events
description: サイトの行動ログ。1行がページ閲覧1回で、セッションは session_id で束ねて作る。ログインしたセッションは必ず買い、匿名のセッションは決して買わない
tags: [web, marketing, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-11T02:20:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-13T01:10:00Z }
status: stable
---

サイトの行動ログで、1行がページ閲覧1回である。セッションのテーブルは
無く、`session_id` で束ねたものがセッションになる。このデータセットで
一番大きく、ほかの6つのテーブルを合わせたより大きいので、`created_at`
で絞ってから読むこと。

**セッションは二種類しかない。** ログインしたセッション(`user_id` あり)は
必ず商品を1つ見て、カートに入れ、`purchase` で終わる。匿名のセッション
(`user_id` が null)は決して買わない。ログインしたのに買わなかった訪問は
記録されていないので、**コンバージョン率はこのデータでは測れない**。
全セッションで割れば匿名セッションの数という生成器の設定値が出て、
ログインしたセッションで割れば常に 100% になる。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `session_id` | STRING | UUID。セッションを束ねるキー |
| `sequence_number` | INT64 | セッション内の順番。1 から欠番なし |
| `user_id` | INT64 | ログインしていれば客の id、していなければ null。セッションの途中で変わらない |
| `event_type` | STRING | `home` / `department` / `product` / `cart` / `purchase` / `cancel`。小文字 |
| `uri` | STRING | `/product/<id>` の `<id>` は商品の id |
| `traffic_source` | STRING | その訪問の流入元。セッション内で一定。[users](/tables/users.md) の同名の列とは語彙が違う |
| `created_at` | TIMESTAMP | UTC。最後の商品閲覧から `purchase` まで最大 4 日空く |
| `city` / `state` / `postal_code` | STRING | 購入セッションでは客の所在地と同じ。緯度経度は無い |

`cancel` は注文のキャンセルではない。匿名のセッションにしか出ず、注文の
`Cancelled` とは関係しない。

# Joins

- [users](/tables/users.md) — `u.id = e.user_id`。購入セッションだけが
  つながる。
- [order_items](/tables/order-items.md) — キーの列が無い。`purchase` の行の
  数は注文の数ではなく明細の数と一致し、客・商品・時刻で 1 対 1 に結べる。
  [セッションと明細のつなぎ方](/insights/sessions-and-order-items.md)。
